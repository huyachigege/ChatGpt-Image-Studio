package api

import (
	"encoding/json"
	"net/http"
	"time"

	"chatgpt2api/internal/users"
)

func imageTaskScopeUserID(identity authIdentity) string {
	if identity.Role == users.RoleAdmin {
		return ""
	}
	return identity.UserID
}

func (s *Server) handleCreateImageTask(w http.ResponseWriter, r *http.Request) {
	if s.imageTasks == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "image task manager is unavailable"})
		return
	}
	identity := identityFromContext(r.Context())
	var body createImageTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	body.UserID = identity.UserID
	body.Username = identity.Username
	body.RequestBaseURL = requestPublicBaseURL(r)
	body = s.normalizeCreateImageTaskRequest(r.Context(), body)
	if quotaErr := s.ensureUserImageQuotaAvailable(r.Context(), imageTaskQuotaKind(body.Mode, body.ResolutionAccess, body.Size), imageTaskQuotaCount(body.Count)); quotaErr != nil {
		writeImageRequestError(w, quotaErr)
		return
	}
	task, err := s.imageTasks.createTask(body)
	if err != nil {
		writeImageRequestError(w, err)
		return
	}
	_, snapshot := s.imageTasks.listTasksForUser(imageTaskScopeUserID(identity))
	writeJSON(w, http.StatusOK, map[string]any{
		"task":     task,
		"snapshot": snapshot,
	})
}

func (s *Server) handleListImageTasks(w http.ResponseWriter, r *http.Request) {
	if s.imageTasks == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "image task manager is unavailable"})
		return
	}
	identity := identityFromContext(r.Context())
	items, snapshot := s.imageTasks.listTasksForUser(imageTaskScopeUserID(identity))
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"snapshot": snapshot,
	})
}

func (s *Server) handleGetImageTask(w http.ResponseWriter, r *http.Request) {
	if s.imageTasks == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "image task manager is unavailable"})
		return
	}
	identity := identityFromContext(r.Context())
	task, snapshot, err := s.imageTasks.getTaskForUser(r.PathValue("id"), imageTaskScopeUserID(identity))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":     task,
		"snapshot": snapshot,
	})
}

func (s *Server) handleCancelImageTask(w http.ResponseWriter, r *http.Request) {
	if s.imageTasks == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "image task manager is unavailable"})
		return
	}
	identity := identityFromContext(r.Context())
	task, err := s.imageTasks.cancelTaskForUser(r.PathValue("id"), imageTaskScopeUserID(identity))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	_, snapshot := s.imageTasks.listTasksForUser(imageTaskScopeUserID(identity))
	writeJSON(w, http.StatusOK, map[string]any{
		"task":     task,
		"snapshot": snapshot,
	})
}

func (s *Server) handleImageTaskSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.imageTasks == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "image task manager is unavailable"})
		return
	}
	identity := identityFromContext(r.Context())
	_, snapshot := s.imageTasks.listTasksForUser(imageTaskScopeUserID(identity))
	writeJSON(w, http.StatusOK, map[string]any{"snapshot": snapshot})
}

func (s *Server) handleImageTaskStream(w http.ResponseWriter, r *http.Request) {
	if s.imageTasks == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "image task manager is unavailable"})
		return
	}
	identity := identityFromContext(r.Context())
	scopeUserID := imageTaskScopeUserID(identity)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	subID, ch := s.imageTasks.subscribe()
	defer s.imageTasks.unsubscribe(subID)

	items, snapshot := s.imageTasks.listTasksForUser(scopeUserID)
	initialPayload := map[string]any{
		"items":    items,
		"snapshot": snapshot,
	}
	raw, err := json.Marshal(initialPayload)
	if err == nil {
		_, _ = w.Write([]byte("event: init\n"))
		_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, writeErr := w.Write([]byte(": heartbeat\n\n")); writeErr != nil {
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case event, ok := <-ch:
			if !ok {
				return
			}
			if identity.Role != users.RoleAdmin && event.Task != nil && event.Task.UserID != identity.UserID {
				continue
			}
			if event.Task == nil && event.Snapshot != nil {
				_, snapshot := s.imageTasks.listTasksForUser(scopeUserID)
				event.Snapshot = snapshot
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
		}
	}
}
