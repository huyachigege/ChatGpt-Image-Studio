package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/imagehistory"
)

const maxImageTaskDeferredAttempts = 5
const imageTaskRetentionAfterFinish = 30 * time.Minute

var (
	imageTaskRetryBackoffBase = 2 * time.Second
	imageTaskRetryBackoffMax  = 5 * time.Second
)

type imageTaskLease struct {
	auth       *accounts.LocalAuth
	account    accounts.PublicAccount
	decision   accounts.ImageAccountRoutingDecision
	release    func()
	forceRoute string
}

type imageTaskManager struct {
	server        *Server
	mu            sync.Mutex
	scheduleMu    sync.Mutex
	scheduleTimer *time.Timer
	scheduleAt    time.Time
	tasks         map[string]*imageTask
	order         []string
	runningUnits  int
	subscribers   map[int]chan imageTaskEvent
	nextSubID     int
}

func newImageTaskManager(server *Server) *imageTaskManager {
	return &imageTaskManager{
		server:      server,
		tasks:       map[string]*imageTask{},
		subscribers: map[int]chan imageTaskEvent{},
	}
}

func (m *imageTaskManager) createTask(req createImageTaskRequest) (*imageTaskView, error) {
	task, err := m.newTask(req)
	if err != nil {
		return nil, err
	}
	if ok, err := m.hasPotentialCompatibleAccounts(task); err != nil {
		return nil, err
	} else if !ok {
		if task.Requirement.NeedPaid {
			return nil, newRequestError("paid_resolution_requires_paid_account", "当前分辨率仅支持 Plus / Pro / Team 图片账号，请先确保有可用 Paid 账号")
		}
		if task.Requirement.SourceAccountID != "" {
			return nil, newRequestError("source_account_unavailable", "原始图片所属账号当前不可用，请使用普通编辑重试")
		}
		return nil, newRequestError("no_available_image_accounts", "当前没有可用的图片账号")
	}

	m.mu.Lock()
	if existing := m.tasks[task.ID]; existing != nil && !isFinalImageTaskStatus(existing.Status) {
		m.mu.Unlock()
		return nil, newRequestError("image_task_already_active", "当前图片任务仍在处理中，请稍后再试")
	}
	m.removeTaskIDFromOrderLocked(task.ID)
	m.tasks[task.ID] = task
	m.order = append(m.order, task.ID)
	view := m.buildTaskViewLocked(task)
	snapshot := m.snapshotLocked()
	subscribers := m.subscriberChannelsLocked()
	m.mu.Unlock()

	m.broadcast(subscribers, imageTaskEvent{
		Type:     "task.upsert",
		Task:     view,
		Snapshot: snapshot,
	})
	if expiresAt := m.initialQueueExpiryAt(task); !expiresAt.IsZero() {
		m.scheduleAfter(expiresAt)
	}
	m.triggerSchedule()
	return view, nil
}

func (m *imageTaskManager) listTasks() ([]imageTaskView, *imageTaskSnapshot) {
	return m.listTasksForUser("")
}

func (m *imageTaskManager) listTasksForUser(userID string) ([]imageTaskView, *imageTaskSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]imageTaskView, 0, len(m.order))
	for _, id := range m.order {
		task := m.tasks[id]
		if task == nil || !imageTaskVisibleToUser(task, userID) {
			continue
		}
		items = append(items, *m.buildTaskViewLocked(task))
	}
	snapshot := m.snapshotLockedForUser(userID)
	return items, snapshot
}

func (m *imageTaskManager) getTask(id string) (*imageTaskView, *imageTaskSnapshot, error) {
	return m.getTaskForUser(id, "")
}

func (m *imageTaskManager) getTaskForUser(id string, userID string) (*imageTaskView, *imageTaskSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task := m.tasks[strings.TrimSpace(id)]
	if task == nil || !imageTaskVisibleToUser(task, userID) {
		return nil, nil, fmt.Errorf("task not found")
	}
	return m.buildTaskViewLocked(task), m.snapshotLockedForUser(userID), nil
}

func (m *imageTaskManager) waitForTask(ctx context.Context, taskID string) (*imageTaskView, error) {
	if taskID = strings.TrimSpace(taskID); taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if task, _, err := m.getTask(taskID); err == nil && isFinalImageTaskStatus(task.Status) {
		return task, nil
	}

	subID, ch := m.subscribe()
	defer m.unsubscribe(subID)

	for {
		select {
		case <-ctx.Done():
			task, _, err := m.getTask(taskID)
			if err == nil && task != nil && task.Status == imageTaskStatusQueued {
				_, _ = m.cancelTask(taskID)
			}
			return nil, ctx.Err()
		case event, ok := <-ch:
			if !ok {
				return nil, fmt.Errorf("task stream closed")
			}
			if event.Task == nil || event.Task.ID != taskID {
				continue
			}
			if isFinalImageTaskStatus(event.Task.Status) {
				return event.Task, nil
			}
		}
	}
}

func (m *imageTaskManager) cancelTask(id string) (*imageTaskView, error) {
	return m.cancelTaskForUser(id, "")
}

func (m *imageTaskManager) cancelTaskForUser(id string, userID string) (*imageTaskView, error) {
	taskID := strings.TrimSpace(id)
	m.mu.Lock()
	task := m.tasks[taskID]
	if task == nil || !imageTaskVisibleToUser(task, userID) {
		m.mu.Unlock()
		return nil, fmt.Errorf("task not found")
	}
	switch task.Status {
	case imageTaskStatusSucceeded, imageTaskStatusFailed, imageTaskStatusCancelled, imageTaskStatusExpired:
		view := m.buildTaskViewLocked(task)
		m.mu.Unlock()
		return view, nil
	case imageTaskStatusQueued:
		task.Status = imageTaskStatusCancelled
		task.FinishedAt = time.Now().UTC()
		for index := range task.Units {
			if task.Units[index].Status == imageTaskStatusQueued {
				task.Units[index].Status = imageTaskStatusCancelled
				task.Images[index].Status = "error"
				task.Images[index].Error = "任务已取消"
			}
		}
	default:
		now := time.Now().UTC()
		task.CancelRequested = true
		task.Status = imageTaskStatusCancelRequested
		for index := range task.Units {
			if task.Units[index].Status == imageTaskStatusQueued {
				task.Units[index].Status = imageTaskStatusCancelled
				task.Units[index].FinishedAt = now
				task.Images[index].Status = "error"
				task.Images[index].Error = "任务已取消"
				continue
			}
			if task.Units[index].Status == imageTaskStatusRunning && task.Units[index].Cancel != nil {
				task.Units[index].Cancel()
			}
		}
		if task.ActiveUnits == 0 {
			task.Status = imageTaskStatusCancelled
			task.FinishedAt = now
		}
	}
	cleanupAt := m.retentionDeadlineForTaskLocked(task)
	view := m.buildTaskViewLocked(task)
	snapshot := m.snapshotLocked()
	subscribers := m.subscriberChannelsLocked()
	m.mu.Unlock()

	m.broadcast(subscribers, imageTaskEvent{
		Type:     "task.upsert",
		Task:     view,
		Snapshot: snapshot,
	})
	if !cleanupAt.IsZero() {
		m.scheduleAfter(cleanupAt)
	}
	return view, nil
}

