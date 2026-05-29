"use client";

import { memo, useState } from "react";
import {
  Brush,
  Clock3,
  Copy,
  Download,
  LoaderCircle,
  RotateCcw,
  Sparkles,
  X,
} from "lucide-react";
import { toast } from "sonner";

import { AppImage as Image } from "@/components/app-image";
import { OriginalImagePreview } from "@/components/original-image-preview";
import type { ImageTaskView } from "@/lib/api";
import { cn } from "@/lib/utils";
import type {
  ImageConversationTurn,
  ImageMode,
  StoredImage,
} from "@/store/image-conversations";

import { formatImageErrorMessage } from "../submit-utils";
import {
  buildConversationSourceLabel,
  buildImageDataUrl,
  buildImageThumbnailUrl,
  buildSourceImageThumbnailUrl,
  buildSourceImageUrl,
} from "../view-utils";

type ActiveRequestState = {
  conversationId: string;
  turnId: string;
  mode: ImageMode;
  count: number;
  variant: "standard" | "selection-edit";
};

type ProcessingStatus = {
  title: string;
  detail: string;
};

function formatWaitingReason(reason?: string) {
  switch (String(reason || "").trim()) {
    case "global_concurrency":
      return "等待全局并发槽位";
    case "paid_account_busy":
      return "等待 Paid 图片账号空闲";
    case "compatible_account_busy":
      return "等待兼容图片账号空闲";
    case "source_account_busy":
      return "等待原始图片所属账号空闲";
    case "retry_backoff":
      return "任务临时失败，正在自动重试";
    default:
      return "等待可用图片账号或并发槽位";
  }
}

function formatTurnSizeLabel(size?: string) {
  return String(size || "")
    .trim()
    .replace("x", "X");
}

function buildDownloadName(createdAt: string, turnId: string, index: number) {
  const date = new Date(createdAt);
  const safeIndex = String(index + 1).padStart(2, "0");
  if (Number.isNaN(date.getTime())) {
    return `chatgpt-image-${turnId.slice(0, 8)}-${safeIndex}.png`;
  }

  const yyyy = String(date.getFullYear());
  const mm = String(date.getMonth() + 1).padStart(2, "0");
  const dd = String(date.getDate()).padStart(2, "0");
  const hh = String(date.getHours()).padStart(2, "0");
  const min = String(date.getMinutes()).padStart(2, "0");
  const sec = String(date.getSeconds()).padStart(2, "0");
  return `chatgpt-image-${yyyy}${mm}${dd}-${hh}${min}${sec}-${safeIndex}.png`;
}

async function copyPromptToClipboard(prompt: string) {
  const text = prompt.trim();
  if (!text) {
    toast.warning("没有可复制的提示词");
    return;
  }

  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      const input = document.createElement("textarea");
      input.value = text;
      input.setAttribute("readonly", "");
      input.style.position = "fixed";
      input.style.left = "-9999px";
      document.body.appendChild(input);
      input.select();
      document.execCommand("copy");
      document.body.removeChild(input);
    }
    toast.success("提示词已复制");
  } catch {
    toast.error("复制失败");
  }
}

type ConversationTurnsProps = {
  conversationId: string;
  turns: ImageConversationTurn[];
  modeLabelMap: Record<ImageMode, string>;
  activeRequest: ActiveRequestState | null;
  activeTaskByTurnId: Map<string, ImageTaskView>;
  cancellingTaskIds: string[];
  processingStatus: ProcessingStatus | null;
  waitingDots: string;
  submitElapsedSeconds: number;
  formatConversationTime: (value: string) => string;
  formatProcessingDuration: (seconds: number) => string;
  onOpenSelectionEditor: (
    conversationId: string,
    turnId: string,
    image: StoredImage,
    imageName: string,
  ) => void;
  onSeedFromResult: (
    conversationId: string,
    image: StoredImage,
    nextMode: ImageMode,
  ) => void;
  onRetryTurn: (
    conversationId: string,
    turn: ImageConversationTurn,
    imageIndex?: number,
  ) => Promise<void>;
  onDiagnoseTurn: (
    conversationId: string,
    turn: ImageConversationTurn,
  ) => Promise<void>;
  onRetryWithDiagnostic: (
    conversationId: string,
    turn: ImageConversationTurn,
  ) => Promise<void>;
  onCancelTurn: (
    conversationId: string,
    turn: ImageConversationTurn,
  ) => Promise<void>;
  onSelectEnhancedPrompt?: (
    conversationId: string,
    turn: ImageConversationTurn,
    prompt: string,
  ) => Promise<void>;
  onUpdateEnhancedPrompt?: (
    conversationId: string,
    turn: ImageConversationTurn,
    index: number,
    prompt: string,
  ) => void;
};

