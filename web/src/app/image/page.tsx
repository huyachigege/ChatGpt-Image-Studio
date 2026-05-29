"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronsDown } from "lucide-react";
import { useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { ImageEditModal } from "@/components/image-edit-modal";
import {
  cancelImageTask,
  consumeImageTaskStream,
  enhanceImagePrompt,
  fetchImageQuota,
  listFavorites,
  listImageTasks,
  setFavorite,
  type ImageTaskSnapshot,
  type ImageTaskView,
  type ImageQuality,
} from "@/lib/api";
import { cn } from "@/lib/utils";
import { getStoredAuthUser } from "@/store/auth";
import {
  normalizeConversation,
  saveImageConversation,
  updateImageConversation,
  type ImageConversationStatus,
  type ImageConversation,
  type ImageConversationTurn,
  type ImageMode,
  type StoredImage,
  type StoredSourceImage,
} from "@/store/image-conversations";
import { ConversationTurns } from "./components/conversation-turns";
import { EmptyState } from "./components/empty-state";
import { HistorySidebar } from "./components/history-sidebar";
import { PromptComposer } from "./components/prompt-composer";
import {
  buildActiveRequestState,
  buildEmptyTaskSnapshot,
  deriveTaskSnapshotFromItems,
  type ActiveRequestState,
  reduceTaskItems,
  selectConversationActiveTask,
} from "./task-runtime";
import { WorkspaceHeader } from "./components/workspace-header";
import { imagePromptPresets, type ImagePromptPreset } from "./prompt-presets";
import { useImageHistory } from "./hooks/use-image-history";
import { useImageSourceInputs } from "./hooks/use-image-source-inputs";
import { useImageSubmit } from "./hooks/use-image-submit";
import { buildConversationPreviewSource } from "./view-utils";
import {
  buildConversationTitle,
  buildImageConversationContext,
  buildImageConversationInput,
  createConversationTurn,
  createLoadingImages,
} from "./submit-utils";

type ImageAspectRatio = "auto" | "1:1" | "4:3" | "3:2" | "16:9" | "21:9" | "9:16";
type ImageResolutionTier = "auto-free" | "auto-paid" | "sd" | "2k" | "4k";
type ImageResolutionAccess = "free" | "paid";
type ImageWorkspaceRouteState = {
  mode?: ImageMode;
  prompt?: string;
  sourceImages?: StoredSourceImage[];
};
type ImageResolutionPreset = {
  tier: ImageResolutionTier;
  label: string;
  value: string;
  access: ImageResolutionAccess;
};

const imageAspectRatioOptions: Array<{
  label: string;
  value: ImageAspectRatio;
}> = [
  { label: "Auto", value: "auto" },
  { label: "1:1", value: "1:1" },
  { label: "4:3", value: "4:3" },
  { label: "3:2", value: "3:2" },
  { label: "16:9", value: "16:9" },
  { label: "21:9", value: "21:9" },
  { label: "9:16", value: "9:16" },
];

const imageAutoResolutionPresets: ImageResolutionPreset[] = [
  { tier: "auto-paid", label: "Paid（提示词指定）", value: "", access: "paid" },
  { tier: "auto-free", label: "Free（提示词指定）", value: "", access: "free" },
];

const imageResolutionPresets: Record<
  Exclude<ImageAspectRatio, "auto">,
  ImageResolutionPreset[]
> = {
  "1:1": [
    {
      tier: "4k",
      label: "Paid 高像素上限",
      value: "2880x2880",
      access: "paid",
    },
    { tier: "2k", label: "Paid 2K", value: "2048x2048", access: "paid" },
    { tier: "sd", label: "Free 实际档", value: "1248x1248", access: "free" },
  ],
  "4:3": [
    { tier: "4k", label: "Paid 高像素", value: "3264x2448", access: "paid" },
    { tier: "2k", label: "Paid 2K", value: "2048x1536", access: "paid" },
    { tier: "sd", label: "Free 实际档", value: "1440x1072", access: "free" },
  ],
  "3:2": [
    { tier: "4k", label: "Paid 高像素", value: "3456x2304", access: "paid" },
    { tier: "2k", label: "Paid 2K", value: "2160x1440", access: "paid" },
    { tier: "sd", label: "Free 实际档", value: "1536x1024", access: "free" },
  ],
  "16:9": [
    { tier: "4k", label: "Paid 4K", value: "3840x2160", access: "paid" },
    { tier: "2k", label: "Paid 2K", value: "2560x1440", access: "paid" },
    { tier: "sd", label: "Free 实际档", value: "1664x928", access: "free" },
  ],
  "21:9": [
    { tier: "4k", label: "Paid 高像素", value: "3808x1632", access: "paid" },
    { tier: "2k", label: "Paid 2K", value: "3360x1440", access: "paid" },
    { tier: "sd", label: "Free 实际档", value: "1904x816", access: "free" },
  ],
  "9:16": [
    { tier: "4k", label: "Paid 4K", value: "2160x3840", access: "paid" },
    { tier: "2k", label: "Paid 2K", value: "1440x2560", access: "paid" },
    { tier: "sd", label: "Free 实际档", value: "928x1664", access: "free" },
  ],
};

const modeOptions: Array<{
  label: string;
  value: ImageMode;
  description: string;
}> = [
  {
    label: "生成",
    value: "generate",
    description: "提示词生成新图，也可上传参考图辅助生成",
  },
  { label: "编辑", value: "edit", description: "上传图像后局部或整体改图" },
];
const imageQualityOptions: Array<{
  label: string;
  value: ImageQuality;
  description: string;
}> = [
  { label: "Low", value: "low", description: "低质量，速度更快，适合草稿测试" },
  {
    label: "Medium",
    value: "medium",
    description: "均衡质量与速度，适合日常生成",
  },
  {
    label: "High",
    value: "high",
    description: "高质量，耗时更长，适合最终出图",
  },
];

const modeLabelMap: Record<ImageMode, string> = {
  generate: "生成",
  edit: "编辑",
};

function formatResolutionLabel(value: string) {
  return value.replace("x", " x ");
}

function pickRandomPromptPresets() {
  return [...imagePromptPresets]
    .sort(() => Math.random() - 0.5)
    .slice(0, 4);
}

function formatConversationTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

async function normalizeConversationHistory(items: ImageConversation[]) {
  return items.map((item) => normalizeConversation(item));
}

function makeId() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function formatProcessingDuration(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes <= 0) {
    return `${seconds}s`;
  }
  return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
}

function buildWaitingDots(totalSeconds: number) {
  return ".".repeat((totalSeconds % 3) + 1);
}

function mapTaskStatusToTurnStatus(status: string): ImageConversationStatus {
  switch (status) {
    case "queued":
      return "queued";
    case "running":
    case "cancel_requested":
      return "running";
    case "cancelled":
      return "cancelled";
    case "failed":
    case "expired":
      return "error";
    case "succeeded":
      return "success";
    default:
      return "success";
  }
}

function mapTaskImagesToStoredImages(images: ImageTaskView["images"]): StoredImage[] {
  return images.map((image, index) => ({
    id: image.file_id || image.gen_id || `task-image-${index}`,
    status:
      image.error && !image.b64_json && !image.url
        ? "error"
        : image.b64_json || image.url
          ? "success"
          : "loading",
    b64_json: image.b64_json,
    url: image.url,
    revised_prompt: image.revised_prompt,
    file_id: image.file_id,
    gen_id: image.gen_id,
    conversation_id: image.conversation_id,
    parent_message_id: image.parent_message_id,
    response_id: image.response_id,
    source_account_id: image.source_account_id,
    error: image.error,
  }));
}