func (m *imageTaskManager) subscribe() (int, <-chan imageTaskEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSubID++
	id := m.nextSubID
	ch := make(chan imageTaskEvent, 32)
	m.subscribers[id] = ch
	return id, ch
}

func (m *imageTaskManager) unsubscribe(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.subscribers[id]
	if !ok {
		return
	}
	delete(m.subscribers, id)
	close(ch)
}

func (m *imageTaskManager) triggerSchedule() {
	go m.schedule()
}

func (m *imageTaskManager) schedule() {
	m.scheduleMu.Lock()
	defer m.scheduleMu.Unlock()

	for {
		if !m.tryScheduleOne() {
			return
		}
	}
}

func (m *imageTaskManager) tryScheduleOne() bool {
	now := time.Now().UTC()
	m.mu.Lock()
	expiredViews := m.expireQueuedTasksLocked(now)
	if len(expiredViews) > 0 {
		snapshot := m.snapshotLocked()
		subscribers := m.subscriberChannelsLocked()
		m.mu.Unlock()
		for _, view := range expiredViews {
			m.broadcast(subscribers, imageTaskEvent{
				Type:     "task.upsert",
				Task:     view,
				Snapshot: snapshot,
			})
		}
		return true
	}
	removedTaskIDs := m.pruneRetainedTasksLocked(now)
	if len(removedTaskIDs) > 0 {
		snapshot := m.snapshotLocked()
		subscribers := m.subscriberChannelsLocked()
		nextWakeAt := m.nextMaintenanceAtLocked(now)
		m.mu.Unlock()
		if !nextWakeAt.IsZero() {
			m.scheduleAfter(nextWakeAt)
		}
		for _, taskID := range removedTaskIDs {
			m.broadcast(subscribers, imageTaskEvent{
				Type:     "task.remove",
				TaskID:   taskID,
				Snapshot: snapshot,
			})
		}
		return true
	}

	maxRunning := m.maxRunningLocked()
	if m.runningUnits >= maxRunning {
		updatedViews := make([]*imageTaskView, 0)
		for _, id := range m.order {
			task := m.tasks[id]
			if task == nil || task.Status != imageTaskStatusQueued {
				continue
			}
			previousReason := task.WaitingReason
			previousBlockers := append([]imageTaskBlocker(nil), task.Blockers...)
			_, retryAt := m.nextReadyQueuedUnitIndexLocked(task, now)
			if !retryAt.IsZero() {
				task.WaitingReason = imageTaskWaitingReasonRetryBackoff
				task.Blockers = []imageTaskBlocker{imageTaskRetryBackoffBlocker(now, retryAt)}
			} else {
				task.WaitingReason = imageTaskWaitingReasonGlobalConcurrency
				task.Blockers = []imageTaskBlocker{{Code: string(imageTaskWaitingReasonGlobalConcurrency), Detail: "等待全局并发槽位"}}
			}
			if previousReason != task.WaitingReason || !sameImageTaskBlockers(previousBlockers, task.Blockers) {
				updatedViews = append(updatedViews, m.buildTaskViewLocked(task))
			}
		}
		snapshot := m.snapshotLocked()
		subscribers := m.subscriberChannelsLocked()
		nextWakeAt := m.nextMaintenanceAtLocked(now)
		m.mu.Unlock()
		if !nextWakeAt.IsZero() {
			m.scheduleAfter(nextWakeAt)
		}
		for _, view := range updatedViews {
			m.broadcast(subscribers, imageTaskEvent{
				Type:     "task.upsert",
				Task:     view,
				Snapshot: snapshot,
			})
		}
		if len(updatedViews) == 0 {
			m.broadcast(subscribers, imageTaskEvent{Type: "snapshot", Snapshot: snapshot})
		}
		return false
	}

	candidateIDs := make([]string, 0, len(m.order))
	for _, id := range m.order {
		task := m.tasks[id]
		if task == nil {
			continue
		}
		if task.Status != imageTaskStatusQueued && task.Status != imageTaskStatusRunning {
			continue
		}
		if task.CancelRequested {
			continue
		}
		if task.ActiveUnits >= m.maxParallelUnitsForTaskLocked(task) {
			continue
		}
		if !m.canStartUnitForUserLocked(task) {
			continue
		}
		unitIndex, _ := m.nextReadyQueuedUnitIndexLocked(task, now)
		if unitIndex < 0 {
			continue
		}
		candidateIDs = append(candidateIDs, id)
	}
	nextWakeAt := m.nextMaintenanceAtLocked(now)
	m.mu.Unlock()

	for _, id := range candidateIDs {
		task := m.copyTask(id)
		if task == nil {
			continue
		}
		unitIndex, _ := m.nextReadyQueuedUnitIndexFromSnapshot(task, time.Now().UTC())
		if unitIndex < 0 {
			continue
		}
		lease, blocker, fatalErr := m.acquireLeaseForTask(task, unitIndex)
		if fatalErr != nil {
			m.failTask(id, fatalErr)
			return true
		}
		if lease == nil {
			m.updateTaskBlocker(id, blocker)
			continue
		}

		m.mu.Lock()
		current := m.tasks[id]
		if current == nil {
			m.mu.Unlock()
			if lease.release != nil {
				lease.release()
			}
			return false
		}
		unitIndex, retryAt := m.nextReadyQueuedUnitIndexLocked(current, time.Now().UTC())
		if unitIndex < 0 {
			m.mu.Unlock()
			if lease.release != nil {
				lease.release()
			}
			if !retryAt.IsZero() && (nextWakeAt.IsZero() || retryAt.Before(nextWakeAt)) {
				nextWakeAt = retryAt
			}
			continue
		}
		if m.runningUnits >= m.maxRunningLocked() || current.ActiveUnits >= m.maxParallelUnitsForTaskLocked(current) || !m.canStartUnitForUserLocked(current) {
			m.mu.Unlock()
			if lease.release != nil {
				lease.release()
			}
			return false
		}
		now := time.Now().UTC()
		runCtx, cancel := context.WithCancel(context.Background())
		if current.StartedAt.IsZero() {
			current.StartedAt = now
		}
		current.Status = imageTaskStatusRunning
		current.WaitingReason = imageTaskWaitingReasonNone
		current.Blockers = nil
		current.ActiveUnits++
		current.Units[unitIndex].Status = imageTaskStatusRunning
		current.Units[unitIndex].StartedAt = now
		current.Units[unitIndex].NextAttemptAt = time.Time{}
		current.Units[unitIndex].Cancel = cancel
		m.runningUnits++
		view := m.buildTaskViewLocked(current)
		snapshot := m.snapshotLocked()
		subscribers := m.subscriberChannelsLocked()
		m.mu.Unlock()

		m.broadcast(subscribers, imageTaskEvent{
			Type:     "task.upsert",
			Task:     view,
			Snapshot: snapshot,
		})

		go m.runUnit(id, unitIndex, lease, runCtx)
		return true
	}

	if !nextWakeAt.IsZero() {
		m.scheduleAfter(nextWakeAt)
	}
	return false
}

