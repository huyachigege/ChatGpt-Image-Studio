"use client";

import { useEffect, useRef, useState, type ClipboardEvent as ReactClipboardEvent, type RefObject } from "react";
import { createPortal } from "react-dom";
import { ArrowUp, Brush, ChevronDown, ImagePlus, Sparkles, Trash2 } from "lucide-react";

import { AppImage as Image } from "@/components/app-image";
import { OriginalImagePreview } from "@/components/original-image-preview";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { ImageQuality } from "@/lib/api";
import type { ImageMode, StoredSourceImage } from "@/store/image-conversations";
import { cn } from "@/lib/utils";
import type { ImagePromptPreset } from "../prompt-presets";
import { buildSourceImageUrl } from "../view-utils";

type PromptComposerProps = {
  mode: ImageMode;
  modeOptions: Array<{ label: string; value: ImageMode; description: string }>;
  imageCount: string;
  imageAspectRatio: string;
  imageAspectRatioOptions: Array<{ label: string; value: string }>;
  imageResolutionTier: string;
  imageResolutionTierLabel: string;
  imageResolutionTierOptions: Array<{ label: string; value: string; disabled?: boolean }>;
  imageQuality: ImageQuality;
  imageQualityOptions: Array<{ label: string; value: ImageQuality; description: string }>;
  imageQualityDisabled: boolean;
  imageQualityDisabledReason: string;
  availableQuota: string;
  sourceImages: StoredSourceImage[];
  imagePrompt: string;
  promptPresets?: ImagePromptPreset[];
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  uploadInputRef: RefObject<HTMLInputElement | null>;
  maskInputRef: RefObject<HTMLInputElement | null>;
  onModeChange: (mode: ImageMode) => void;
  onImageCountChange: (value: string) => void;
  onImageAspectRatioChange: (value: string) => void;
  onImageResolutionTierChange: (value: string) => void;
  onImageQualityChange: (value: string) => void;
  onPromptChange: (value: string) => void;
  onPromptPaste: (event: ReactClipboardEvent<Element>) => void;
  onApplyPromptPreset?: (preset: ImagePromptPreset) => void;
  onRemoveSourceImage: (id: string) => void;
  onOpenSourceSelectionEditor: (sourceImageId: string) => void;
  onAppendFiles: (files: FileList | null, role: "image" | "mask") => Promise<void>;
  onMobileCollapsedChange?: (collapsed: boolean) => void;
  onSubmit: () => Promise<void>;
};