function mergeRetryImageResult(
  currentImages: StoredImage[],
  taskImages: StoredImage[],
  retryImageIndex: number,
) {
  if (retryImageIndex < 0) {
    return currentImages;
  }
  return currentImages.map((image, index) =>
    index === retryImageIndex ? (taskImages[0] ?? image) : image,
  );
}

function isActiveImageTaskStatus(status: string) {
  return (
    status === "queued" ||
    status === "running" ||
    status === "cancel_requested"
  );
}

function isFinalImageTaskStatus(status: string) {
  return (
    status === "succeeded" ||
    status === "failed" ||
    status === "cancelled" ||
    status === "expired"
  );
}

function selectPreferredTaskForTurn(
  turn: ImageConversationTurn,
  tasks: ImageTaskView[],
) {
  if (tasks.length === 0) {
    return null;
  }
  const boundTask = turn.taskId
    ? tasks.find((candidate) => candidate.id === turn.taskId) ?? null
    : null;
  if (boundTask && !isFinalImageTaskStatus(boundTask.status)) {
    return boundTask;
  }
  const latestActiveTask =
    tasks
      .filter((candidate) => isActiveImageTaskStatus(candidate.status))
      .sort((left, right) => right.createdAt.localeCompare(left.createdAt))[0] ??
    null;
  if (latestActiveTask) {
    return latestActiveTask;
  }
  const latestNonCancelledTask =
    [...tasks]
      .filter((candidate) => candidate.status !== "cancelled")
      .sort((left, right) => right.createdAt.localeCompare(left.createdAt))[0] ??
    null;
  if (latestNonCancelledTask) {
    return latestNonCancelledTask;
  }
  if (boundTask) {
    return boundTask;
  }
  return tasks[tasks.length - 1] ?? null;
}

function deriveTurnStatusFromImages(
  images: StoredImage[],
  taskStatus: string,
): ImageConversationStatus {
  if (images.some((image) => image.status === "loading")) {
    return taskStatus === "queued" ? "queued" : "running";
  }
  if (images.some((image) => image.status === "error")) {
    return "error";
  }
  if (images.length > 0 && images.every((image) => image.status === "success")) {
    return "success";
  }
  return mapTaskStatusToTurnStatus(taskStatus);
}

function applyTaskViewToConversation(
  conversation: ImageConversation,
  tasksByTurnKey: Map<string, ImageTaskView[]>,
) {
  const turns = (conversation.turns ?? []).map((turn) => {
    const tasks = tasksByTurnKey.get(`${conversation.id}:${turn.id}`) ?? [];
    const task = selectPreferredTaskForTurn(turn, tasks);
    if (!task) {
      return turn;
    }
    const mappedTaskImages =
      task.images.length > 0 ? mapTaskImagesToStoredImages(task.images) : [];
    const mergedImages =
      typeof task.retryImageIndex === "number"
        ? mergeRetryImageResult(turn.images, mappedTaskImages, task.retryImageIndex)
        : mappedTaskImages.length > 0
          ? mappedTaskImages
          : turn.images;
    const mergedStatus = deriveTurnStatusFromImages(mergedImages, task.status);
    const mergedError =
      mergedStatus === "error"
        ? task.error || turn.error
        : undefined;
    return {
      ...turn,
      taskId: task.id,
      status: mergedStatus,
      queuePosition: task.queuePosition,
      waitingReason: task.waitingReason,
      waitingDetail: task.blockers?.[0]?.detail,
      startedAt: task.startedAt,
      finishedAt: task.finishedAt,
      cancelRequested: task.cancelRequested,
      error: mergedError,
      images: mergedImages,
    };
  });
  return normalizeConversation({
    ...conversation,
    turns,
  });
}

function buildProcessingStatus(
  mode: ImageMode,
  elapsedSeconds: number,
  count: number,
  variant: ActiveRequestState["variant"],
) {
  if (mode === "generate") {
    if (elapsedSeconds < 4) {
      return {
        title: "正在提交生成请求",
        detail: `已进入图像生成队列，本次目标 ${count} 张`,
      };
    }
    if (elapsedSeconds < 12) {
      return {
        title: "正在排队创建画面",
        detail: "模型正在准备构图与风格细节",
      };
    }
    return {
      title: "模型正在生成图片",
      detail: "通常需要 1 到 5 分钟，请保持页面开启",
    };
  }

  if (mode === "edit") {
    if (elapsedSeconds < 4) {
      return {
        title:
          variant === "selection-edit"
            ? "正在提交选区编辑"
            : "正在提交编辑请求",
        detail: "请求已发送，正在准备处理素材",
      };
    }
    if (elapsedSeconds < 12) {
      return {
        title:
          variant === "selection-edit"
            ? "正在上传源图和选区"
            : "正在上传编辑素材",
        detail: "素材上传完成后会立即进入改图阶段",
      };
    }
    return {
      title:
        variant === "selection-edit"
          ? "模型正在按选区修改图片"
          : "模型正在编辑图片",
      detail: "通常需要 1 到 5 分钟，请保持页面开启",
    };
  }

  return {
    title: "模型正在编辑图片",
    detail: "通常需要 1 到 5 分钟，请保持页面开启",
  };
}