func (m *imageTaskManager) runUnit(taskID string, unitIndex int, lease *imageTaskLease, ctx context.Context) {
	images, err := m.server.executeImageTaskUnit(ctx, taskID, unitIndex, lease)
	if lease != nil && lease.release != nil {
		lease.release()
	}

	m.mu.Lock()
	task := m.tasks[taskID]
	if task == nil {
		m.mu.Unlock()
		m.triggerSchedule()
		return
	}
	now := time.Now().UTC()
	task.ActiveUnits--
	if task.ActiveUnits < 0 {
		task.ActiveUnits = 0
	}
	task.Units[unitIndex].Cancel = nil
	m.runningUnits--
	if m.runningUnits < 0 {
		m.runningUnits = 0
	}
	var deferredErr *imageTaskDeferredError
	if task.CancelRequested {
		task.Units[unitIndex].Status = imageTaskStatusCancelled
		task.Units[unitIndex].FinishedAt = now
		task.Units[unitIndex].Error = "任务已取消"
		task.Units[unitIndex].NextAttemptAt = time.Time{}
		task.Images[unitIndex].Status = "error"
		task.Images[unitIndex].Error = "任务已取消"
	} else if errors.As(err, &deferredErr) {
		if shouldRememberImageTaskFailedAttempt(deferredErr) {
			imageTaskUnitRememberAttempt(&task.Units[unitIndex], deferredErr.accessToken)
		}
		task.Units[unitIndex].DeferredCount++
		m.logTaskDeferredError(task, unitIndex, deferredErr)
		if task.Units[unitIndex].DeferredCount > maxImageTaskDeferredAttempts {
			message := "临时失败重试次数过多，请稍后重试"
			if deferredErr != nil && strings.TrimSpace(deferredErr.Error()) != "" {
				message = fmt.Sprintf("%s：%s", message, strings.TrimSpace(sanitizeImageUserFacingMessage(deferredErr)))
				if accountLabel := firstNonEmpty(deferredErr.accountEmail, deferredErr.accountFile); accountLabel != "" {
					message = fmt.Sprintf("%s（账号：%s）", message, accountLabel)
				}
			}
			task.Units[unitIndex].FinishedAt = now
			task.Units[unitIndex].Status = imageTaskStatusFailed
			task.Units[unitIndex].Error = message
			task.Units[unitIndex].NextAttemptAt = time.Time{}
			task.Images[unitIndex].Status = "error"
			task.Images[unitIndex].Error = message
		} else {
			backoff := imageTaskRetryBackoffDuration(task.Units[unitIndex].DeferredCount)
			task.Units[unitIndex].Status = imageTaskStatusQueued
			task.Units[unitIndex].StartedAt = time.Time{}
			task.Units[unitIndex].FinishedAt = time.Time{}
			task.Units[unitIndex].Error = ""
			task.Units[unitIndex].NextAttemptAt = now.Add(backoff)
			task.Images[unitIndex].Status = "loading"
			task.Images[unitIndex].Error = ""
			blocker := imageTaskRetryBackoffBlocker(now, task.Units[unitIndex].NextAttemptAt)
			task.WaitingReason = imageTaskWaitingReason(blocker.Code)
			task.Blockers = []imageTaskBlocker{blocker}
		}
	} else if err != nil {
		task.Units[unitIndex].FinishedAt = now
		task.Units[unitIndex].Status = imageTaskStatusFailed
		message := sanitizeImageUserFacingMessage(err)
		task.Units[unitIndex].Error = message
		task.Images[unitIndex].Status = "error"
		task.Images[unitIndex].Error = message
	} else if len(images) > 0 {
		task.Units[unitIndex].FinishedAt = now
		task.Units[unitIndex].Status = imageTaskStatusSucceeded
		image := images[0]
		image.ID = task.Images[unitIndex].ID
		image.Status = "success"
		if strings.TrimSpace(image.Prompt) == "" {
			image.Prompt = strings.TrimSpace(task.Prompt)
		}
		task.Images[unitIndex] = image
	} else {
		message := "image task returned no images"
		task.Units[unitIndex].FinishedAt = now
		task.Units[unitIndex].Status = imageTaskStatusFailed
		task.Units[unitIndex].Error = message
		task.Images[unitIndex].Status = "error"
		task.Images[unitIndex].Error = message
	}

	queuedUnits := 0
	runningUnits := 0
	failedUnits := 0
	cancelledUnits := 0
	for _, unit := range task.Units {
		switch unit.Status {
		case imageTaskStatusQueued:
			queuedUnits++
		case imageTaskStatusRunning:
			runningUnits++
		case imageTaskStatusFailed:
			failedUnits++
		case imageTaskStatusCancelled:
			cancelledUnits++
		}
	}

	switch {
	case task.CancelRequested && queuedUnits == 0 && runningUnits == 0:
		task.Status = imageTaskStatusCancelled
		task.FinishedAt = now
		task.WaitingReason = imageTaskWaitingReasonNone
		task.Blockers = nil
	case task.CancelRequested && runningUnits > 0:
		task.Status = imageTaskStatusCancelRequested
		task.FinishedAt = time.Time{}
		task.WaitingReason = imageTaskWaitingReasonNone
		task.Blockers = nil
	case runningUnits > 0:
		task.Status = imageTaskStatusRunning
		task.WaitingReason = imageTaskWaitingReasonNone
		task.Blockers = nil
	case queuedUnits > 0:
		task.Status = imageTaskStatusQueued
		task.FinishedAt = time.Time{}
	case failedUnits > 0:
		task.Status = imageTaskStatusFailed
		task.FinishedAt = now
		task.Error = firstNonEmpty(task.Images[unitIndex].Error, task.Error, "image task failed")
		task.WaitingReason = imageTaskWaitingReasonNone
		task.Blockers = nil
	case cancelledUnits == len(task.Units):
		task.Status = imageTaskStatusCancelled
		task.FinishedAt = now
		task.WaitingReason = imageTaskWaitingReasonNone
		task.Blockers = nil
	default:
		task.Status = imageTaskStatusSucceeded
		task.FinishedAt = now
		task.WaitingReason = imageTaskWaitingReasonNone
		task.Blockers = nil
	}

	view := m.buildTaskViewLocked(task)
	snapshot := m.snapshotLocked()
	subscribers := m.subscriberChannelsLocked()
	cleanupAt := m.retentionDeadlineForTaskLocked(task)
	m.mu.Unlock()

	m.broadcast(subscribers, imageTaskEvent{
		Type:     "task.upsert",
		Task:     view,
		Snapshot: snapshot,
	})
	if !cleanupAt.IsZero() {
		m.scheduleAfter(cleanupAt)
	}
	m.triggerSchedule()
}