export function PromptComposer({
  mode,
  modeOptions,
  imageCount,
  imageAspectRatio,
  imageAspectRatioOptions,
  imageResolutionTier,
  imageResolutionTierLabel,
  imageResolutionTierOptions,
  imageQuality,
  imageQualityOptions,
  imageQualityDisabled,
  imageQualityDisabledReason,
  availableQuota,
  sourceImages,
  imagePrompt,
  promptPresets = [],
  textareaRef,
  uploadInputRef,
  maskInputRef,
  onModeChange,
  onImageCountChange,
  onImageAspectRatioChange,
  onImageResolutionTierChange,
  onImageQualityChange,
  onPromptChange,
  onPromptPaste,
  onApplyPromptPreset,
  onRemoveSourceImage,
  onOpenSourceSelectionEditor,
  onAppendFiles,
  onMobileCollapsedChange,
  onSubmit,
}: PromptComposerProps) {
  const imageQualityLabel = imageQualityOptions.find((item) => item.value === imageQuality)?.label ?? imageQuality;
  const [isPresetPanelOpen, setIsPresetPanelOpen] = useState(false);
  const [presetQuery, setPresetQuery] = useState("");
  const [activePresetCategory, setActivePresetCategory] = useState("全部");
  const presetCategories = Array.from(new Set(promptPresets.map((preset) => preset.category).filter(Boolean)));
  const filteredPromptPresets = promptPresets
    .filter((preset) => activePresetCategory === "全部" || preset.category === activePresetCategory)
    .filter((preset) => {
      const query = presetQuery.trim().toLowerCase();
      if (!query) {
        return true;
      }
      return `${preset.title} ${preset.description} ${preset.prompt} ${preset.author} ${preset.category} ${preset.source}`.toLowerCase().includes(query);
    })
    .slice(0, 60);
  const showImageOutputControls = mode === "generate";
  const showImageQualityControl = mode === "edit" || mode === "generate";
  const imageQualityPrefix = "质量";
  const hasComposerContent = imagePrompt.trim().length > 0 || sourceImages.length > 0;
  const previousHasComposerContentRef = useRef(hasComposerContent);
  const [isMobileComposerExpanded, setIsMobileComposerExpanded] = useState(hasComposerContent);
  const isMobileComposerCollapsed = !isMobileComposerExpanded;
  const showMobileExpandedSections = !isMobileComposerCollapsed;

  useEffect(() => {
    if (hasComposerContent && !previousHasComposerContentRef.current) {
      setIsMobileComposerExpanded(true);
    } else if (!hasComposerContent && previousHasComposerContentRef.current) {
      setIsMobileComposerExpanded(false);
    }

    previousHasComposerContentRef.current = hasComposerContent;
  }, [hasComposerContent]);

  useEffect(() => {
    onMobileCollapsedChange?.(isMobileComposerCollapsed);
  }, [isMobileComposerCollapsed, onMobileCollapsedChange]);


  return (
    <div
      onPaste={onPromptPaste}
      className={cn(
        "fixed inset-x-0 bottom-0 z-30 px-3 backdrop-blur supports-[padding:max(0px)]:pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:px-4 lg:static lg:inset-auto lg:bottom-auto lg:z-20 lg:rounded-none lg:border-x-0 lg:border-b-0 lg:border-t lg:bg-white lg:px-5 lg:shadow-none dark:lg:border-[var(--studio-border)] dark:lg:bg-[var(--studio-panel-soft)]",
        isMobileComposerCollapsed
          ? "border-transparent bg-white/96 shadow-none dark:bg-[color:var(--studio-bg)]"
          : "rounded-[26px] border border-stone-200 bg-white/96 shadow-[0_18px_50px_-24px_rgba(15,23,42,0.35)] dark:border-[var(--studio-border)] dark:bg-[color:var(--studio-bg)] dark:shadow-[0_24px_70px_-30px_rgba(0,0,0,0.82)]",
        isMobileComposerCollapsed ? "py-1 sm:py-1.5" : "py-1 sm:py-1.5",
        "lg:border-stone-200 lg:bg-white lg:py-2 lg:shadow-none",
      )}
    >
      <div className="mx-auto flex w-full max-w-[1120px] flex-col gap-2.5 px-4 sm:px-6">
        <div
          className={cn(
            "flex-col gap-2.5 xl:flex-row xl:items-center xl:justify-between",
            showMobileExpandedSections ? "flex" : "hidden lg:flex",
          )}
        >
          <div className="flex min-w-0 items-center gap-2">
            <div className="hide-scrollbar min-w-0 -mx-1 overflow-x-auto px-1 xl:mx-0 xl:px-0">
              <div className="inline-flex min-w-max rounded-full bg-stone-100 p-1">
                {modeOptions.map((item) => (
                  <button
                    key={item.value}
                    type="button"
                    onClick={() => onModeChange(item.value)}
                    className={cn(
                      "rounded-full px-3 py-1.5 text-[13px] font-medium transition sm:px-4 sm:py-2 sm:text-sm",
                      mode === item.value
                        ? "bg-stone-950 text-white shadow-sm dark:bg-[var(--studio-accent-strong)] dark:text-[var(--studio-accent-foreground)]"
                        : "text-stone-600 hover:bg-stone-200 hover:text-stone-900 dark:text-[var(--studio-text)] dark:hover:bg-[var(--studio-panel-muted)] dark:hover:text-[var(--studio-text-strong)]",
                    )}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
            {isMobileComposerExpanded ? (
              <button
                type="button"
                className="inline-flex size-8 shrink-0 items-center justify-center rounded-full text-stone-400 transition hover:bg-stone-100 hover:text-stone-700 sm:hidden"
                onClick={(event) => {
                  event.stopPropagation();
                  setIsMobileComposerExpanded(false);
                  textareaRef.current?.blur();
                }}
                aria-label="收起输入框"
                title="收起输入框"
              >
                <ChevronDown className="size-4" />
              </button>
            ) : null}
          </div>

          <div className="hide-scrollbar -mx-1 flex items-center gap-1.5 overflow-x-auto px-1 pb-1 sm:mx-0 sm:gap-2 sm:px-0 sm:pb-0 xl:justify-end">
            {showImageOutputControls ? (
              <Select value={imageAspectRatio} onValueChange={onImageAspectRatioChange}>
                <SelectTrigger className="h-9 w-[84px] shrink-0 rounded-full border-stone-200 bg-white text-[13px] font-medium text-stone-700 shadow-none focus-visible:ring-0 sm:h-10 sm:w-[108px] sm:text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {imageAspectRatioOptions.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}

            {showImageOutputControls ? (
              <Select value={imageResolutionTier} onValueChange={onImageResolutionTierChange}>
                <SelectTrigger
                  className="h-9 w-[168px] shrink-0 rounded-full border-stone-200 bg-white text-[13px] font-medium text-stone-700 shadow-none focus-visible:ring-0 sm:h-10 sm:w-[238px] sm:text-sm"
                  title={imageResolutionTierLabel}
                >
                  <SelectValue>{imageResolutionTierLabel}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {imageResolutionTierOptions.map((item) => (
                    <SelectItem key={item.value} value={item.value} disabled={item.disabled}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}


            {showImageQualityControl ? (
              <Select value={imageQuality} onValueChange={onImageQualityChange} disabled={imageQualityDisabled}>
                <SelectTrigger
                  className={cn(
                    "h-10 w-[136px] shrink-0 rounded-full border-stone-200 bg-white text-sm font-medium text-stone-700 shadow-none focus-visible:ring-0",
                    "h-9 w-[108px] text-[13px] sm:h-10 sm:w-[136px] sm:text-sm",
                    imageQualityDisabled && "cursor-not-allowed bg-stone-50 text-stone-400 opacity-80",
                  )}
                  title={
                    imageQualityDisabled
                      ? imageQualityDisabledReason
                      : imageQualityOptions.find((item) => item.value === imageQuality)?.description
                  }
                >
                  <SelectValue>{`${imageQualityPrefix} ${imageQualityLabel}`}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {imageQualityOptions.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      <span title={item.description}>{imageQualityPrefix} {item.label}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}

            {mode === "generate" ? (
              <div className="flex shrink-0 items-center gap-1 rounded-full border border-stone-200 bg-white px-2 py-0.5 sm:gap-1.5 sm:px-2.5 sm:py-1">
                <span className="text-[13px] font-medium text-stone-700 sm:text-sm">张数</span>
                <Input
                  type="number"
                  min="1"
                  max="8"
                  step="1"
                  value={imageCount}
                  onChange={(event) => onImageCountChange(event.target.value)}
                  className="h-7 w-[36px] border-0 bg-transparent px-0 text-center text-[13px] font-medium text-stone-700 shadow-none focus-visible:ring-0 sm:h-8 sm:w-[42px] sm:text-sm"
                />
              </div>
            ) : null}

            <span className="shrink-0 rounded-full bg-stone-100 px-2.5 py-1.5 text-[11px] font-medium text-stone-600 dark:bg-[var(--studio-panel-muted)] dark:text-[var(--studio-text-muted)] sm:px-3 sm:py-2 sm:text-xs">
              今日剩余 {availableQuota}
            </span>
          </div>
        </div>

          <div
          className="overflow-hidden rounded-[24px] border border-stone-200 bg-[#fafaf9] shadow-[inset_0_1px_0_rgba(255,255,255,0.9)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] sm:rounded-[28px]"
          onClick={() => {
            setIsMobileComposerExpanded(true);
            textareaRef.current?.focus();
          }}
        >
          {sourceImages.length > 0 ? (
            <div
              className={cn(
                "hide-scrollbar gap-2 overflow-x-auto border-b border-stone-200 px-3 py-2 sm:gap-3 sm:px-4 sm:py-3",
                showMobileExpandedSections ? "flex" : "hidden lg:flex",
              )}
            >
              {sourceImages.map((item) => (
                <div
                  key={item.id}
                  className="w-[104px] shrink-0 overflow-hidden rounded-[16px] border border-stone-200 bg-white sm:w-[126px] sm:rounded-[18px]"
                >
                  <div className="flex items-center justify-between border-b border-stone-100 px-3 py-2 text-[11px] font-medium text-stone-500">
                    <span>{item.role === "mask" ? "遮罩" : "源图"}</span>
                    <div className="flex items-center gap-1">
                      {mode === "edit" && item.role === "image" ? (
                        <button
                          type="button"
                          onClick={(event) => {
                            event.stopPropagation();
                            onOpenSourceSelectionEditor(item.id);
                          }}
                          className="rounded-md p-1 text-stone-400 transition hover:bg-stone-100 hover:text-stone-700"
                          title="选区编辑"
                          aria-label="选区编辑"
                        >
                          <Brush className="size-3.5" />
                        </button>
                      ) : null}
                      <button
                        type="button"
                        onClick={(event) => {
                          event.stopPropagation();
                          onRemoveSourceImage(item.id);
                        }}
                        className="rounded-md p-1 text-stone-400 transition hover:bg-stone-100 hover:text-rose-500"
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    </div>
                  </div>
                  <OriginalImagePreview
                    originalUrl={buildSourceImageUrl(item)}
                    title={item.name}
                    className="block w-full text-left"
                  >
                    <Image
                      src={buildSourceImageUrl(item)}
                      alt={item.name}
                      width={160}
                      height={110}
                      unoptimized
                      className="block h-16 w-full cursor-zoom-in bg-stone-50 object-contain sm:h-20"
                    />
                  </OriginalImagePreview>
                </div>
              ))}
            </div>
          ) : null}

          <div className="relative px-3 pb-1.5 pt-2 sm:px-4 sm:pb-2 sm:pt-2.5">
            {isMobileComposerCollapsed ? (
              <>
                <button
                  type="button"
                  className="flex min-h-[22px] w-full items-center px-1 py-0 text-left text-[14px] leading-5 text-stone-400 sm:hidden"
                  onClick={() => {
                    setIsMobileComposerExpanded(true);
                    textareaRef.current?.focus();
                  }}
                >
                  <span className="block w-full truncate">
                    {imagePrompt.trim() ||
                      (mode === "generate"
                        ? "描述你想生成的画面，也可以先上传参考图"
                        : "描述你想如何修改当前图片")}
                  </span>
                </button>
                <Textarea
                  ref={textareaRef}
                  value={imagePrompt}
                  onChange={(event) => onPromptChange(event.target.value)}
                  placeholder={
                    mode === "generate"
                      ? "描述你想生成的画面，也可以先上传参考图"
                      : mode === "edit"
                        ? "描述你想如何修改当前图片"
                        : "可选：描述你想增强的方向"
                  }
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && !event.shiftKey) {
                      event.preventDefault();
                      void onSubmit();
                    }
                  }}
                  className="hidden resize-none border-0 bg-transparent !px-1 !pb-1 text-[14px] text-stone-900 shadow-none placeholder:text-stone-400 focus-visible:ring-0 sm:block sm:min-h-[38px] sm:max-h-[70px] sm:overflow-y-auto sm:!pt-1 sm:pr-10 sm:text-[15px] sm:leading-7"
                  onFocus={() => setIsMobileComposerExpanded(true)}
                />
              </>
            ) : (
              <Textarea
                ref={textareaRef}
                value={imagePrompt}
                onChange={(event) => onPromptChange(event.target.value)}
                placeholder={
                  mode === "generate"
                    ? "描述你想生成的画面，也可以先上传参考图"
                    : mode === "edit"
                      ? "描述你想如何修改当前图片"
                      : "可选：描述你想增强的方向"
                }
                onKeyDown={(event) => {
                  if (event.key === "Enter" && !event.shiftKey) {
                    event.preventDefault();
                    void onSubmit();
                  }
                }}
                className="resize-none border-0 bg-transparent !px-1 !pb-1 text-[14px] text-stone-900 shadow-none placeholder:text-stone-400 focus-visible:ring-0 min-h-[30px] max-h-[70px] overflow-y-auto !pt-1 pr-10 leading-6 sm:min-h-[38px] sm:text-[15px] sm:leading-7"
                onFocus={() => setIsMobileComposerExpanded(true)}
              />
            )}
          </div>
          <div className={cn("relative px-3 pb-1.5 pt-2.5 sm:px-4 sm:pb-2.5 sm:pt-2.5", showMobileExpandedSections ? "block" : "hidden lg:block")}>
            {isPresetPanelOpen && typeof document !== "undefined" ? createPortal((
              <div
                className="fixed inset-0 z-50 flex items-end justify-center bg-stone-950/45 px-3 py-4 backdrop-blur-sm sm:items-start sm:p-6 sm:pt-10 lg:pt-8"
                onClick={(event) => {
                  event.stopPropagation();
                  setIsPresetPanelOpen(false);
                }}
              >
                <div
                  className="flex h-[88vh] w-full max-w-[1080px] flex-col overflow-hidden rounded-[28px] border border-stone-200 bg-white shadow-[0_28px_90px_-24px_rgba(15,23,42,0.55)] dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:h-[82vh]"
                  onClick={(event) => event.stopPropagation()}
                >
                  <div className="border-b border-stone-100 p-4 dark:border-[var(--studio-border)] sm:p-5">
                    <div className="flex items-start justify-between gap-4">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <div className="text-base font-semibold text-stone-950 dark:text-[var(--studio-text-strong)] sm:text-lg">提示词模板</div>
                          <span className="rounded-full bg-stone-950 px-2.5 py-1 text-[11px] font-semibold text-white dark:bg-[var(--studio-accent-strong)] dark:text-[var(--studio-accent-foreground)]">gpt-image-2</span>
                        </div>
                        <div className="mt-1 text-xs leading-5 text-stone-500 dark:text-[var(--studio-text-muted)] sm:text-sm">
                          来自 EvoLinkAI/awesome-gpt-image-2-API-and-Prompts，按分类浏览后可直接套用。
                        </div>
                      </div>
                      <button
                        type="button"
                        className="shrink-0 rounded-full px-3 py-1.5 text-xs font-medium text-stone-500 transition hover:bg-stone-100 hover:text-stone-900 dark:hover:bg-[var(--studio-panel-muted)] dark:hover:text-[var(--studio-text-strong)]"
                        onClick={() => setIsPresetPanelOpen(false)}
                      >
                        关闭
                      </button>
                    </div>
                    <div className="mt-4 flex flex-col gap-3 lg:flex-row lg:items-center">
                      <Input
                        value={presetQuery}
                        onChange={(event) => setPresetQuery(event.target.value)}
                        placeholder="搜索标题、提示词、分类、作者"
                        className="h-10 rounded-full border-stone-200 bg-stone-50 px-4 text-sm shadow-none focus-visible:ring-1 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-muted)]"
                      />
                      <div className="hide-scrollbar -mx-1 flex gap-2 overflow-x-auto px-1 lg:max-w-[520px]">
                        {["全部", ...presetCategories].map((category) => (
                          <button
                            key={category}
                            type="button"
                            className={cn(
                              "shrink-0 rounded-full px-3 py-2 text-xs font-medium transition",
                              activePresetCategory === category
                                ? "bg-stone-950 text-white dark:bg-[var(--studio-accent-strong)] dark:text-[var(--studio-accent-foreground)]"
                                : "bg-stone-100 text-stone-600 hover:bg-stone-200 hover:text-stone-900 dark:bg-[var(--studio-panel-muted)] dark:text-[var(--studio-text-muted)] dark:hover:text-[var(--studio-text-strong)]",
                            )}
                            onClick={() => setActivePresetCategory(category)}
                          >
                            {category}
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>
                  <div className="hide-scrollbar flex-1 overflow-y-auto p-3 sm:p-4">
                    {filteredPromptPresets.length > 0 ? (
                      <div className="grid gap-3 lg:grid-cols-2">
                        {filteredPromptPresets.map((preset) => (
                          <article
                            key={preset.id}
                            className="group overflow-hidden rounded-[22px] border border-stone-200 bg-stone-50/70 transition hover:border-stone-300 hover:bg-white hover:shadow-[0_18px_50px_-28px_rgba(15,23,42,0.45)] dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-muted)] dark:hover:bg-[var(--studio-panel)]"
                          >
                            <div className="flex gap-3 p-3 sm:gap-4 sm:p-4">
                              {preset.imageUrl ? (
                                <Image
                                  src={preset.imageUrl}
                                  alt={preset.title}
                                  width={156}
                                  height={112}
                                  unoptimized
                                  className="h-24 w-28 shrink-0 rounded-2xl bg-stone-100 object-cover sm:h-28 sm:w-36"
                                />
                              ) : null}
                              <div className="min-w-0 flex-1">
                                <div className="flex flex-wrap items-center gap-1.5">
                                  <span className="rounded-full bg-white px-2 py-1 text-[10px] font-semibold text-stone-500 dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]">{preset.category}</span>
                                  <span className="rounded-full bg-white px-2 py-1 text-[10px] font-semibold text-stone-500 dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]">{preset.model}</span>
                                </div>
                                <h3 className="mt-2 line-clamp-2 text-sm font-semibold leading-5 text-stone-950 dark:text-[var(--studio-text-strong)] sm:text-[15px]">{preset.title}</h3>
                                <div className="mt-1 truncate text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">by {preset.author}</div>
                              </div>
                            </div>
                            <div className="border-t border-stone-200/70 px-3 py-3 dark:border-[var(--studio-border)] sm:px-4">
                              <div className="max-h-28 overflow-y-auto rounded-2xl bg-white px-3 py-2 text-xs leading-5 text-stone-600 dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text)]">
                                {preset.prompt}
                              </div>
                              <div className="mt-3 flex items-center justify-between gap-3">
                                <a
                                  href={preset.sourceUrl}
                                  target="_blank"
                                  rel="noreferrer"
                                  className="min-w-0 truncate text-xs font-medium text-stone-400 transition hover:text-stone-700 dark:text-[var(--studio-text-muted)] dark:hover:text-[var(--studio-text-strong)]"
                                  onClick={(event) => event.stopPropagation()}
                                >
                                  查看来源
                                </a>
                                <Button
                                  type="button"
                                  size="sm"
                                  className="h-8 shrink-0 rounded-full bg-stone-950 px-3 text-xs font-semibold text-white hover:bg-stone-800 dark:bg-[var(--studio-accent-strong)] dark:text-[var(--studio-accent-foreground)] dark:hover:bg-[var(--studio-accent)]"
                                  onClick={() => {
                                    onApplyPromptPreset?.(preset);
                                    setIsPresetPanelOpen(false);
                                  }}
                                >
                                  套用模板
                                </Button>
                              </div>
                            </div>
                          </article>
                        ))}
                      </div>
                    ) : (
                      <div className="flex h-full min-h-[260px] items-center justify-center rounded-[24px] border border-dashed border-stone-200 text-sm text-stone-400 dark:border-[var(--studio-border)] dark:text-[var(--studio-text-muted)]">
                        没找到匹配模板
                      </div>
                    )}
                  </div>
                </div>
              </div>
            ), document.body) : null}
            <div className="flex items-end justify-between gap-3">
              <div className="flex min-w-0 flex-wrap items-center gap-1.5 sm:gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 rounded-full border-stone-200 bg-white px-2 text-[11px] font-medium text-stone-700 shadow-none sm:h-8 sm:px-2.5 sm:text-xs"
                  onClick={(event) => {
                    event.stopPropagation();
                    setIsPresetPanelOpen((current) => !current);
                  }}
                >
                  <Sparkles className="size-3.5" />
                  模板
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 rounded-full border-stone-200 bg-white px-2 text-[11px] font-medium text-stone-700 shadow-none sm:h-8 sm:px-2.5 sm:text-xs"
                  onClick={(event) => {
                    event.stopPropagation();
                    uploadInputRef.current?.click();
                  }}
                >
                  <ImagePlus className="size-3.5" />
                  {mode === "generate" ? "上传参考图" : "上传源图"}
                </Button>
                <span className="hidden truncate text-xs text-stone-400 dark:text-[var(--studio-text-muted)] lg:inline">
                  Enter 发送 · Shift+Enter 换行 · 可直接粘贴图片
                </span>
              </div>

              <button
                type="button"
                onClick={() => void onSubmit()}
                className="inline-flex size-8 shrink-0 items-center justify-center rounded-full bg-stone-950 text-white transition hover:bg-stone-800 disabled:cursor-not-allowed disabled:bg-stone-300 dark:bg-[var(--studio-accent-strong)] dark:text-[var(--studio-accent-foreground)] dark:hover:bg-[var(--studio-accent)] dark:disabled:bg-[var(--studio-panel-muted)] dark:disabled:text-[var(--studio-text-muted)] sm:size-9"
                aria-label="提交图片任务"
              >
                <ArrowUp className="size-4" />
              </button>
            </div>
          </div>

          <input
            ref={uploadInputRef}
            type="file"
            accept="image/*"
            multiple
            className="hidden"
            onChange={(event) => {
              void onAppendFiles(event.target.files, "image");
              event.currentTarget.value = "";
            }}
          />
        </div>
      </div>
    </div>
  );
}