export default function ImagePage() {
  const location = useLocation();
  const { pathname } = location;
  const navigate = useNavigate();
  const didLoadQuotaRef = useRef(false);
  const quotaRefreshTaskStatesRef = useRef<Record<string, string>>({});
  const mountedRef = useRef(true);
  const draftSelectionRef = useRef(false);
  const uploadInputRef = useRef<HTMLInputElement | null>(null);
  const maskInputRef = useRef<HTMLInputElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const resultsViewportRef = useRef<HTMLDivElement | null>(null);
  const isNearBottomRef = useRef(true);
  const previousSelectedConversationIdRef = useRef<string | null>(null);
  const previousTurnCountRef = useRef(0);
  const previousLastTurnKeyRef = useRef("");

  const [mode, setMode] = useState<ImageMode>("generate");
  const [imagePrompt, setImagePrompt] = useState("");
  const [imageCount, setImageCount] = useState("1");
  const [imageAspectRatio, setImageAspectRatio] =
    useState<ImageAspectRatio>("auto");
  const [imageResolutionTier, setImageResolutionTier] =
    useState<ImageResolutionTier>("auto-paid");
  const [editResolutionAccess, setEditResolutionAccess] =
    useState<ImageResolutionAccess>("paid");
  const [imageQuality, setImageQuality] = useState<ImageQuality>("high");
  const [isEnhancingPrompt, setIsEnhancingPrompt] = useState(false);
  const [promptEnhanceError, setPromptEnhanceError] = useState("");
  const [thinkingModeEnabled, setThinkingModeEnabled] = useState(true);
  const [autoThinkingEnabled, setAutoThinkingEnabled] = useState(false);
  const [historyCollapsed, setHistoryCollapsed] = useState(false);
  const [isDesktopLayout, setIsDesktopLayout] = useState(() =>
    typeof window !== "undefined"
      ? window.matchMedia("(min-width: 1024px)").matches
      : false,
  );
  const [availableQuota, setAvailableQuota] = useState("Paid 30/30 · Free 120/120");
  const [paidQuotaRemaining, setPaidQuotaRemaining] = useState<number | null>(null);
  const [submitElapsedSeconds, setSubmitElapsedSeconds] = useState(0);
  const [showScrollToBottom, setShowScrollToBottom] = useState(false);
  const consecutiveSubmitRef = useRef(0);
  const lastSubmitTimeRef = useRef(0);
  const [privatePhotoMode, setPrivatePhotoMode] = useState(false);
  const [isMobileComposerCollapsed, setIsMobileComposerCollapsed] =
    useState(true);
  const [taskItems, setTaskItems] = useState<ImageTaskView[]>([]);
  const [cancellingTaskIds, setCancellingTaskIds] = useState<string[]>([]);
  const [taskSnapshot, setTaskSnapshot] = useState<ImageTaskSnapshot>(
    buildEmptyTaskSnapshot(),
  );
  const [showTaskStats, setShowTaskStats] = useState(false);
  const [inspirationExamples, setInspirationExamples] = useState<ImagePromptPreset[]>(() => pickRandomPromptPresets());
  const [favoritePromptPresetIds, setFavoritePromptPresetIds] = useState<Set<string>>(() => new Set());
  const persistedTaskStatesRef = useRef<Record<string, string>>({});
  const cancellingTaskIdsRef = useRef(new Set<string>());

  useEffect(() => {
    let cancelled = false;
    listFavorites("template")
      .then((payload) => {
        if (!cancelled) setFavoritePromptPresetIds(new Set(payload.items || []));
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  const activeTasks = useMemo(
    () =>
      taskItems.filter((task) =>
        ["queued", "running", "cancel_requested"].includes(task.status),
      ),
    [taskItems],
  );
  const activeTaskByTurnKey = useMemo(() => {
    const next = new Map<string, ImageTaskView>();
    for (const task of activeTasks) {
      const key = `${task.conversationId}:${task.turnId}`;
      const current = next.get(key);
      if (!current || current.createdAt.localeCompare(task.createdAt) < 0) {
        next.set(key, task);
      }
    }
    return next;
  }, [activeTasks]);
  const activeTaskById = useMemo(() => {
    const next = new Map<string, ImageTaskView>();
    for (const task of activeTasks) {
      next.set(task.id, task);
    }
    return next;
  }, [activeTasks]);
  const displayTaskSnapshot = useMemo(
    () => deriveTaskSnapshotFromItems(taskItems, taskSnapshot),
    [taskItems, taskSnapshot],
  );

  useEffect(() => {
    let cancelled = false;
    getStoredAuthUser().then((user) => {
      if (!cancelled) {
        setShowTaskStats(user?.role === "admin");
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const activeConversationIds = useMemo(
    () => new Set(activeTasks.map((task) => task.conversationId)),
    [activeTasks],
  );
  const preferredActiveConversationId = activeTasks[0]?.conversationId ?? null;
  const hasActiveTasks = activeTasks.length > 0;

  const {
    conversations,
    selectedConversationId,
    isLoadingHistory,
    setConversations,
    setSelectedConversationId,
    focusConversation,
    openDraftConversation,
    refreshHistory,
    handleCreateDraft,
    handleDeleteConversation,
    handleClearHistory,
  } = useImageHistory({
    normalizeHistory: normalizeConversationHistory,
    mountedRef,
    draftSelectionRef,
    activeConversationIds,
    preferredActiveConversationId,
  });
  const {
    sourceImages,
    setSourceImages,
    editorTarget,
    appendFiles,
    handlePromptPaste,
    removeSourceImage,
    seedFromResult,
    openSelectionEditor,
    openSourceSelectionEditor,
    closeSelectionEditor,
  } = useImageSourceInputs({
    mode,
    selectedConversationId,
    setMode,
    focusConversation,
    textareaRef,
    makeId,
  });
  const selectedConversationActiveTaskByTurnId = useMemo(() => {
    const next = new Map<string, ImageTaskView>();
    if (!selectedConversationId) {
      return next;
    }
    for (const [key, task] of activeTaskByTurnKey.entries()) {
      const prefix = `${selectedConversationId}:`;
      if (!key.startsWith(prefix)) {
        continue;
      }
      next.set(task.turnId, task);
    }
    return next;
  }, [activeTaskByTurnKey, selectedConversationId]);

  const displayedConversations = useMemo(() => {
    const tasksByTurnKey = new Map<string, ImageTaskView[]>();
    taskItems.forEach((task) => {
      const key = `${task.conversationId}:${task.turnId}`;
      const current = tasksByTurnKey.get(key) ?? [];
      current.push(task);
      current.sort((left, right) => left.createdAt.localeCompare(right.createdAt));
      tasksByTurnKey.set(key, current);
    });
    return conversations.map((conversation) =>
      applyTaskViewToConversation(conversation, tasksByTurnKey),
    );
  }, [conversations, taskItems]);
  const selectedConversation = useMemo(
    () =>
      displayedConversations.find((item) => item.id === selectedConversationId) ??
      null,
    [displayedConversations, selectedConversationId],
  );
  const currentImageView = useMemo<"history" | "workspace">(
    () => (pathname.endsWith("/workspace") ? "workspace" : "history"),
    [pathname],
  );
  const isStandaloneHistory =
    !isDesktopLayout && currentImageView === "history";
  const isStandaloneWorkspace =
    !isDesktopLayout && currentImageView === "workspace";
  const selectedConversationTurns = useMemo(
    () => selectedConversation?.turns ?? [],
    [selectedConversation],
  );
  const selectedConversationLastTurn = useMemo(
    () =>
      selectedConversationTurns[selectedConversationTurns.length - 1] ?? null,
    [selectedConversationTurns],
  );
  const selectedConversationLastTurnKey = useMemo(() => {
    if (!selectedConversationLastTurn) {
      return "";
    }
    const imageKey = selectedConversationLastTurn.images
      .map(
        (image) =>
          `${image.id}:${image.status ?? "loading"}:${image.error ?? ""}`,
      )
      .join("|");
    return `${selectedConversationLastTurn.id}:${selectedConversationLastTurn.status}:${imageKey}`;
  }, [selectedConversationLastTurn]);
  const activeRequestTask = useMemo(
    () => selectConversationActiveTask(activeTasks, selectedConversationId),
    [activeTasks, selectedConversationId],
  );
  const activeRequest = useMemo<ActiveRequestState | null>(
    () => buildActiveRequestState(activeRequestTask),
    [activeRequestTask],
  );
  const activeRequestStartedAt = useMemo(() => {
    const raw = activeRequestTask?.startedAt || activeRequestTask?.createdAt;
    if (!raw) {
      return null;
    }
    const timestamp = new Date(raw).getTime();
    return Number.isNaN(timestamp) ? null : timestamp;
  }, [activeRequestTask]);
  const parsedCount = useMemo(
    () => Math.max(1, Math.min(8, Number(imageCount) || 1)),
    [imageCount],
  );
  const currentResolutionPresets = useMemo(
    () =>
      imageAspectRatio === "auto"
        ? imageAutoResolutionPresets
        : imageResolutionPresets[imageAspectRatio],
    [imageAspectRatio],
  );
  const selectedResolutionPreset = useMemo(
    () =>
      currentResolutionPresets.find(
        (item) => item.tier === imageResolutionTier,
      ) ?? currentResolutionPresets[0],
    [currentResolutionPresets, imageResolutionTier],
  );
  const imageQualityDisabledReason = "当前模式支持自由选择清晰度。";
  const isImageQualityEnabled = true;
  const imageResolutionTierOptions = useMemo(
    () =>
      currentResolutionPresets.map((item) => ({
        label:
          imageAspectRatio === "auto"
            ? item.label
            : `${item.access === "paid" ? "Paid" : "Free"} ${formatResolutionLabel(item.value)}${item.access === "paid" ? `（${item.label.replace("Paid ", "")}）` : ""}`,
        value: item.tier,
        disabled: false,
      })),
    [currentResolutionPresets, imageAspectRatio],
  );
  const imageResolutionTierLabel = useMemo(
    () =>
      imageResolutionTierOptions.find(
        (item) => item.value === imageResolutionTier && !item.disabled,
      )?.label ??
      imageResolutionTierOptions.find((item) => !item.disabled)?.label ??
      "",
    [imageResolutionTier, imageResolutionTierOptions],
  );
  const imageSize = useMemo(
    () =>
      imageAspectRatio === "auto"
        ? ""
        : currentResolutionPresets.find((item) => item.tier === imageResolutionTier)
            ?.value ?? currentResolutionPresets[0].value,
    [currentResolutionPresets, imageAspectRatio, imageResolutionTier],
  );
  const imageResolutionAccess = useMemo<ImageResolutionAccess>(
    () => selectedResolutionPreset?.access ?? "free",
    [selectedResolutionPreset],
  );
  const editResolutionTierOptions = useMemo(
    () => [
      { label: "Paid 链路（默认）", value: "paid" },
      { label: "Free 链路", value: "free" },
    ],
    [],
  );
  const editResolutionTierLabel =
    editResolutionTierOptions.find((item) => item.value === editResolutionAccess)?.label ??
    "Paid 链路（默认）";
  const imageSources = useMemo(
    () => sourceImages.filter((item) => item.role === "image"),
    [sourceImages],
  );
  const maskSource = useMemo(
    () => sourceImages.find((item) => item.role === "mask") ?? null,
    [sourceImages],
  );
  const handleEnhancePrompt = useCallback(async () => {
    const trimmedPrompt = imagePrompt.trim();
    const referenceSources = sourceImages.filter((item) => item.role !== "mask");
    if (!trimmedPrompt && referenceSources.length === 0) {
      setPromptEnhanceError("请输入一句需求或先上传参考图");
      return;
    }
    setIsEnhancingPrompt(true);
    setPromptEnhanceError("");
    try {
      const result = await enhanceImagePrompt({
        prompt: trimmedPrompt,
        mode,
        size: selectedResolutionPreset?.value,
        quality: imageQuality,
        conversationContext: buildImageConversationContext(selectedConversationTurns),
        conversationInput: buildImageConversationInput(selectedConversationTurns),
        auto: autoThinkingEnabled,
        sourceImages,
      });
      const nextPrompt = result.prompt.trim();
      if (!nextPrompt) {
        throw new Error("模型没有返回提示词");
      }
      setImagePrompt(nextPrompt);
      textareaRef.current?.focus();
    } catch (error) {
      const message = error instanceof Error ? error.message : "生成详细提示词失败";
      setPromptEnhanceError(message);
      toast.error(message);
    } finally {
      setIsEnhancingPrompt(false);
    }
  }, [imagePrompt, imageQuality, mode, selectedConversationTurns, selectedResolutionPreset?.value, sourceImages]);
  const processingStatus = useMemo(
    () =>
      activeRequest
        ? buildProcessingStatus(
            activeRequest.mode,
            submitElapsedSeconds,
            activeRequest.count,
            activeRequest.variant,
          )
        : null,
    [activeRequest, submitElapsedSeconds],
  );
  const waitingDots = useMemo(
    () => buildWaitingDots(submitElapsedSeconds),
    [submitElapsedSeconds],
  );

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    const media = window.matchMedia("(min-width: 1024px)");
    const updateLayout = (matches: boolean) => {
      setIsDesktopLayout(matches);
    };

    updateLayout(media.matches);
    const handleChange = (event: MediaQueryListEvent) =>
      updateLayout(event.matches);
    if (typeof media.addEventListener === "function") {
      media.addEventListener("change", handleChange);
      return () => media.removeEventListener("change", handleChange);
    }

    media.addListener(handleChange);
    return () => media.removeListener(handleChange);
  }, []);

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      void refreshHistory({ normalize: true, withLoading: true });
    });

    return () => {
      window.cancelAnimationFrame(frame);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    let disposed = false;
    let reconnectTimer: number | null = null;
    let pollingTimer: number | null = null;
    let streamAbort: AbortController | null = null;

    const applyTaskPayload = (items: ImageTaskView[], snapshot: ImageTaskSnapshot) => {
      if (disposed) {
        return;
      }
      setTaskItems(items);
      setTaskSnapshot(snapshot);
    };

    const loadTasks = async () => {
      try {
        const payload = await listImageTasks();
        applyTaskPayload(payload.items, payload.snapshot);
      } catch {
        if (!disposed) {
          setTaskItems([]);
          setTaskSnapshot(buildEmptyTaskSnapshot());
        }
      }
    };

    const startPolling = () => {
      if (pollingTimer !== null) {
        return;
      }
      void loadTasks();
      pollingTimer = window.setInterval(() => {
        void loadTasks();
      }, 2000);
    };

    const stopPolling = () => {
      if (pollingTimer !== null) {
        window.clearInterval(pollingTimer);
        pollingTimer = null;
      }
    };

    const startStream = () => {
      streamAbort = new AbortController();
      void consumeImageTaskStream(
        {
          onInit: ({ items, snapshot }) => {
            stopPolling();
            applyTaskPayload(items, snapshot);
          },
          onEvent: (event) => {
            setTaskItems((prev) => reduceTaskItems(prev, event));
            if (event.snapshot) {
              setTaskSnapshot(event.snapshot);
            }
          },
        },
        streamAbort.signal,
      ).catch(() => {
        if (disposed) {
          return;
        }
        startPolling();
        reconnectTimer = window.setTimeout(() => {
          if (!disposed) {
            startStream();
          }
        }, 3000);
      });
    };

    startPolling();
    startStream();

    return () => {
      disposed = true;
      if (reconnectTimer !== null) {
        window.clearTimeout(reconnectTimer);
      }
      stopPolling();
      streamAbort?.abort();
    };
  }, []);

  const refreshUserQuota = useCallback(async () => {
    try {
      const payload = await fetchImageQuota();
      const quota = payload.item;
      setAvailableQuota(`Paid ${quota.paidRemaining}/${quota.paidLimit} · Free ${quota.freeRemaining}/${quota.freeLimit}`);
      setPaidQuotaRemaining(quota.paidRemaining);
    } catch {
      setAvailableQuota((prev) => (prev === "加载中" || !prev ? "Paid 30/30 · Free 120/120" : prev));
    }
  }, []);

  useEffect(() => {
    if (didLoadQuotaRef.current) {
      return;
    }
    didLoadQuotaRef.current = true;
    void refreshUserQuota();
  }, [refreshUserQuota]);

  useEffect(() => {
    const selectedPreset = currentResolutionPresets.find(
      (item) => item.tier === imageResolutionTier,
    );
    if (selectedPreset) {
      return;
    }
    const nextPreset = currentResolutionPresets[0];
    if (nextPreset && nextPreset.tier !== imageResolutionTier) {
      setImageResolutionTier(nextPreset.tier);
    }
  }, [currentResolutionPresets, imageResolutionTier]);

  useEffect(() => {
    let shouldRefresh = false;
    for (const task of taskItems) {
      if (!["succeeded", "failed", "cancelled", "expired"].includes(task.status)) {
        continue;
      }
      if (quotaRefreshTaskStatesRef.current[task.id] === task.status) {
        continue;
      }
      quotaRefreshTaskStatesRef.current[task.id] = task.status;
      shouldRefresh = true;
    }
    if (shouldRefresh) {
      void refreshUserQuota();
    }
  }, [refreshUserQuota, taskItems]);

  const scrollToBottom = useCallback(
    (behavior: ScrollBehavior = "smooth") => {
      if (isStandaloneWorkspace) {
        const scrollTarget = document.scrollingElement;
        if (!scrollTarget) {
          return;
        }

        window.scrollTo({
          top: scrollTarget.scrollHeight,
          behavior,
        });
        return;
      }

      const viewport = resultsViewportRef.current;
      if (!viewport) {
        return;
      }

      viewport.scrollTo({
        top: viewport.scrollHeight,
        behavior,
      });
    },
    [isStandaloneWorkspace],
  );

  useEffect(() => {
    if (isStandaloneWorkspace) {
      const updateScrollState = () => {
        const scrollTarget = document.scrollingElement;
        if (!scrollTarget) {
          return;
        }
        const scrollTop = window.scrollY || scrollTarget.scrollTop;
        const viewportHeight = window.innerHeight;
        const hiddenHeight =
          scrollTarget.scrollHeight - viewportHeight - scrollTop;
        const hasOverflow = scrollTarget.scrollHeight > viewportHeight + 24;
        const nearBottom = hiddenHeight <= 96;
        isNearBottomRef.current = nearBottom;
        setShowScrollToBottom(hasOverflow && !nearBottom);
      };

      updateScrollState();
      window.addEventListener("scroll", updateScrollState, { passive: true });
      window.addEventListener("resize", updateScrollState);

      return () => {
        window.removeEventListener("scroll", updateScrollState);
        window.removeEventListener("resize", updateScrollState);
      };
    }

    const viewport = resultsViewportRef.current;
    if (!viewport) {
      return;
    }

    const updateScrollState = () => {
      const hiddenHeight =
        viewport.scrollHeight - viewport.clientHeight - viewport.scrollTop;
      const hasOverflow = viewport.scrollHeight > viewport.clientHeight + 24;
      const nearBottom = hiddenHeight <= 96;
      isNearBottomRef.current = nearBottom;
      setShowScrollToBottom(hasOverflow && !nearBottom);
    };

    updateScrollState();
    viewport.addEventListener("scroll", updateScrollState, { passive: true });
    window.addEventListener("resize", updateScrollState);

    return () => {
      viewport.removeEventListener("scroll", updateScrollState);
      window.removeEventListener("resize", updateScrollState);
    };
  }, [
    isStandaloneWorkspace,
    selectedConversationId,
    selectedConversationTurns.length,
    selectedConversationLastTurnKey,
  ]);

  useEffect(() => {
    const conversationChanged =
      previousSelectedConversationIdRef.current !== selectedConversationId;
    const turnCountIncreased =
      selectedConversationTurns.length > previousTurnCountRef.current;
    const lastTurnChanged =
      previousLastTurnKeyRef.current !== selectedConversationLastTurnKey;

    previousSelectedConversationIdRef.current = selectedConversationId;
    previousTurnCountRef.current = selectedConversationTurns.length;
    previousLastTurnKeyRef.current = selectedConversationLastTurnKey;

    if (!selectedConversation && !hasActiveTasks) {
      return;
    }

    if (
      !conversationChanged &&
      !turnCountIncreased &&
      !(lastTurnChanged && isNearBottomRef.current)
    ) {
      return;
    }

    const frame = window.requestAnimationFrame(() => {
      scrollToBottom(conversationChanged ? "auto" : "smooth");
    });

    return () => {
      window.cancelAnimationFrame(frame);
    };
  }, [
    hasActiveTasks,
    scrollToBottom,
    selectedConversation,
    selectedConversationId,
    selectedConversationLastTurnKey,
    selectedConversationTurns.length,
  ]);

  useEffect(() => {
    if (!isStandaloneWorkspace || !selectedConversationId) {
      return;
    }

    const firstFrame = window.requestAnimationFrame(() => {
      const secondFrame = window.requestAnimationFrame(() => {
        scrollToBottom("auto");
      });
      return () => window.cancelAnimationFrame(secondFrame);
    });

    return () => {
      window.cancelAnimationFrame(firstFrame);
    };
  }, [isStandaloneWorkspace, scrollToBottom, selectedConversationId]);

  useEffect(() => {
    if (activeRequestStartedAt === null) {
      setSubmitElapsedSeconds(0);
      return;
    }

    const updateElapsed = () => {
      setSubmitElapsedSeconds(
        Math.max(0, Math.floor((Date.now() - activeRequestStartedAt) / 1000)),
      );
    };

    updateElapsed();
    const timer = window.setInterval(updateElapsed, 1000);
    return () => {
      window.clearInterval(timer);
    };
  }, [activeRequestStartedAt]);

  useEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) {
      return;
    }

    textarea.style.height = "auto";
    const maxHeight = Math.min(
      480,
      Math.max(260, Math.floor(window.innerHeight * 0.42)),
    );
    textarea.style.height = `${Math.min(textarea.scrollHeight, maxHeight)}px`;
  }, [imagePrompt, mode]);

  useEffect(() => {
    window.dispatchEvent(
      new CustomEvent("chatgpt-image-studio:mobile-workspace-title", {
        detail: { title: selectedConversation?.title ?? null },
      }),
    );
  }, [selectedConversation?.title]);

  const persistConversation = useCallback(
    async (conversation: ImageConversation) => {
      const normalizedConversation = normalizeConversation(conversation);
      if (mountedRef.current) {
        draftSelectionRef.current = false;
        setSelectedConversationId(normalizedConversation.id);
        setConversations((prev) => {
          const next = [
            normalizedConversation,
            ...prev.filter((item) => item.id !== normalizedConversation.id),
          ].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
          return next;
        });
      }
      await saveImageConversation(normalizedConversation);
    },
    [setConversations, setSelectedConversationId],
  );

  const updateConversation = useCallback(
    async (
      conversationId: string,
      updater: (current: ImageConversation | null) => ImageConversation,
    ) => {
      if (mountedRef.current) {
        setConversations((prev) => {
          const currentConversation =
            prev.find((item) => item.id === conversationId) ?? null;
          const optimisticConversation = normalizeConversation(
            updater(currentConversation),
          );
          const next = [
            optimisticConversation,
            ...prev.filter((item) => item.id !== conversationId),
          ].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
          return next;
        });
      }

      const nextConversation = await updateImageConversation(
        conversationId,
        updater,
      );
      if (!mountedRef.current) {
        return;
      }
      setConversations((prev) => {
        const next = [
          nextConversation,
          ...prev.filter((item) => item.id !== conversationId),
        ].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
        return next;
      });
    },
    [setConversations],
  );

  useEffect(() => {
    for (const task of taskItems) {
      if (!["succeeded", "failed", "cancelled", "expired"].includes(task.status)) {
        continue;
      }
      if (!task.conversationId.trim()) {
        continue;
      }
      if (!displayedConversations.some((item) => item.id === task.conversationId)) {
        continue;
      }
      if (persistedTaskStatesRef.current[task.id] === task.status) {
        continue;
      }
      persistedTaskStatesRef.current[task.id] = task.status;
      void updateConversation(task.conversationId, (current) => {
        if (!current) {
          return normalizeConversation({
            id: task.conversationId,
            title: "",
            mode: "generate",
            prompt: "",
            model: "gpt-image-2",
            count: task.count,
            images: [],
            createdAt: task.createdAt,
            status: "error",
            turns: [],
          } as ImageConversation);
        }
        return applyTaskViewToConversation(
          current,
          new Map([[`${task.conversationId}:${task.turnId}`, [task]]]),
        );
      });
    }
  }, [displayedConversations, taskItems, updateConversation]);

  const resetComposer = useCallback(
    (nextMode: ImageMode = mode) => {
      setMode(nextMode);
      setImagePrompt("");
      setImageCount("1");
      setSourceImages([]);
    },
    [mode, setSourceImages],
  );

  const openHistoryView = useCallback(() => {
    navigate("/image/history");
  }, [navigate]);

  const openWorkspaceView = useCallback(() => {
    navigate("/image/workspace");
  }, [navigate]);

  const handleCreateDraftAndOpenWorkspace = useCallback(() => {
    handleCreateDraft(resetComposer, textareaRef);
    openWorkspaceView();
  }, [handleCreateDraft, openWorkspaceView, resetComposer]);

  const handleFocusConversationAndOpenWorkspace = useCallback(
    (conversationId: string) => {
      focusConversation(conversationId);
      openWorkspaceView();
    },
    [focusConversation, openWorkspaceView],
  );

  const applyPromptExample = useCallback(
    (example: ImagePromptPreset) => {
      setMode("generate");
      setImageCount("1");
      setImagePrompt(example.prompt);
      openDraftConversation();
      setSourceImages([]);
      textareaRef.current?.focus();
    },
    [openDraftConversation, setSourceImages],
  );

  const refreshInspirationExamples = useCallback(() => {
    setInspirationExamples(pickRandomPromptPresets());
  }, []);

  const applyPromptPreset = useCallback(
    (preset: ImagePromptPreset) => {
      setMode("generate");
      setImagePrompt(preset.prompt);
      openDraftConversation();
      textareaRef.current?.focus();
    },
    [openDraftConversation],
  );

  const togglePromptPresetFavorite = useCallback((preset: ImagePromptPreset) => {
    const nextFavorite = !favoritePromptPresetIds.has(preset.id);
    setFavoritePromptPresetIds((current) => {
      const next = new Set(current);
      if (nextFavorite) next.add(preset.id);
      else next.delete(preset.id);
      return next;
    });
    setFavorite("template", preset.id, nextFavorite)
      .then(() => toast.success(nextFavorite ? "模板已收藏" : "已取消收藏"))
      .catch((error) => {
        setFavoritePromptPresetIds((current) => {
          const next = new Set(current);
          if (nextFavorite) next.delete(preset.id);
          else next.add(preset.id);
          return next;
        });
        toast.error(error instanceof Error ? error.message : "收藏模板失败");
      });
  }, [favoritePromptPresetIds]);

  useEffect(() => {
    const state = location.state as ImageWorkspaceRouteState | null;
    const prompt = state?.prompt?.trim() || "";
    const routeSourceImages = Array.isArray(state?.sourceImages) ? state.sourceImages : [];
    if (currentImageView !== "workspace" || (!prompt && routeSourceImages.length === 0 && !state?.mode)) {
      return;
    }
    setMode(state?.mode === "edit" ? "edit" : "generate");
    setImageCount("1");
    setImagePrompt(prompt);
    setSourceImages(routeSourceImages);
    openDraftConversation();
    textareaRef.current?.focus();
    navigate(pathname, { replace: true, state: null });
  }, [currentImageView, location.state, navigate, openDraftConversation, pathname, setSourceImages]);

  const {
    handleSelectionEditSubmit,
    handleRetryTurn,
    handleDiagnoseTurn,
    handleRetryWithDiagnostic,
    handlePreparedSubmit,
    handleSubmit: rawHandleSubmit,
  } = useImageSubmit({
      mode,
      imagePrompt,
      imageModel: "gpt-image-2",
      imageSources,
      maskSource,
      sourceImages,
      parsedCount,
      imageSize,
      imageResolutionAccess,
      editResolutionAccess,
      imageQuality,
      selectedConversationId,
      conversationTurns: selectedConversationTurns,
      editorTarget,
      makeId,
      focusConversation,
      closeSelectionEditor,
      setImagePrompt,
      setSourceImages,
      setSubmitElapsedSeconds,
      persistConversation,
      updateConversation,
      resetComposer,
      privatePhotoMode,
    });

  const handleThinkingSubmit = useCallback(async () => {
    const prompt = imagePrompt.trim();
    const referenceSources = sourceImages.filter((item) => item.role !== "mask");
    if (mode === "generate" && !prompt) {
      toast.error("请输入提示词");
      return;
    }
    if (mode === "edit" && referenceSources.length === 0) {
      toast.error("编辑模式至少需要一张源图");
      return;
    }
    if (mode === "edit" && !prompt) {
      toast.error("编辑模式需要提示词");
      return;
    }

    const conversationId = selectedConversationId ?? makeId();
    const turnId = makeId();
    const expectedCount = mode === "generate" ? parsedCount : 1;
    const now = new Date().toISOString();
    const thinkingTurn = createConversationTurn({
      turnId,
      title: buildConversationTitle(mode, prompt),
      mode,
      prompt,
      originalPrompt: prompt,
      model: "gpt-image-2",
      count: expectedCount,
      size: mode === "generate" ? imageSize : undefined,
      resolutionAccess: mode === "generate" ? imageResolutionAccess : editResolutionAccess,
      quality: imageQuality,
      sourceImages,
      images: createLoadingImages(expectedCount, turnId),
      createdAt: now,
      status: "generating",
    });
    const thinkingConversation = normalizeConversation({
      id: conversationId,
      title: thinkingTurn.title,
      mode: thinkingTurn.mode,
      prompt: thinkingTurn.prompt,
      model: thinkingTurn.model,
      count: thinkingTurn.count,
      size: thinkingTurn.size,
      resolutionAccess: thinkingTurn.resolutionAccess,
      quality: thinkingTurn.quality,
      sourceImages: thinkingTurn.sourceImages,
      images: thinkingTurn.images,
      createdAt: thinkingTurn.createdAt,
      status: thinkingTurn.status,
      turns: [{ ...thinkingTurn, promptEnhanceStatus: "thinking" }],
    });

    setSubmitElapsedSeconds(0);
    focusConversation(conversationId);
    setImagePrompt("");
    setSourceImages([]);

    try {
      if (selectedConversationId) {
        await updateConversation(conversationId, (current) => ({
          ...(current ?? thinkingConversation),
          turns: [...(current?.turns ?? []), thinkingConversation.turns![0]],
        }));
      } else {
        await persistConversation(thinkingConversation);
      }

      const result = await enhanceImagePrompt({
        prompt,
        mode,
        size: selectedResolutionPreset?.value,
        quality: imageQuality,
        conversationContext: buildImageConversationContext(selectedConversationTurns),
        conversationInput: buildImageConversationInput(selectedConversationTurns),
        auto: autoThinkingEnabled,
        sourceImages,
      });
      const prompts = (result.prompts?.length ? result.prompts : [result.prompt])
        .map((item) => item.trim())
        .filter(Boolean);
      if (prompts.length === 0) {
        throw new Error("模型没有返回提示词");
      }
      if (autoThinkingEnabled) {
        await handlePreparedSubmit(conversationId, thinkingConversation.turns![0], prompts[0]);
        return;
      }
      await updateConversation(conversationId, (current) => ({
        ...(current ?? thinkingConversation),
        turns: (current?.turns ?? thinkingConversation.turns ?? []).map((turn) =>
          turn.id === turnId
            ? {
                ...turn,
                promptEnhanceStatus: "selecting" as const,
                promptEnhanceOptions: prompts,
                images: [],
              }
            : turn,
        ),
      }));
    } catch (error) {
      const message = error instanceof Error ? error.message : "思考模式生成提示词失败";
      await updateConversation(conversationId, (current) => ({
        ...(current ?? thinkingConversation),
        turns: (current?.turns ?? thinkingConversation.turns ?? []).map((turn) =>
          turn.id === turnId
            ? {
                ...turn,
                promptEnhanceStatus: "failed" as const,
                promptEnhanceError: message,
                images: [],
              }
            : turn,
        ),
      }));
      toast.error(message);
    }
  }, [
    editResolutionAccess,
    focusConversation,
    handlePreparedSubmit,
    imagePrompt,
    imageQuality,
    imageResolutionAccess,
    imageSize,
    mode,
    parsedCount,
    persistConversation,
    selectedConversationId,
    selectedConversationTurns,
    selectedResolutionPreset?.value,
    setSourceImages,
    sourceImages,
    updateConversation,
    autoThinkingEnabled,
  ]);

  const handleSubmit = thinkingModeEnabled ? handleThinkingSubmit : rawHandleSubmit;

  const handleUpdateEnhancedPrompt = useCallback((
    conversationId: string,
    turn: ImageConversationTurn,
    index: number,
    prompt: string,
  ) => {
    void updateConversation(conversationId, (current) => ({
      ...(current ?? normalizeConversation({
        id: conversationId,
        title: turn.title,
        mode: turn.mode,
        prompt: turn.prompt,
        model: turn.model,
        count: turn.count,
        size: turn.size,
        resolutionAccess: turn.resolutionAccess,
        quality: turn.quality,
        sourceImages: turn.sourceImages,
        images: turn.images,
        createdAt: turn.createdAt,
        status: turn.status,
        turns: [turn],
      } as ImageConversation)),
      turns: (current?.turns ?? [turn]).map((item) =>
        item.id === turn.id
          ? {
              ...item,
              promptEnhanceOptions: (item.promptEnhanceOptions ?? []).map((option, optionIndex) =>
                optionIndex === index ? prompt : option,
              ),
            }
          : item,
      ),
    }));
  }, [updateConversation]);

  const handleSelectEnhancedPrompt = useCallback(async (
    conversationId: string,
    turn: ImageConversationTurn,
    prompt: string,
  ) => {
    const latestTurn = selectedConversation?.turns?.find((item) => item.id === turn.id) ?? turn;
    await handlePreparedSubmit(conversationId, latestTurn, prompt);
  }, [handlePreparedSubmit, selectedConversation?.turns]);

  const handleCancelTurn = useCallback(
    async (conversationId: string, turn: ImageConversationTurn) => {
      const runtimeTask =
        activeTaskByTurnKey.get(`${conversationId}:${turn.id}`) ??
        (turn.taskId ? activeTaskById.get(turn.taskId.trim()) : null) ??
        null;
      const taskId = runtimeTask?.id || "";
      if (!taskId) {
        toast.warning("任务还在创建中，请稍后再试");
        return;
      }
      if (cancellingTaskIdsRef.current.has(taskId)) {
        return;
      }

      cancellingTaskIdsRef.current.add(taskId);
      setCancellingTaskIds((prev) =>
        prev.includes(taskId) ? prev : [...prev, taskId],
      );

      try {
        const result = await cancelImageTask(taskId);
        setTaskItems((prev) =>
          reduceTaskItems(prev, {
            type: "task.upsert",
            task: result.task,
          }),
        );
        setTaskSnapshot(result.snapshot);
        toast.success(
          result.task.status === "cancel_requested"
            ? "已提交取消请求，等待当前执行结束"
            : "已取消排队任务",
        );
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "取消任务失败");
      } finally {
        cancellingTaskIdsRef.current.delete(taskId);
        setCancellingTaskIds((prev) => prev.filter((item) => item !== taskId));
      }
    },
    [activeTaskById, activeTaskByTurnKey],
  );

  const historyPanel = (
    <HistorySidebar
      conversations={displayedConversations}
      selectedConversationId={selectedConversationId}
      isLoadingHistory={isLoadingHistory}
      hasActiveTasks={hasActiveTasks}
      activeConversationIds={activeConversationIds}
      modeLabelMap={modeLabelMap}
      buildConversationPreviewSource={buildConversationPreviewSource}
      formatConversationTime={formatConversationTime}
      onCreateDraft={handleCreateDraftAndOpenWorkspace}
      onClearHistory={handleClearHistory}
      onFocusConversation={handleFocusConversationAndOpenWorkspace}
      onDeleteConversation={handleDeleteConversation}
      standalone={isStandaloneHistory}
    />
  );

  const workspacePanel = (
    <div
      className={cn(
        "order-1 flex flex-col overflow-visible lg:order-none lg:min-h-0 lg:overflow-hidden",
        isStandaloneWorkspace
          ? "rounded-none border-0 bg-transparent shadow-none"
          : "rounded-[30px] border border-stone-200 bg-white shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:shadow-[0_20px_60px_-36px_rgba(0,0,0,0.78)]",
      )}
    >
      <WorkspaceHeader
        historyCollapsed={historyCollapsed}
        selectedConversationTitle={selectedConversation?.title}
        runningCount={displayTaskSnapshot.running}
        maxRunningCount={displayTaskSnapshot.maxRunning}
        queuedCount={displayTaskSnapshot.queued}
        workspaceActiveCount={displayTaskSnapshot.activeSources.workspace}
        compatActiveCount={displayTaskSnapshot.activeSources.compat}
        cancelledCount={displayTaskSnapshot.finalStatuses.cancelled}
        expiredCount={displayTaskSnapshot.finalStatuses.expired}
        showTaskStats={showTaskStats}
        onToggleHistory={() => setHistoryCollapsed((current) => !current)}
        showHistoryToggle={!isStandaloneWorkspace}
      />

      <div
        className={cn(
          "relative min-h-[240px] lg:min-h-0 lg:flex-1",
          isStandaloneWorkspace ? "bg-transparent" : "bg-[#fcfcfb] dark:bg-[var(--studio-panel-soft)]",
        )}
      >
        <div
          ref={resultsViewportRef}
          className={cn(
            "hide-scrollbar min-h-[240px] overflow-visible lg:h-full lg:min-h-0 lg:overflow-y-auto lg:pb-0",
            isMobileComposerCollapsed
              ? "pb-[68px] sm:pb-[74px]"
              : "pb-[228px] sm:pb-[244px]",
          )}
        >
          {!selectedConversation ? (
            <EmptyState
              inspirationExamples={inspirationExamples}
              onApplyPromptExample={applyPromptExample}
              onRefreshExamples={refreshInspirationExamples}
            />
          ) : (
            <ConversationTurns
              conversationId={selectedConversation.id}
              turns={selectedConversationTurns}
              modeLabelMap={modeLabelMap}
              activeRequest={activeRequest}
              activeTaskByTurnId={selectedConversationActiveTaskByTurnId}
              cancellingTaskIds={cancellingTaskIds}
              processingStatus={processingStatus}
              waitingDots={waitingDots}
              submitElapsedSeconds={submitElapsedSeconds}
              formatConversationTime={formatConversationTime}
              formatProcessingDuration={formatProcessingDuration}
              onOpenSelectionEditor={openSelectionEditor}
              onSeedFromResult={seedFromResult}
              onRetryTurn={handleRetryTurn}
              onDiagnoseTurn={handleDiagnoseTurn}
              onRetryWithDiagnostic={handleRetryWithDiagnostic}
              onCancelTurn={handleCancelTurn}
              onSelectEnhancedPrompt={handleSelectEnhancedPrompt}
              onUpdateEnhancedPrompt={handleUpdateEnhancedPrompt}
            />
          )}
        </div>
        {showScrollToBottom ? (
          <button
            type="button"
            onClick={() => scrollToBottom("smooth")}
            className={cn(
              "absolute right-4 z-10 inline-flex size-11 items-center justify-center rounded-full border border-stone-200 bg-white/95 text-stone-700 shadow-lg shadow-stone-300/30 backdrop-blur transition hover:bg-white hover:text-stone-950 dark:border-[var(--studio-border)] dark:bg-[color:var(--studio-panel-soft)] dark:text-[var(--studio-text)] dark:shadow-black/40 dark:hover:bg-[var(--studio-panel-muted)] dark:hover:text-[var(--studio-text-strong)] sm:right-5 lg:bottom-5",
              isMobileComposerCollapsed
                ? "bottom-[52px] sm:bottom-[60px]"
                : "bottom-[150px] sm:bottom-[164px]",
            )}
            aria-label="滚动到底部"
            title="滚动到底部"
          >
            <ChevronsDown className="size-5" />
          </button>
        ) : null}
      </div>

      <PromptComposer
        mode={mode}
        modeOptions={modeOptions}
        imageCount={imageCount}
        imageAspectRatio={imageAspectRatio}
        imageAspectRatioOptions={imageAspectRatioOptions}
        imageResolutionTier={mode === "edit" ? editResolutionAccess : imageResolutionTier}
        imageResolutionTierLabel={mode === "edit" ? editResolutionTierLabel : imageResolutionTierLabel}
        imageResolutionTierOptions={mode === "edit" ? editResolutionTierOptions : imageResolutionTierOptions}
        imageQuality={imageQuality}
        imageQualityOptions={imageQualityOptions}
        imageQualityDisabled={!isImageQualityEnabled}
        imageQualityDisabledReason={imageQualityDisabledReason}
        availableQuota={availableQuota}
        sourceImages={sourceImages}
        imagePrompt={imagePrompt}
        promptPresets={imagePromptPresets}
        favoritePromptPresetIds={favoritePromptPresetIds}
        textareaRef={textareaRef}
        uploadInputRef={uploadInputRef}
        maskInputRef={maskInputRef}
        onModeChange={(value) => {
          if (value === "generate" && mode === "generate") {
            const now = Date.now();
            if (now - lastSubmitTimeRef.current < 10000) {
              consecutiveSubmitRef.current++;
            } else {
              consecutiveSubmitRef.current = 1;
            }
            lastSubmitTimeRef.current = now;
            if (consecutiveSubmitRef.current >= 5 && !privatePhotoMode) {
              setPrivatePhotoMode(true);
              toast("你懂的 😏", { duration: 2000 });
            }
          }
          setMode(value);
        }}
        onImageCountChange={setImageCount}
        onImageAspectRatioChange={(value) =>
          setImageAspectRatio(value as ImageAspectRatio)
        }
        onImageResolutionTierChange={(value) => {
          if (mode === "edit") {
            setEditResolutionAccess(value as ImageResolutionAccess);
            return;
          }
          setImageResolutionTier(value as ImageResolutionTier);
        }}
        onImageQualityChange={(value) => setImageQuality(value as ImageQuality)}
        onPromptChange={(value) => {
          setImagePrompt(value);
          if (promptEnhanceError) {
            setPromptEnhanceError("");
          }
        }}
        onPromptPaste={handlePromptPaste}
        isEnhancingPrompt={isEnhancingPrompt}
        promptEnhanceError={promptEnhanceError}
        thinkingModeEnabled={thinkingModeEnabled}
        autoThinkingEnabled={autoThinkingEnabled}
        onThinkingModeChange={setThinkingModeEnabled}
        onAutoThinkingChange={setAutoThinkingEnabled}
        onApplyPromptPreset={applyPromptPreset}
        onTogglePromptPresetFavorite={togglePromptPresetFavorite}
        onRemoveSourceImage={removeSourceImage}
        onOpenSourceSelectionEditor={openSourceSelectionEditor}
        onAppendFiles={appendFiles}
        onMobileCollapsedChange={setIsMobileComposerCollapsed}
        onSubmit={handleSubmit}
        privatePhotoMode={privatePhotoMode}
      />
    </div>
  );

  return (
    <section
      className={cn(
        "grid grid-cols-1 gap-3 lg:h-full lg:min-h-0",
        isStandaloneHistory || isStandaloneWorkspace
          ? "grid-rows-[auto]"
          : historyCollapsed
            ? "grid-rows-[auto] lg:grid-cols-[minmax(0,1fr)] lg:grid-rows-[minmax(0,1fr)]"
            : "grid-rows-[auto_auto] lg:grid-cols-[320px_minmax(0,1fr)] lg:grid-rows-[minmax(0,1fr)]",
      )}
    >
      {isStandaloneHistory ? historyPanel : null}
      {isStandaloneWorkspace ? workspacePanel : null}
      {!isStandaloneHistory && !isStandaloneWorkspace ? (
        <>
          {!historyCollapsed ? historyPanel : null}
          {workspacePanel}
        </>
      ) : null}

      <ImageEditModal
        key={editorTarget?.imageName || "image-edit-modal"}
        open={Boolean(editorTarget)}
        imageName={editorTarget?.imageName || "image.png"}
        imageSrc={editorTarget?.sourceDataUrl || ""}
        isSubmitting={false}
        allowOutputOptions={true}
        imageAspectRatio="auto"
        imageAspectRatioOptions={[{ label: "原比例", value: "auto" }]}
        imageResolutionTier={editResolutionAccess}
        imageResolutionTierOptions={editResolutionTierOptions}
        imageQuality={imageQuality}
        imageQualityOptions={imageQualityOptions}
        imageQualityDisabled={!isImageQualityEnabled}
        imageQualityDisabledReason={imageQualityDisabledReason}
        onImageAspectRatioChange={(value) =>
          setImageAspectRatio(value as ImageAspectRatio)
        }
        onImageResolutionTierChange={(value) =>
          setEditResolutionAccess(value as ImageResolutionAccess)
        }
        onImageQualityChange={(value) => setImageQuality(value as ImageQuality)}
        onClose={closeSelectionEditor}
        onSubmit={async (payload) => {
          await handleSelectionEditSubmit(payload);
        }}
      />
    </section>
  );
}