func (m *imageTaskManager) failTask(taskID string, err error) {
	m.mu.Lock()
	task := m.tasks[taskID]
	if task == nil {
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	task.Status = imageTaskStatusFailed
	task.Error = err.Error()
	task.FinishedAt = now
	for index := range task.Images {
		if task.Images[index].Status == "loading" {
			task.Images[index].Status = "error"
			task.Images[index].Error = err.Error()
		}
	}
	view := m.buildTaskViewLocked(task)
	snapshot := m.snapshotLocked()
	subscribers := m.subscriberChannelsLocked()
	cleanupAt := m.retentionDeadlineForTaskLocked(task)
	m.mu.Unlock()

	m.broadcast(subscribers, imageTaskEvent{
		Type:     "task.upsert",
		Task:     view,
		Snapshot: snapshot,
	})
	if !cleanupAt.IsZero() {
		m.scheduleAfter(cleanupAt)
	}
}

func (m *imageTaskManager) updateTaskBlocker(taskID string, blocker imageTaskBlocker) {
	m.mu.Lock()
	task := m.tasks[taskID]
	if task == nil || task.Status != imageTaskStatusQueued {
		m.mu.Unlock()
		return
	}
	task.WaitingReason = imageTaskWaitingReason(blocker.Code)
	task.Blockers = nil
	if blocker.Code != "" {
		task.Blockers = []imageTaskBlocker{blocker}
	}
	view := m.buildTaskViewLocked(task)
	snapshot := m.snapshotLocked()
	subscribers := m.subscriberChannelsLocked()
	m.mu.Unlock()

	m.broadcast(subscribers, imageTaskEvent{
		Type:     "task.upsert",
		Task:     view,
		Snapshot: snapshot,
	})
}

func cloneStringSet(values map[string]struct{}) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]struct{}, len(values))
	for value := range values {
		clone[value] = struct{}{}
	}
	return clone
}

func (m *imageTaskManager) copyTask(taskID string) *imageTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[taskID]
	if task == nil {
		return nil
	}
	copy := *task
	copy.Images = append([]imagehistory.Image(nil), task.Images...)
	copy.Units = append([]imageTaskUnit(nil), task.Units...)
	for index := range copy.Units {
		copy.Units[index].Attempted = cloneStringSet(task.Units[index].Attempted)
	}
	copy.SourceImages = append([]imageTaskSourceImage(nil), task.SourceImages...)
	copy.ReferenceImages = append([]imageTaskSourceImage(nil), task.ReferenceImages...)
	copy.Blockers = append([]imageTaskBlocker(nil), task.Blockers...)
	return &copy
}

func (m *imageTaskManager) removeTaskIDFromOrderLocked(taskID string) {
	if len(m.order) == 0 {
		return
	}
	nextOrder := m.order[:0]
	for _, currentID := range m.order {
		if currentID == taskID {
			continue
		}
		nextOrder = append(nextOrder, currentID)
	}
	m.order = nextOrder
}

func (m *imageTaskManager) acquireLeaseForTask(task *imageTask, unitIndex int) (*imageTaskLease, imageTaskBlocker, error) {
	store := m.server.getStore()
	allowDisabled := m.server.allowDisabledStudioImageAccounts()

	excluded := imageTaskUnitAttemptedTokens(task, unitIndex)
	preferredRoute := m.preferredRouteForTask(task, unitIndex)
	if task.Requirement.SourceAccountID != "" {
		auth, account, release, err := store.FindImageAuthByIDWithLeaseForUserRoute(task.Requirement.SourceAccountID, task.UserID, preferredRoute)
		if err == nil {
			if _, attempted := excluded[auth.AccessToken]; !attempted {
				return &imageTaskLease{
					auth:    auth,
					account: account,
					release: release,
				}, imageTaskBlocker{}, nil
			}
			release()
		} else {
			if errors.Is(err, accounts.ErrSourceAccountNotFound) {
				return nil, imageTaskBlocker{}, newRequestError("source_account_not_found", "原始图片所属账号不存在，请使用普通编辑重试")
			}
			if errors.Is(err, accounts.ErrImageAuthInUse) {
				return nil, imageTaskBlocker{Code: string(imageTaskWaitingReasonSourceAccountBusy), Detail: "等待原始图片所属账号空闲"}, nil
			}
			return nil, imageTaskBlocker{}, err
		}
	}

	allowAccount := m.allowAccountFn(task)
	isFreeResolution := strings.EqualFold(strings.TrimSpace(task.ResolutionAccess), "free")
	if task.Requirement.PolicySnapshot != nil && task.Requirement.PolicySnapshot.Enabled {
		auth, account, decision, release, err := store.AcquireImageAuthLeaseForUserConversationWithPolicyRouteFilteredWithDisabledOption(
			excluded,
			allowAccount,
			allowDisabled,
			task.Requirement.PolicySnapshot,
			task.UserID,
			task.ConversationID,
			preferredRoute,
		)
		if err == nil {
			return &imageTaskLease{
				auth:     auth,
				account:  account,
				decision: decision,
				release:  release,
			}, imageTaskBlocker{}, nil
		}
		if errors.Is(err, accounts.ErrSelectedImageGroupsExhausted) || errors.Is(err, accounts.ErrNoAvailableImageAuth) || errors.Is(err, accounts.ErrImageAuthInUse) {
			if isFreeResolution && allowAccount != nil {
				allowAny := imageTaskAttemptAllowAnyAccount(task)
				auth2, account2, decision2, release2, err2 := store.AcquireImageAuthLeaseForUserConversationWithPolicyRouteFilteredWithDisabledOption(
					excluded, allowAny, allowDisabled, task.Requirement.PolicySnapshot, task.UserID, task.ConversationID, preferredRoute,
				)
				if err2 == nil {
					return &imageTaskLease{auth: auth2, account: account2, decision: decision2, release: release2}, imageTaskBlocker{}, nil
				}
			}
			if !isFreeResolution && m.server.externalResponsesConfigured() {
				return m.externalResponsesLease(), imageTaskBlocker{}, nil
			}
			if len(excluded) > 0 {
				imageTaskUnitClearAttempts(task, unitIndex)
				return m.acquireLeaseForTask(task, unitIndex)
			}
			return nil, m.busyBlocker(task), nil
		}
		return nil, imageTaskBlocker{}, err
	}

	auth, account, release, err := store.AcquireImageAuthLeaseForUserConversationRouteFilteredWithDisabledOption(
		excluded,
		allowAccount,
		allowDisabled,
		task.UserID,
		task.ConversationID,
		preferredRoute,
	)
	if err == nil {
		return &imageTaskLease{
			auth:    auth,
			account: account,
			release: release,
		}, imageTaskBlocker{}, nil
	}
	if errors.Is(err, accounts.ErrNoAvailableImageAuth) || errors.Is(err, accounts.ErrImageAuthInUse) {
		if isFreeResolution && allowAccount != nil {
			allowAny := imageTaskAttemptAllowAnyAccount(task)
			auth2, account2, release2, err2 := store.AcquireImageAuthLeaseForUserConversationRouteFilteredWithDisabledOption(
				excluded, allowAny, allowDisabled, task.UserID, task.ConversationID, preferredRoute,
			)
			if err2 == nil {
				return &imageTaskLease{auth: auth2, account: account2, release: release2}, imageTaskBlocker{}, nil
			}
		}
		if !isFreeResolution && m.server.externalResponsesConfigured() {
			return m.externalResponsesLease(), imageTaskBlocker{}, nil
		}
		if len(excluded) > 0 {
			imageTaskUnitClearAttempts(task, unitIndex)
			return m.acquireLeaseForTask(task, unitIndex)
		}
		return nil, m.busyBlocker(task), nil
	}
	return nil, imageTaskBlocker{}, err
}