export const ConversationTurns = memo(function ConversationTurns({
  conversationId,
  turns,
  modeLabelMap,
  activeRequest,
  activeTaskByTurnId,
  cancellingTaskIds,
  processingStatus,
  waitingDots,
  submitElapsedSeconds,
  formatConversationTime,
  formatProcessingDuration,
  onOpenSelectionEditor,
  onSeedFromResult,
  onRetryTurn,
  onDiagnoseTurn,
  onRetryWithDiagnostic,
  onCancelTurn,
  onSelectEnhancedPrompt,
  onUpdateEnhancedPrompt,
}: ConversationTurnsProps) {
  const [diagnosingTurnIds, setDiagnosingTurnIds] = useState<string[]>([]);
  const [diagnosticRetryingTurnIds, setDiagnosticRetryingTurnIds] = useState<string[]>([]);

  const runDiagnoseTurn = async (turn: ImageConversationTurn) => {
    if (diagnosingTurnIds.includes(turn.id)) {
      return;
    }
    setDiagnosingTurnIds((current) => [...current, turn.id]);
    try {
      await onDiagnoseTurn(conversationId, turn);
    } finally {
      setDiagnosingTurnIds((current) => current.filter((id) => id !== turn.id));
    }
  };

  const runRetryWithDiagnostic = async (turn: ImageConversationTurn) => {
    if (diagnosticRetryingTurnIds.includes(turn.id)) {
      return;
    }
    setDiagnosticRetryingTurnIds((current) => [...current, turn.id]);
    try {
      await onRetryWithDiagnostic(conversationId, turn);
    } finally {
      setDiagnosticRetryingTurnIds((current) => current.filter((id) => id !== turn.id));
    }
  };

  return (
    <div className="mx-auto flex w-full max-w-[1120px] flex-col gap-8 px-4 pt-0 pb-8 sm:px-6 sm:py-8">
      {turns.map((turn) => {
        const turnProcessing = Boolean(
          activeRequest &&
          activeRequest.conversationId === conversationId &&
          activeRequest.turnId === turn.id,
        );
        const runtimeTask = activeTaskByTurnId.get(turn.id) ?? null;
        const cancelTaskId = runtimeTask?.id || "";
        const cancelPending = Boolean(
          cancelTaskId && cancellingTaskIds.includes(cancelTaskId),
        );
        const isCancelableTask =
          Boolean(cancelTaskId) &&
          runtimeTask?.status === "queued" ||
          Boolean(cancelTaskId) &&
          runtimeTask?.status === "running" ||
          Boolean(cancelTaskId) &&
          runtimeTask?.status === "cancel_requested";
        const cancelRequested =
          turn.cancelRequested ||
          runtimeTask?.cancelRequested ||
          runtimeTask?.status === "cancel_requested";
        const effectiveTaskStatus =
          runtimeTask?.status ||
          (cancelRequested
            ? "cancel_requested"
            : turn.status === "queued"
              ? "queued"
              : turn.status === "running" || turn.status === "generating"
                ? "running"
                : turn.status === "cancelled"
                  ? "cancelled"
                  : "");
        const showQueuedState = effectiveTaskStatus === "queued";
        const showRunningState =
          effectiveTaskStatus === "running" ||
          effectiveTaskStatus === "cancel_requested";
        const disableCancel =
          cancelPending || cancelRequested || !cancelTaskId;
        const cancelLabel =
          cancelPending || cancelRequested
            ? "取消中"
            : runtimeTask?.status === "running"
              ? "取消任务"
              : cancelTaskId
              ? "取消排队"
              : "准备中";
        const diagnosingTurn = diagnosingTurnIds.includes(turn.id);
        const diagnosticRunning =
          diagnosingTurn || turn.diagnosticStatus === "running";
        const diagnosticRetryingTurn = diagnosticRetryingTurnIds.includes(turn.id);
        const diagnosticReference = turn.diagnostic?.referenceImages?.find(
          (item) => item.dataUrl || item.url,
        );
        const showDiagnosticPanel =
          diagnosticRunning ||
          turn.diagnosticStatus === "failed" ||
          Boolean(turn.diagnostic);

        return (
          <div key={turn.id} className="space-y-4">
            <div className="flex justify-end">
              <div className="flex w-full max-w-[78%] flex-col items-end gap-4">
                {turn.sourceImages && turn.sourceImages.length > 0 ? (
                  <div className="flex flex-wrap justify-end gap-2.5">
                    {turn.sourceImages.map((source) => (
                      <div
                        key={source.id}
                        className="w-[136px] overflow-hidden rounded-[20px] border border-stone-200 bg-white shadow-sm"
                      >
                        <div className="border-b border-stone-100 px-3 py-2 text-left text-[11px] font-medium text-stone-500">
                          {buildConversationSourceLabel(source)}
                        </div>
                        <OriginalImagePreview
                          originalUrl={buildSourceImageUrl(source)}
                          title={source.name}
                          className="group relative block w-full text-left"
                        >
                          <Image
                            src={buildSourceImageThumbnailUrl(source)}
                            alt={source.name}
                            width={220}
                            height={160}
                            unoptimized
                            className="block h-24 w-full cursor-zoom-in bg-stone-50 object-contain"
                          />
                        </OriginalImagePreview>
                      </div>
                    ))}
                  </div>
                ) : null}
                <div className="group flex max-w-full flex-col items-start gap-1.5">
                  <div className="min-w-0 whitespace-pre-wrap break-words rounded-[28px] bg-[#f2f2f1] px-5 py-4 text-[15px] leading-7 text-stone-800 shadow-[inset_0_1px_0_rgba(255,255,255,0.75)]">
                    {turn.originalPrompt && turn.enhancedPrompt ? (
                      <div className="space-y-3">
                        <div>
                          <div className="mb-1 text-xs font-semibold text-stone-500">原始提示词</div>
                          <div>{turn.originalPrompt}</div>
                        </div>
                        <div className="border-t border-stone-200 pt-3">
                          <div className="mb-1 text-xs font-semibold text-sky-600">增强后提示词</div>
                          <div>{turn.enhancedPrompt}</div>
                        </div>
                      </div>
                    ) : (
                      turn.prompt || "无额外提示词"
                    )}
                  </div>
                  <button
                    type="button"
                    onClick={() =>
                      void copyPromptToClipboard(turn.prompt || "")
                    }
                    className="inline-flex h-7 shrink-0 items-center gap-1 rounded-full border border-stone-200 bg-white px-2.5 text-xs font-medium text-stone-500 opacity-0 shadow-sm transition hover:bg-stone-100 hover:text-stone-900 focus-visible:opacity-100 focus-visible:outline-none group-hover:opacity-100"
                    title="复制提示词"
                    aria-label="复制提示词"
                  >
                    <Copy className="size-3.5" />
                    复制
                  </button>
                </div>
              </div>
            </div>

            <div className="space-y-4">
              <div className="flex items-center gap-3 px-1">
                <span className="flex size-9 items-center justify-center rounded-2xl bg-stone-950 text-white">
                  <Sparkles className="size-4" />
                </span>
                <div>
                  <div className="text-sm font-semibold tracking-tight text-stone-900">
                    ChatGpt Image Studio
                  </div>
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-2 px-1 text-xs text-stone-500">
                <span className="rounded-full bg-stone-100 px-3 py-1.5">
                  {modeLabelMap[turn.mode]}
                </span>
                <span className="rounded-full bg-stone-100 px-3 py-1.5">
                  {turn.model}
                </span>
                <span className="rounded-full bg-stone-100 px-3 py-1.5">
                  {turn.count} 张
                </span>
                {cancelRequested ? (
                  <span className="rounded-full bg-rose-50 px-3 py-1.5 text-rose-700">
                    取消中
                  </span>
                ) : showQueuedState ? (
                  <span className="rounded-full bg-amber-50 px-3 py-1.5 text-amber-700">
                    排队中{turn.queuePosition ? ` · #${turn.queuePosition}` : ""}
                  </span>
                ) : null}
                {showRunningState ? (
                  <span className="rounded-full bg-sky-50 px-3 py-1.5 text-sky-700">
                    处理中
                  </span>
                ) : null}
                {effectiveTaskStatus === "cancelled" ? (
                  <span className="rounded-full bg-stone-200 px-3 py-1.5 text-stone-600">
                    已取消
                  </span>
                ) : null}
                {turn.size ? (
                  <span className="rounded-full bg-stone-100 px-3 py-1.5">
                    {formatTurnSizeLabel(turn.size)}
                  </span>
                ) : null}
                {turn.quality ? (
                  <span className="rounded-full bg-stone-100 px-3 py-1.5">
                    Quality {turn.quality}
                  </span>
                ) : null}
                {turn.scale ? (
                  <span className="rounded-full bg-stone-100 px-3 py-1.5">
                    {turn.scale}
                  </span>
                ) : null}
                <span className="rounded-full bg-stone-100 px-3 py-1.5">
                  <Clock3 className="mr-1 inline size-3.5" />
                  {formatConversationTime(turn.createdAt)}
                </span>
              </div>

              {turn.promptEnhanceStatus ? (
                <div className="rounded-[22px] border border-sky-100 bg-sky-50/70 p-4 text-sm text-sky-900 shadow-sm">
                  <div className="flex items-center gap-2 font-semibold">
                    {turn.promptEnhanceStatus === "thinking" ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : (
                      <Sparkles className="size-4" />
                    )}
                    {turn.promptEnhanceStatus === "thinking"
                      ? `思考中${waitingDots}`
                      : turn.promptEnhanceStatus === "selecting"
                      ? "选择或编辑一个提示词后生成"
                      : "思考失败"}
                  </div>
                  {turn.promptEnhanceStatus === "thinking" ? (
                    <p className="mt-2 text-xs leading-6 text-sky-700">
                      正在结合历史上下文、当前提示词和参考图理解意图，并使用 xhigh 思考强度生成增强提示词。
                    </p>
                  ) : null}
                  {turn.promptEnhanceStatus === "failed" ? (
                    <p className="mt-2 text-xs leading-6 text-rose-600">
                      {turn.promptEnhanceError || "生成增强提示词失败，请重试。"}
                    </p>
                  ) : null}
                  {turn.promptEnhanceStatus === "selecting" && turn.promptEnhanceOptions?.length ? (
                    <div className="mt-3 grid gap-2">
                      <p className="rounded-2xl bg-white/70 px-3 py-2 text-xs leading-5 text-sky-700">
                        下面每个候选都可以直接编辑：你可以先在文本框里修改细节、比例或风格，再点击「使用此提示词生成」。
                      </p>
                      {turn.promptEnhanceOptions.map((option, optionIndex) => (
                        <div
                          key={`${turn.id}-enhanced-${optionIndex}`}
                          className="rounded-2xl border border-sky-200 bg-white p-3 text-left text-xs leading-5 text-stone-700"
                        >
                          <div className="mb-2 font-semibold text-sky-700">方案 {optionIndex + 1} · 可编辑</div>
                          <textarea
                            value={option}
                            onChange={(event) => onUpdateEnhancedPrompt?.(conversationId, turn, optionIndex, event.target.value)}
                            className="min-h-24 w-full resize-y rounded-xl border border-stone-200 bg-stone-50 px-3 py-2 text-xs leading-5 text-stone-700 outline-none transition focus:border-sky-300 focus:bg-white"
                          />
                          <button
                            type="button"
                            className="mt-2 inline-flex h-8 items-center rounded-full bg-sky-600 px-3 text-xs font-semibold text-white transition hover:bg-sky-700 disabled:cursor-not-allowed disabled:opacity-60"
                            onClick={() => void onSelectEnhancedPrompt?.(conversationId, turn, option)}
                            disabled={!onSelectEnhancedPrompt || !option.trim()}
                          >
                            使用此提示词生成
                          </button>
                        </div>
                      ))}
                    </div>
                  ) : null}
                </div>
              ) : null}

              {turn.images.length > 0 && (!turn.promptEnhanceStatus || turn.promptEnhanceStatus === "thinking") ? (
                <div
                  className={cn(
                    "grid gap-4",
                    turn.images.length === 1
                      ? "grid-cols-1"
                      : "grid-cols-1 lg:grid-cols-2",
                  )}
                >
                  {turn.images.map((image, index) => {
                    const imageDataUrl = buildImageDataUrl(image);
                    const imageThumbUrl = buildImageThumbnailUrl(image);
                    const downloadName = buildDownloadName(
                      turn.createdAt,
                      turn.id,
                      index,
                    );

                    return (
                      <div
                        key={image.id}
                        className={cn(
                          "overflow-hidden rounded-[22px] border border-stone-200 bg-white shadow-sm",
                          image.status === "success" &&
                            "w-fit max-w-[75%] justify-self-start",
                          image.status !== "success" &&
                            "w-full max-w-[270px] justify-self-start",
                        )}
                      >
                        {image.status === "success" && imageDataUrl ? (
                          <div>
                            <OriginalImagePreview
                              originalUrl={imageDataUrl}
                              title={`Generated result ${index + 1}`}
                              className="group relative flex items-center justify-center bg-white/40 backdrop-blur-sm text-left"
                            >
                              <Image
                                src={imageThumbUrl}
                                alt={`Generated result ${index + 1}`}
                                width={360}
                                height={360}
                                unoptimized
                                className="block h-auto max-h-[270px] w-auto max-w-full cursor-zoom-in"
                              />
                            </OriginalImagePreview>
                            <div className="flex flex-wrap items-center gap-2 border-t border-stone-100 px-4 py-3">
                              <button
                                type="button"
                                className="inline-flex size-9 items-center justify-center rounded-full border border-stone-200 bg-white text-stone-600 transition hover:bg-stone-100 hover:text-stone-900"
                                onClick={() =>
                                  onOpenSelectionEditor(
                                    conversationId,
                                    turn.id,
                                    image,
                                    downloadName,
                                  )
                                }
                                title="选区"
                                aria-label="选区"
                              >
                                <Brush className="size-4" />
                              </button>
                              <button
                                type="button"
                                className="inline-flex size-9 items-center justify-center rounded-full border border-stone-200 bg-white text-stone-600 transition hover:bg-stone-100 hover:text-stone-900"
                                onClick={() =>
                                  onSeedFromResult(
                                    conversationId,
                                    image,
                                    "edit",
                                  )
                                }
                                title="引用"
                                aria-label="引用"
                              >
                                <Copy className="size-4" />
                              </button>
                              <a
                                href={imageDataUrl}
                                download={downloadName}
                                className="inline-flex size-9 items-center justify-center rounded-full border border-stone-200 bg-white text-stone-600 transition hover:bg-stone-100 hover:text-stone-900"
                                title="下载"
                                aria-label="下载"
                              >
                                <Download className="size-4" />
                              </a>
                            </div>
                          </div>
                        ) : image.status === "error" ? (
                          <div className="flex min-h-[320px] flex-col">
                            <div className="flex flex-1 items-center justify-center whitespace-pre-line bg-rose-50 px-6 py-8 text-center text-sm leading-7 text-rose-600">
                              {formatImageErrorMessage(
                                image.error || "处理失败",
                              )}
                            </div>
                            {showDiagnosticPanel ? (
                              <div className="space-y-3 border-t border-stone-100 bg-amber-50/60 px-4 py-3 text-left text-xs leading-6 text-stone-700">
                                <div className="flex items-center justify-between gap-3">
                                  <div className="font-semibold text-stone-900">拒绝诊断</div>
                                  <span className="inline-flex items-center gap-1 rounded-full bg-white/80 px-2.5 py-1 text-[11px] font-medium text-amber-700">
                                    {diagnosticRunning ? (
                                      <LoaderCircle className="size-3 animate-spin" />
                                    ) : null}
                                    {diagnosticRunning
                                      ? "诊断中"
                                      : turn.diagnosticStatus === "failed"
                                      ? "诊断失败"
                                      : "已诊断"}
                                  </span>
                                </div>
                                {diagnosticRunning ? (
                                  <div className="rounded-2xl bg-white/80 px-3 py-2 text-amber-700">
                                    {diagnosingTurn
                                      ? "正在分析拒绝原因，并尝试生成更安全的新提示词/参考图；完成后会直接显示在这张卡片上。"
                                      : "上次诊断未完成或页面已刷新，可以点击下方「重新诊断」。"}
                                  </div>
                                ) : null}
                                {turn.diagnosticStatus === "failed" ? (
                                  <div className="rounded-2xl bg-white/80 px-3 py-2 text-rose-600">
                                    {turn.diagnosticError || "诊断失败，请稍后重试。"}
                                  </div>
                                ) : null}
                                {turn.diagnostic ? (
                                  <>
                                    <div>
                                      <div className="font-semibold text-stone-900">诊断建议</div>
                                      <div>{turn.diagnostic.reason}</div>
                                    </div>
                                    <div>
                                      <div className="font-semibold text-stone-900">新提示词</div>
                                      <div className="line-clamp-4 whitespace-pre-wrap rounded-2xl bg-white/80 p-3 text-stone-700">
                                        {turn.diagnostic.revisedPrompt}
                                      </div>
                                    </div>
                                    {diagnosticReference ? (
                                      <div>
                                        <div className="font-semibold text-stone-900">新参考图</div>
                                        <Image
                                          src={diagnosticReference.dataUrl || diagnosticReference.url || ""}
                                          alt="Diagnostic reference"
                                          width={180}
                                          height={120}
                                          unoptimized
                                          className="mt-1 h-24 w-full rounded-2xl bg-white object-contain"
                                        />
                                      </div>
                                    ) : null}
                                    {turn.diagnostic.omitOriginalReferences ? (
                                      <div className="rounded-2xl bg-white/80 px-3 py-2 text-amber-700">
                                        已判断原参考图可能触发拒绝，引用重试时不会携带失败参考图。
                                      </div>
                                    ) : null}
                                    <div className="rounded-2xl bg-white/80 px-3 py-2 text-stone-600">
                                      使用方式：确认新提示词后点下方「引用重试」，系统会用诊断后的提示词和参考图重新提交。
                                      {turn.diagnosticUpdatedAt ? ` · ${formatConversationTime(turn.diagnosticUpdatedAt)}` : ""}
                                    </div>
                                  </>
                                ) : null}
                              </div>
                            ) : null}
                            <div className="flex flex-wrap items-center gap-2 border-t border-stone-100 px-4 py-3">
                              <button
                                type="button"
                                className="inline-flex size-9 items-center justify-center rounded-full border border-stone-200 bg-white text-rose-600 transition hover:bg-rose-50 hover:text-rose-700 disabled:cursor-not-allowed disabled:opacity-60"
                                onClick={() =>
                                  void onRetryTurn(conversationId, turn, index)
                                }
                                disabled={turnProcessing}
                                title={turnProcessing ? "处理中" : "重试"}
                                aria-label="重试"
                              >
                                <RotateCcw className="size-4" />
                              </button>
                              <button
                                type="button"
                                className="inline-flex h-9 items-center gap-1.5 rounded-full border border-amber-200 bg-white px-3 text-xs font-medium text-amber-700 transition hover:bg-amber-50 disabled:cursor-not-allowed disabled:opacity-60"
                                onClick={() => void runDiagnoseTurn(turn)}
                                disabled={turnProcessing || diagnosingTurn}
                                title={diagnosingTurn ? "诊断中" : "诊断拒绝原因"}
                                aria-label="诊断拒绝原因"
                              >
                                {diagnosingTurn ? (
                                  <LoaderCircle className="size-3.5 animate-spin" />
                                ) : (
                                  <Sparkles className="size-3.5" />
                                )}
                                {diagnosingTurn ? "诊断中" : turn.diagnostic ? "重新诊断" : "诊断"}
                              </button>
                              {turn.diagnostic ? (
                                <button
                                  type="button"
                                  className="inline-flex h-9 items-center gap-1.5 rounded-full border border-stone-900 bg-stone-950 px-3 text-xs font-medium text-white transition hover:bg-stone-800 disabled:cursor-not-allowed disabled:opacity-60"
                                  onClick={() => void runRetryWithDiagnostic(turn)}
                                  disabled={turnProcessing || diagnosticRetryingTurn}
                                  title="引用诊断建议重试"
                                  aria-label="引用诊断建议重试"
                                >
                                  <RotateCcw className="size-3.5" />
                                  {diagnosticRetryingTurn ? "重试中" : "引用重试"}
                                </button>
                              ) : null}
                            </div>
                          </div>
                        ) : (
                          <div className="flex min-h-[320px] flex-col items-center justify-center gap-3 bg-stone-50 px-6 py-8 text-center text-stone-500">
                            <div className="rounded-full bg-white p-3 shadow-sm">
                              <LoaderCircle className="size-5 animate-spin" />
                            </div>
                            <p className="text-sm font-medium text-stone-700">
                              {turn.promptEnhanceStatus === "thinking"
                                ? `思考中${waitingDots}`
                                : cancelRequested
                                ? "正在取消任务"
                                : showQueuedState
                                ? "已加入等候队列"
                                : turnProcessing && processingStatus
                                ? `${processingStatus.title}${waitingDots}`
                                : "正在处理图片..."}
                            </p>
                            <p className="text-xs leading-6 text-stone-400">
                              {turn.promptEnhanceStatus === "thinking"
                                ? "正在结合上下文和参考图生成增强提示词，完成后会自动提交或让你选择方案"
                                : cancelRequested
                                ? "正在等待当前请求结束，取消后将丢弃本次结果"
                                : showQueuedState
                                ? `${turn.waitingDetail || formatWaitingReason(turn.waitingReason)}${(turn.queuePosition ?? 0) > 1 ? ` · 前面还有 ${turn.queuePosition! - 1} 个` : ""}`
                                : turnProcessing && processingStatus
                                ? `${processingStatus.detail} · 已等待 ${formatProcessingDuration(submitElapsedSeconds)}`
                                : "图片处理通常需要几分钟，请稍候"}
                            </p>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              ) : null}
              {isCancelableTask ? (
                <div className="flex px-1">
                  <button
                    type="button"
                    onClick={() => void onCancelTurn(conversationId, turn)}
                    disabled={disableCancel}
                    className="inline-flex items-center gap-1 rounded-full border border-rose-200 bg-white px-3 py-2 text-sm font-medium text-rose-600 transition hover:bg-rose-50 hover:text-rose-700 disabled:cursor-not-allowed disabled:border-stone-200 disabled:text-stone-400 disabled:hover:bg-white disabled:hover:text-stone-400"
                    title={cancelLabel}
                    aria-label={cancelLabel}
                  >
                    <X className="size-4" />
                    {cancelLabel}
                  </button>
                </div>
              ) : null}
            </div>
          </div>
        );
      })}
    </div>
  );
});

ConversationTurns.displayName = "ConversationTurns";