func (m *imageTaskManager) externalResponsesLease() *imageTaskLease {
	return &imageTaskLease{
		auth:       &accounts.LocalAuth{AccessToken: "external_responses", Name: "external_responses"},
		account:    accounts.PublicAccount{Type: "External Responses", Email: m.server.externalResponsesLogAccount(), Status: "正常"},
		forceRoute: "external_responses",
	}
}

func (m *imageTaskManager) hasPotentialCompatibleAccounts(task *imageTask) (bool, error) {
	store := m.server.getStore()
	allowDisabled := m.server.allowDisabledStudioImageAccounts()

	if task.Requirement.SourceAccountID != "" {
		auth, account, err := store.FindImageAuthByID(task.Requirement.SourceAccountID)
		if err != nil || auth == nil {
			return false, nil
		}
		if !isImageAccountUsable(account, allowDisabled) && !accounts.NeedsImageQuotaRefreshWithTTL(account, time.Now(), m.server.cfg.ImageQuotaRefreshTTL()) {
			return false, nil
		}
		return true, nil
	}

	count, err := store.CountPotentialImageAuthCandidatesWithPolicyFilteredWithDisabledOption(
		m.allowAccountFn(task),
		allowDisabled,
		task.Requirement.PolicySnapshot,
	)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *imageTaskManager) allowAccountFn(task *imageTask) func(accounts.PublicAccount) bool {
	return imageTaskAttemptAllowAccount(task)
}

func (m *imageTaskManager) preferredRouteForTask(task *imageTask, unitIndexes ...int) string {
	if task == nil || m == nil || m.server == nil {
		return "legacy"
	}
	if !task.Requirement.NeedPaid && task.Mode == "generate" && task.SourceReference == nil && task.ContextReference == nil && len(task.SourceImages) == 0 && len(task.ReferenceImages) == 0 && len(unitIndexes) > 0 {
		unitIndex := unitIndexes[0]
		if unitIndex >= 0 && unitIndex < len(task.Units) && task.Units[unitIndex].DeferredCount > 0 {
			return "legacy"
		}
	}
	if task.SourceReference != nil || task.Mode == "edit" || len(task.SourceImages) > 0 || len(task.ReferenceImages) > 0 || task.ContextReference != nil {
		return "responses"
	}
	if task.Mode == "generate" && task.Requirement.NeedPaid {
		return "responses"
	}
	if task.Mode == "generate" && !strings.EqualFold(strings.TrimSpace(task.ResolutionAccess), "legacy") && m.server.externalResponsesConfigured() {
		return "responses"
	}
	return "legacy"
}

func (m *imageTaskManager) busyBlocker(task *imageTask) imageTaskBlocker {
	if task.Requirement.SourceAccountID != "" {
		return imageTaskBlocker{Code: string(imageTaskWaitingReasonSourceAccountBusy), Detail: "等待原始图片所属账号空闲"}
	}
	if task.Requirement.NeedPaid {
		return imageTaskBlocker{Code: string(imageTaskWaitingReasonPaidAccountBusy), Detail: "等待 Paid 图片账号空闲"}
	}
	return imageTaskBlocker{Code: string(imageTaskWaitingReasonCompatibleAccountBusy), Detail: "等待兼容图片账号空闲"}
}

func normalizeImageConversationContext(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	normalized := strings.Join(fields, " ")
	if len([]rune(normalized)) <= 3000 {
		return normalized
	}
	return string([]rune(normalized)[:3000])
}

func (m *imageTaskManager) newTask(req createImageTaskRequest) (*imageTask, error) {
	id := firstNonEmpty(strings.TrimSpace(req.TaskID), strings.TrimSpace(req.TurnID))
	if id == "" {
		id = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	prompt := strings.TrimSpace(req.Prompt)
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "generate"
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	if count > 8 {
		count = 8
	}
	sourceImages := make([]imageTaskSourceImage, 0, len(req.SourceImages))
	for index, source := range req.SourceImages {
		sourceImages = append(sourceImages, imageTaskSourceImage{
			ID:      firstNonEmpty(strings.TrimSpace(source.ID), fmt.Sprintf("%s-source-%d", id, index)),
			Role:    firstNonEmpty(strings.TrimSpace(source.Role), "image"),
			Name:    firstNonEmpty(strings.TrimSpace(source.Name), fmt.Sprintf("source-%d.png", index+1)),
			DataURL: strings.TrimSpace(source.DataURL),
			URL:     strings.TrimSpace(source.URL),
		})
	}

	referenceImages := make([]imageTaskSourceImage, 0, len(req.ReferenceImages))
	for index, source := range req.ReferenceImages {
		referenceImages = append(referenceImages, imageTaskSourceImage{
			ID:      firstNonEmpty(strings.TrimSpace(source.ID), fmt.Sprintf("%s-reference-%d", id, index)),
			Role:    "image",
			Name:    firstNonEmpty(strings.TrimSpace(source.Name), fmt.Sprintf("reference-%d.png", index+1)),
			DataURL: strings.TrimSpace(source.DataURL),
			URL:     strings.TrimSpace(source.URL),
		})
	}

	var sourceReference *imageTaskSourceReference
	if req.SourceReference != nil {
		sourceReference = &imageTaskSourceReference{
			OriginalFileID:  strings.TrimSpace(req.SourceReference.OriginalFileID),
			OriginalGenID:   strings.TrimSpace(req.SourceReference.OriginalGenID),
			ConversationID:  strings.TrimSpace(req.SourceReference.ConversationID),
			ParentMessageID: strings.TrimSpace(req.SourceReference.ParentMessageID),
			ResponseID:      strings.TrimSpace(req.SourceReference.ResponseID),
			SourceAccountID: strings.TrimSpace(req.SourceReference.SourceAccountID),
		}
	}

	var contextReference *imageTaskContextReference
	if req.ContextReference != nil {
		contextReference = &imageTaskContextReference{
			ConversationID:  strings.TrimSpace(req.ContextReference.ConversationID),
			ParentMessageID: strings.TrimSpace(req.ContextReference.ParentMessageID),
			ResponseID:      strings.TrimSpace(req.ContextReference.ResponseID),
			SourceAccountID: strings.TrimSpace(req.ContextReference.SourceAccountID),
		}
	}

	resolutionAccess := strings.ToLower(strings.TrimSpace(req.ResolutionAccess))
	requirePaid := m.server.configuredImageMode() == "studio" &&
		(resolutionAccess == "paid" || requiresPaidGenerateTask(req.Size))
	requirement := imageTaskRequirement{
		NeedPaid:        requirePaid,
		SourceAccountID: "",
	}
	if sourceReference != nil && sourceReference.SourceAccountID != "" {
		requirement.SourceAccountID = sourceReference.SourceAccountID
		requirement.NeedPaid = false
	} else if req.Policy != nil && req.Policy.Enabled {
		normalized := req.Policy.Normalize()
		requirement.PolicySnapshot = &normalized
	}

	commonSystemHint := strings.TrimSpace(firstNonEmpty(req.CommonSystemHint, m.server.cfg.ImageCommonSystemHint()))
	privateSystemHint := ""
	if req.PrivatePhotoMode {
		privateSystemHint = m.server.cfg.ImagePrivateSystemHint()
	}
	privateSystemHint = strings.TrimSpace(firstNonEmpty(req.PrivateSystemHint, privateSystemHint, req.SystemHint))

	createdAt := time.Now().UTC()
	images := make([]imagehistory.Image, 0, count)
	units := make([]imageTaskUnit, 0, count)
	for index := 0; index < count; index++ {
		images = append(images, imagehistory.Image{
			ID:     fmt.Sprintf("%s-%d", id, index),
			Status: "loading",
		})
		units = append(units, imageTaskUnit{
			Index:     index,
			Status:    imageTaskStatusQueued,
			Attempted: map[string]struct{}{},
		})
	}

	return &imageTask{
		ID:                  id,
		UserID:              strings.TrimSpace(req.UserID),
		Username:            strings.TrimSpace(req.Username),
		ConversationID:      strings.TrimSpace(req.ConversationID),
		TurnID:              strings.TrimSpace(req.TurnID),
		Source:              firstNonEmpty(strings.TrimSpace(req.Source), "workspace"),
		Mode:                mode,
		Prompt:              prompt,
		Model:               normalizeRequestedImageModel(req.Model, m.server.cfg.ChatGPT.Model),
		Count:               count,
		RetryImageIndex:     req.RetryImageIndex,
		Size:                strings.TrimSpace(req.Size),
		ResolutionAccess:    resolutionAccess,
		Quality:             strings.TrimSpace(req.Quality),
		Background:          strings.TrimSpace(req.Background),
		ResponseFormat:      "url",
		PrivatePhotoMode:    req.PrivatePhotoMode,
		CommonSystemHint:    commonSystemHint,
		PrivateSystemHint:   privateSystemHint,
		SystemHint:          privateSystemHint,
		ConversationContext: normalizeImageConversationContext(req.ConversationContext),
		RequestBaseURL:      strings.TrimRight(strings.TrimSpace(req.RequestBaseURL), "/"),
		SourceImages:        sourceImages,
		ReferenceImages:     referenceImages,
		SourceReference:     sourceReference,
		ContextReference:    contextReference,
		Requirement:         requirement,
		CreatedAt:           createdAt,
		Status:              imageTaskStatusQueued,
		Images:              images,
		Units:               units,
	}, nil
}

func imageTaskUnitAttemptedTokens(task *imageTask, unitIndex int) map[string]struct{} {
	if task == nil || unitIndex < 0 || unitIndex >= len(task.Units) || len(task.Units[unitIndex].Attempted) == 0 {
		return nil
	}
	attempted := make(map[string]struct{}, len(task.Units[unitIndex].Attempted))
	for token := range task.Units[unitIndex].Attempted {
		attempted[token] = struct{}{}
	}
	return attempted
}

func imageTaskUnitRememberAttempt(unit *imageTaskUnit, accessToken string) {
	token := strings.TrimSpace(accessToken)
	if unit == nil || token == "" {
		return
	}
	if unit.Attempted == nil {
		unit.Attempted = map[string]struct{}{}
	}
	unit.Attempted[token] = struct{}{}
}

func imageTaskUnitClearAttempts(task *imageTask, unitIndex int) {
	if task == nil || unitIndex < 0 || unitIndex >= len(task.Units) {
		return
	}
	task.Units[unitIndex].Attempted = map[string]struct{}{}
}

func shouldRememberImageTaskFailedAttempt(err error) bool {
	return isImageAccountSwitchError(err)
}

func (m *imageTaskManager) nextQueuedUnitIndexLocked(task *imageTask) int {
	for index := range task.Units {
		if task.Units[index].Status == imageTaskStatusQueued {
			return index
		}
	}
	return -1
}

func (m *imageTaskManager) nextReadyQueuedUnitIndexLocked(task *imageTask, now time.Time) (int, time.Time) {
	return m.nextReadyQueuedUnitIndexFromSnapshot(task, now)
}

func (m *imageTaskManager) nextReadyQueuedUnitIndexFromSnapshot(task *imageTask, now time.Time) (int, time.Time) {
	earliestRetryAt := time.Time{}
	for index := range task.Units {
		unit := task.Units[index]
		if unit.Status != imageTaskStatusQueued {
			continue
		}
		if !unit.NextAttemptAt.IsZero() && unit.NextAttemptAt.After(now) {
			if earliestRetryAt.IsZero() || unit.NextAttemptAt.Before(earliestRetryAt) {
				earliestRetryAt = unit.NextAttemptAt
			}
			continue
		}
		return index, time.Time{}
	}
	return -1, earliestRetryAt
}

func (m *imageTaskManager) queueTTL() time.Duration {
	if m == nil || m.server == nil || m.server.cfg == nil {
		return 10 * time.Minute
	}
	ttl := m.server.cfg.ImageTaskQueueTTL()
	if ttl <= 0 {
		return 10 * time.Minute
	}
	return ttl
}

func (m *imageTaskManager) initialQueueExpiryAt(task *imageTask) time.Time {
	if task == nil || !task.StartedAt.IsZero() || task.Status != imageTaskStatusQueued {
		return time.Time{}
	}
	return task.CreatedAt.Add(m.queueTTL())
}

func (m *imageTaskManager) retentionDeadlineForTaskLocked(task *imageTask) time.Time {
	if task == nil || !isFinalImageTaskStatus(task.Status) || task.FinishedAt.IsZero() {
		return time.Time{}
	}
	return task.FinishedAt.Add(imageTaskRetentionAfterFinish)
}

func (m *imageTaskManager) expireQueuedTasksLocked(now time.Time) []*imageTaskView {
	expired := make([]*imageTaskView, 0)
	for _, id := range m.order {
		task := m.tasks[id]
		if task == nil || task.Status != imageTaskStatusQueued || !task.StartedAt.IsZero() {
			continue
		}
		expiresAt := m.initialQueueExpiryAt(task)
		if expiresAt.IsZero() || expiresAt.After(now) {
			continue
		}
		task.Status = imageTaskStatusExpired
		task.Error = "图片任务排队超时，请稍后重试"
		task.FinishedAt = now
		task.WaitingReason = imageTaskWaitingReasonNone
		task.Blockers = nil
		for index := range task.Units {
			if task.Units[index].Status == imageTaskStatusQueued {
				task.Units[index].Status = imageTaskStatusCancelled
				task.Units[index].FinishedAt = now
				task.Units[index].Error = task.Error
				task.Images[index].Status = "error"
				task.Images[index].Error = task.Error
			}
		}
		expired = append(expired, m.buildTaskViewLocked(task))
	}
	return expired
}

func (m *imageTaskManager) nextWakeAtLocked(now time.Time) time.Time {
	nextWakeAt := time.Time{}
	for _, id := range m.order {
		task := m.tasks[id]
		if task == nil || task.Status != imageTaskStatusQueued {
			continue
		}
		if retryAt := m.taskNextRetryAtLocked(task, now); !retryAt.IsZero() {
			if nextWakeAt.IsZero() || retryAt.Before(nextWakeAt) {
				nextWakeAt = retryAt
			}
		}
		if expiresAt := m.initialQueueExpiryAt(task); !expiresAt.IsZero() && expiresAt.After(now) {
			if nextWakeAt.IsZero() || expiresAt.Before(nextWakeAt) {
				nextWakeAt = expiresAt
			}
		}
	}
	return nextWakeAt
}

func (m *imageTaskManager) nextCleanupAtLocked(now time.Time) time.Time {
	nextCleanupAt := time.Time{}
	for _, id := range m.order {
		task := m.tasks[id]
		if task == nil {
			continue
		}
		cleanupAt := m.retentionDeadlineForTaskLocked(task)
		if cleanupAt.IsZero() || !cleanupAt.After(now) {
			continue
		}
		if nextCleanupAt.IsZero() || cleanupAt.Before(nextCleanupAt) {
			nextCleanupAt = cleanupAt
		}
	}
	return nextCleanupAt
}

func (m *imageTaskManager) nextMaintenanceAtLocked(now time.Time) time.Time {
	nextWakeAt := m.nextWakeAtLocked(now)
	nextCleanupAt := m.nextCleanupAtLocked(now)
	switch {
	case nextWakeAt.IsZero():
		return nextCleanupAt
	case nextCleanupAt.IsZero():
		return nextWakeAt
	case nextCleanupAt.Before(nextWakeAt):
		return nextCleanupAt
	default:
		return nextWakeAt
	}
}

func (m *imageTaskManager) pruneRetainedTasksLocked(now time.Time) []string {
	if len(m.order) == 0 {
		return nil
	}
	nextOrder := make([]string, 0, len(m.order))
	removed := make([]string, 0)
	for _, id := range m.order {
		task := m.tasks[id]
		if task == nil {
			continue
		}
		cleanupAt := m.retentionDeadlineForTaskLocked(task)
		if !cleanupAt.IsZero() && !cleanupAt.After(now) {
			delete(m.tasks, id)
			removed = append(removed, id)
			continue
		}
		nextOrder = append(nextOrder, id)
	}
	m.order = nextOrder
	return removed
}

func (m *imageTaskManager) taskNextRetryAtLocked(task *imageTask, now time.Time) time.Time {
	_, retryAt := m.nextReadyQueuedUnitIndexLocked(task, now)
	return retryAt
}

func (m *imageTaskManager) maxRunningLocked() int {
	maxRunning, _, _ := m.server.cfg.ImageQueueConfig()
	if maxRunning <= 0 {
		maxRunning = 1
	}
	return maxRunning
}

func (m *imageTaskManager) maxParallelUnitsForTaskLocked(task *imageTask) int {
	maxRunning := m.maxRunningLocked()
	if maxRunning <= 1 || task == nil || task.Count <= 1 || !m.taskSupportsParallelUnitsLocked(task) {
		return 1
	}
	if task.Count < maxRunning {
		return task.Count
	}
	return maxRunning
}

func (m *imageTaskManager) taskSupportsParallelUnitsLocked(task *imageTask) bool {
	return task != nil && m.preferredRouteForTask(task) == "responses"
}

func (m *imageTaskManager) canStartUnitForUserLocked(task *imageTask) bool {
	if task == nil {
		return false
	}
	if m.taskSupportsParallelUnitsLocked(task) {
		return true
	}
	userID := strings.TrimSpace(task.UserID)
	if userID == "" {
		return true
	}
	for _, other := range m.tasks {
		if other == nil || strings.TrimSpace(other.UserID) != userID || other.ActiveUnits <= 0 {
			continue
		}
		return false
	}
	return true
}

func (m *imageTaskManager) buildTaskViewLocked(task *imageTask) *imageTaskView {
	queuePosition := 0
	if task.Status == imageTaskStatusQueued {
		position := 1
		for _, id := range m.order {
			if id == task.ID {
				queuePosition = position
				break
			}
			candidate := m.tasks[id]
			if candidate == nil {
				continue
			}
			for _, unit := range candidate.Units {
				if unit.Status == imageTaskStatusQueued {
					position++
				}
			}
		}
	}

	view := &imageTaskView{
		ID:              task.ID,
		UserID:          task.UserID,
		ConversationID:  task.ConversationID,
		TurnID:          task.TurnID,
		Mode:            task.Mode,
		Status:          task.Status,
		CreatedAt:       task.CreatedAt.Format(time.RFC3339Nano),
		Count:           task.Count,
		RetryImageIndex: task.RetryImageIndex,
		QueuePosition:   queuePosition,
		WaitingReason:   task.WaitingReason,
		Blockers:        append([]imageTaskBlocker(nil), task.Blockers...),
		Images:          append([]imagehistory.Image(nil), task.Images...),
		Error:           task.Error,
		CancelRequested: task.CancelRequested,
	}
	if !task.StartedAt.IsZero() {
		view.StartedAt = task.StartedAt.Format(time.RFC3339Nano)
	}
	if !task.FinishedAt.IsZero() {
		view.FinishedAt = task.FinishedAt.Format(time.RFC3339Nano)
	}
	return view
}

func (m *imageTaskManager) snapshotLocked() *imageTaskSnapshot {
	return m.snapshotLockedForUser("")
}

func (m *imageTaskManager) snapshotLockedForUser(userID string) *imageTaskSnapshot {
	queued := 0
	total := 0
	activeSources := imageTaskSourceSnapshot{}
	finalStatuses := imageTaskFinalStatusSnapshot{}
	for _, id := range m.order {
		task := m.tasks[id]
		if task == nil || !imageTaskVisibleToUser(task, userID) {
			continue
		}
		total++
		queuedUnitsForTask := 0
		runningUnitsForTask := 0
		for _, unit := range task.Units {
			switch unit.Status {
			case imageTaskStatusQueued:
				queuedUnitsForTask++
			case imageTaskStatusRunning:
				runningUnitsForTask++
			}
		}
		queued += queuedUnitsForTask
		addImageTaskSourceCountN(
			&activeSources,
			task.Source,
			queuedUnitsForTask+runningUnitsForTask,
		)
		switch task.Status {
		case imageTaskStatusSucceeded:
			finalStatuses.Succeeded++
		case imageTaskStatusFailed:
			finalStatuses.Failed++
		case imageTaskStatusCancelled:
			finalStatuses.Cancelled++
		case imageTaskStatusExpired:
			finalStatuses.Expired++
		}
	}
	return &imageTaskSnapshot{
		Running:          runningUnitsForUser(m, userID),
		MaxRunning:       m.maxRunningLocked(),
		Queued:           queued,
		Total:            total,
		ActiveSources:    activeSources,
		FinalStatuses:    finalStatuses,
		RetentionSeconds: int(imageTaskRetentionAfterFinish / time.Second),
	}
}

func (m *imageTaskManager) subscriberChannelsLocked() []chan imageTaskEvent {
	channels := make([]chan imageTaskEvent, 0, len(m.subscribers))
	for _, ch := range m.subscribers {
		channels = append(channels, ch)
	}
	return channels
}

func imageTaskVisibleToUser(task *imageTask, userID string) bool {
	if strings.TrimSpace(userID) == "" {
		return true
	}
	return task != nil && strings.TrimSpace(task.UserID) == strings.TrimSpace(userID)
}

func runningUnitsForUser(m *imageTaskManager, userID string) int {
	if strings.TrimSpace(userID) == "" {
		return m.runningUnits
	}
	running := 0
	for _, task := range m.tasks {
		if !imageTaskVisibleToUser(task, userID) {
			continue
		}
		for _, unit := range task.Units {
			if unit.Status == imageTaskStatusRunning {
				running++
			}
		}
	}
	return running
}

func (m *imageTaskManager) broadcast(subscribers []chan imageTaskEvent, event imageTaskEvent) {
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (m *imageTaskManager) scheduleAfter(when time.Time) {
	if when.IsZero() {
		return
	}
	delay := time.Until(when)
	if delay < 0 {
		delay = 0
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.scheduleAt.IsZero() && !when.Before(m.scheduleAt) {
		return
	}
	if m.scheduleTimer != nil {
		m.scheduleTimer.Stop()
	}
	m.scheduleAt = when
	m.scheduleTimer = time.AfterFunc(delay, func() {
		m.mu.Lock()
		m.scheduleTimer = nil
		m.scheduleAt = time.Time{}
		m.mu.Unlock()
		m.triggerSchedule()
	})
}

func imageTaskRetryBackoffDuration(attempt int) time.Duration {
	if attempt <= 0 {
		return imageTaskRetryBackoffBase
	}
	backoff := imageTaskRetryBackoffBase
	for step := 1; step < attempt; step++ {
		backoff *= 2
		if backoff >= imageTaskRetryBackoffMax {
			return imageTaskRetryBackoffMax
		}
	}
	if backoff > imageTaskRetryBackoffMax {
		return imageTaskRetryBackoffMax
	}
	return backoff
}

func imageTaskRetryBackoffBlocker(now, nextAttemptAt time.Time) imageTaskBlocker {
	if nextAttemptAt.IsZero() {
		return imageTaskBlocker{
			Code:   string(imageTaskWaitingReasonRetryBackoff),
			Detail: "临时失败，稍后自动重试",
		}
	}
	waitFor := time.Until(nextAttemptAt)
	if !now.IsZero() {
		waitFor = nextAttemptAt.Sub(now)
	}
	if waitFor < time.Second {
		waitFor = time.Second
	}
	return imageTaskBlocker{
		Code:   string(imageTaskWaitingReasonRetryBackoff),
		Detail: fmt.Sprintf("临时失败，约 %s 后自动重试", formatRetryBackoff(waitFor)),
	}
}

func formatRetryBackoff(delay time.Duration) string {
	if delay < time.Second {
		return "1 秒"
	}
	seconds := int(delay.Round(time.Second) / time.Second)
	if seconds < 60 {
		return fmt.Sprintf("%d 秒", seconds)
	}
	minutes := seconds / 60
	remainSeconds := seconds % 60
	if remainSeconds == 0 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	return fmt.Sprintf("%d 分 %d 秒", minutes, remainSeconds)
}

func addImageTaskSourceCount(target *imageTaskSourceSnapshot, source string) {
	addImageTaskSourceCountN(target, source, 1)
}

func addImageTaskSourceCountN(target *imageTaskSourceSnapshot, source string, count int) {
	if target == nil {
		return
	}
	if count <= 0 {
		return
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "compat":
		target.Compat += count
	default:
		target.Workspace += count
	}
}

func sameImageTaskBlockers(left, right []imageTaskBlocker) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func requiresPaidGenerateTask(size string) bool {
	normalized := normalizeGenerateImageSize(size)
	return strings.EqualFold(normalized, "2048x2048") ||
		strings.EqualFold(normalized, "2880x2880") ||
		strings.EqualFold(normalized, "2048x1536") ||
		strings.EqualFold(normalized, "3264x2448") ||
		strings.EqualFold(normalized, "2160x1440") ||
		strings.EqualFold(normalized, "3456x2304") ||
		strings.EqualFold(normalized, "2560x1440") ||
		strings.EqualFold(normalized, "3840x2160") ||
		strings.EqualFold(normalized, "3360x1440") ||
		strings.EqualFold(normalized, "3808x1632") ||
		strings.EqualFold(normalized, "1440x2560") ||
		strings.EqualFold(normalized, "2160x3840")
}

func writeSSEEvent(w http.ResponseWriter, event imageTaskEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func isFinalImageTaskStatus(status imageTaskStatus) bool {
	switch status {
	case imageTaskStatusSucceeded, imageTaskStatusFailed, imageTaskStatusCancelled, imageTaskStatusExpired:
		return true
	default:
		return false
	}
}

func (m *imageTaskManager) logTaskDeferredError(task *imageTask, unitIndex int, deferredErr *imageTaskDeferredError) {
	if m == nil || m.server == nil || task == nil || deferredErr == nil {
		return
	}
	now := time.Now().UTC()
	accountLabel := firstNonEmpty(deferredErr.accountEmail, deferredErr.accountFile)
	errorMessage := deferredErr.Error()
	if accountLabel != "" {
		errorMessage = fmt.Sprintf("%s（账号：%s）", errorMessage, accountLabel)
	}
	entry := imageRequestLogEntry{
		ID:           fmt.Sprintf("%s-deferred-%d-%d", task.ID, unitIndex, task.Units[unitIndex].DeferredCount),
		StartedAt:    now.Format(time.RFC3339Nano),
		FinishedAt:   now.Format(time.RFC3339Nano),
		Endpoint:     "/api/image/tasks [deferred_retry #" + fmt.Sprintf("%d", task.Units[unitIndex].DeferredCount) + " for task " + task.ID + "]",
		Operation:    task.Mode,
		Route:        "deferred_retry",
		AccountEmail: deferredErr.accountEmail,
		AccountFile:  deferredErr.accountFile,
		UserID:       task.UserID,
		Username:     task.Username,
		Success:      false,
		Error:        errorMessage,
		ErrorCode:    inferErrorCode(deferredErr),
	}
	metadata := newImageRequestMetadata(task.Prompt, task.Size, task.Quality)
	metadata.applyTo(&entry)
	m.server.reqLogs.add(entry)
}
