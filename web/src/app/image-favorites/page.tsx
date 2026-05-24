"use client";

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Copy, Heart, ImageIcon, ImagePlus, LoaderCircle, RefreshCw, Search, Sparkles, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { listFavorites, listImageGallery, setFavorite, type ImageGalleryItem } from "@/lib/api";
import {
  exportLocalImageConversationsSnapshot,
  exportServerImageConversationsSnapshot,
  type ImageConversation,
} from "@/store/image-conversations";

import { imagePromptPresets, type ImagePromptPreset } from "../image/prompt-presets";

const IMAGE_PAGE_SIZE = 96;

type FavoriteTab = "templates" | "images";

function basename(name: string) {
  return name.split("/").pop() || name;
}

function imagePromptKeys(value?: string) {
  const trimmed = String(value || "").trim().split("?")[0];
  if (!trimmed) return [];
  const marker = "/v1/files/image/";
  const markerIndex = trimmed.lastIndexOf(marker);
  const pathValue = markerIndex >= 0 ? trimmed.slice(markerIndex + marker.length) : trimmed.replace(/^\/?v1\/files\/image\//, "");
  return Array.from(
    new Set(
      [trimmed, pathValue, basename(trimmed), basename(pathValue)]
        .map((key) => key.trim().replace(/^\/+|\/+$/g, ""))
        .filter(Boolean),
    ),
  );
}

function buildConversationPromptMap(conversations: ImageConversation[]) {
  const metadata = new Map<string, { prompt: string; conversationId: string; turnId?: string }>();
  const addImage = (url: string | undefined, prompt: string, conversationId: string, turnId?: string) => {
    const normalizedPrompt = prompt.trim();
    if (!url || !normalizedPrompt) return;
    for (const key of imagePromptKeys(url)) {
      metadata.set(key, { prompt: normalizedPrompt, conversationId, turnId });
    }
  };
  for (const conversation of conversations) {
    const conversationPrompt = conversation.prompt.trim();
    for (const image of conversation.images || []) {
      addImage(image.url, image.prompt || conversationPrompt, conversation.id);
    }
    for (const turn of conversation.turns || []) {
      const turnPrompt = (turn.prompt || conversationPrompt).trim();
      for (const image of turn.images || []) {
        addImage(image.url, image.prompt || turnPrompt, conversation.id, turn.id);
      }
    }
  }
  return metadata;
}

function attachLocalPrompts(items: ImageGalleryItem[], conversations: ImageConversation[]) {
  const metadata = buildConversationPromptMap(conversations);
  if (metadata.size === 0) return items;
  return items.map((item) => {
    if (item.prompt) return item;
    for (const key of [...imagePromptKeys(item.name), ...imagePromptKeys(item.url)]) {
      const match = metadata.get(key);
      if (match) {
        return {
          ...item,
          prompt: match.prompt,
          conversationId: item.conversationId || match.conversationId,
          turnId: item.turnId || match.turnId,
        };
      }
    }
    return item;
  });
}

function buildGallerySourceImage(item: ImageGalleryItem, index = 0) {
  return {
    id: `favorite-${item.id || item.name}-${index}`,
    role: "image" as const,
    name: basename(item.name),
    url: item.url,
  };
}

function matchesTemplate(preset: ImagePromptPreset, query: string) {
  const keyword = query.trim().toLowerCase();
  if (!keyword) return true;
  return [preset.title, preset.description, preset.prompt, preset.category]
    .filter(Boolean)
    .join(" ")
    .toLowerCase()
    .includes(keyword);
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
}

export default function ImageFavoritesPage() {
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<FavoriteTab>("templates");
  const [queryDraft, setQueryDraft] = useState("");
  const [query, setQuery] = useState("");
  const [favoriteTemplateIds, setFavoriteTemplateIds] = useState<Set<string>>(() => new Set());
  const [imageItems, setImageItems] = useState<ImageGalleryItem[]>([]);
  const [imagePage, setImagePage] = useState(1);
  const [imageTotal, setImageTotal] = useState(0);
  const [isLoadingTemplates, setIsLoadingTemplates] = useState(true);
  const [isLoadingImages, setIsLoadingImages] = useState(true);
  const [updatingKey, setUpdatingKey] = useState("");

  const imagePageCount = Math.max(1, Math.ceil(imageTotal / IMAGE_PAGE_SIZE));

  const loadTemplateFavorites = useCallback(async () => {
    setIsLoadingTemplates(true);
    try {
      const payload = await listFavorites("template");
      setFavoriteTemplateIds(new Set(payload.items || []));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取模板收藏失败");
    } finally {
      setIsLoadingTemplates(false);
    }
  }, []);

  const loadImageFavorites = useCallback(async (nextPage: number, nextQuery: string) => {
    setIsLoadingImages(true);
    try {
      const payload = await listImageGallery({ page: nextPage, pageSize: IMAGE_PAGE_SIZE, q: nextQuery, favorite: true });
      let nextItems = payload.items || [];
      if (nextItems.some((item) => !item.prompt)) {
        const [localConversations, serverConversations] = await Promise.all([
          exportLocalImageConversationsSnapshot().catch(() => []),
          exportServerImageConversationsSnapshot().catch(() => []),
        ]);
        const conversationById = new Map<string, ImageConversation>();
        for (const conversation of [...localConversations, ...serverConversations]) {
          conversationById.set(conversation.id, conversation);
        }
        nextItems = attachLocalPrompts(nextItems, [...conversationById.values()]);
      }
      setImageItems(nextItems);
      setImageTotal(payload.total || 0);
      setImagePage(payload.page || nextPage);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取图片收藏失败");
    } finally {
      setIsLoadingImages(false);
    }
  }, []);

  useEffect(() => {
    void loadTemplateFavorites();
  }, [loadTemplateFavorites]);

  useEffect(() => {
    void loadImageFavorites(imagePage, query);
  }, [imagePage, query, loadImageFavorites]);

  const favoriteTemplates = useMemo(
    () => imagePromptPresets.filter((preset) => favoriteTemplateIds.has(preset.id) && matchesTemplate(preset, query)),
    [favoriteTemplateIds, query],
  );

  const handleSearchSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setQuery(queryDraft.trim());
    setImagePage(1);
  };

  const copyPrompt = async (prompt?: string) => {
    const text = prompt?.trim();
    if (!text) {
      toast.error("当前项目没有可复制的提示词");
      return;
    }
    await navigator.clipboard.writeText(text);
    toast.success("提示词已复制");
  };

  const useTemplate = (preset: ImagePromptPreset) => {
    navigate("/image/workspace", {
      state: { mode: "generate", prompt: preset.prompt },
    });
  };

  const useImage = (item: ImageGalleryItem) => {
    navigate("/image/workspace", {
      state: {
        mode: "generate",
        prompt: item.prompt?.trim() || "",
        sourceImages: [buildGallerySourceImage(item)],
      },
    });
  };

  const removeTemplateFavorite = async (preset: ImagePromptPreset) => {
    setUpdatingKey(`template:${preset.id}`);
    try {
      await setFavorite("template", preset.id, false);
      setFavoriteTemplateIds((current) => {
        const next = new Set(current);
        next.delete(preset.id);
        return next;
      });
      toast.success("已移出模板收藏");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "取消模板收藏失败");
    } finally {
      setUpdatingKey("");
    }
  };

  const removeImageFavorite = async (item: ImageGalleryItem) => {
    setUpdatingKey(`image:${item.name}`);
    try {
      await setFavorite("image", item.name, false);
      toast.success("已移出图片收藏");
      void loadImageFavorites(imagePage, query);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "取消图片收藏失败");
    } finally {
      setUpdatingKey("");
    }
  };

  return (
    <section className="h-full">
      <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-[30px] border border-stone-200 bg-[#fcfcfb] px-4 pb-4 pt-0 shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 lg:px-6">
        <div className="sticky top-0 z-20 -mx-4 bg-[#fcfcfb] px-4 pt-5 pb-4 transition-colors duration-200 dark:bg-[var(--studio-panel)] sm:-mx-5 sm:px-5 sm:pt-6 sm:pb-4 lg:-mx-6 lg:px-6 lg:pt-7 lg:pb-5">
          <section className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="flex items-center gap-4">
              <div className="inline-flex size-12 items-center justify-center rounded-[18px] bg-stone-950 text-white shadow-sm">
                <Heart className="size-5 fill-current" />
              </div>
              <div>
                <h1 className="text-2xl font-semibold tracking-tight text-stone-950 dark:text-[var(--studio-text-strong)]">收藏管理</h1>
                <p className="mt-1 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">统一管理模板收藏和图片收藏，可一键带提示词进入生图对话</p>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <div className="inline-flex rounded-full border border-stone-200 bg-white p-1 text-xs shadow-none dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
                <button type="button" className={`rounded-full px-4 py-2 transition ${activeTab === "templates" ? "bg-stone-950 text-white" : "text-stone-600 hover:bg-stone-100 dark:text-[var(--studio-text-muted)]"}`} onClick={() => setActiveTab("templates")}>模板收藏</button>
                <button type="button" className={`rounded-full px-4 py-2 transition ${activeTab === "images" ? "bg-stone-950 text-white" : "text-stone-600 hover:bg-stone-100 dark:text-[var(--studio-text-muted)]"}`} onClick={() => setActiveTab("images")}>图片收藏</button>
              </div>
              <form className="flex items-center gap-2" onSubmit={handleSearchSubmit}>
                <Input value={queryDraft} onChange={(event) => setQueryDraft(event.target.value)} placeholder="搜索收藏提示词" className="h-10 w-48 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none" />
                <Button type="submit" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none">
                  <Search className="size-4" />搜索
                </Button>
              </form>
              <Button type="button" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none" onClick={() => { void loadTemplateFavorites(); void loadImageFavorites(imagePage, query); }} disabled={isLoadingTemplates || isLoadingImages}>
                {isLoadingTemplates || isLoadingImages ? <LoaderCircle className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                刷新
              </Button>
            </div>
          </section>
        </div>

        <div className="min-h-0 flex-1 overflow-auto pr-1">
          {activeTab === "templates" ? (
            isLoadingTemplates ? (
              <div className="grid min-h-[280px] place-items-center text-sm text-stone-500"><LoaderCircle className="mr-2 inline size-4 animate-spin" />正在读取模板收藏...</div>
            ) : favoriteTemplates.length === 0 ? (
              <div className="grid min-h-[280px] place-items-center rounded-[28px] border border-dashed border-stone-200 bg-white/70 text-sm text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">没有匹配的模板收藏。</div>
            ) : (
              <div className="grid gap-3 pb-3 sm:grid-cols-2 xl:grid-cols-3">
                {favoriteTemplates.map((preset) => (
                  <article key={preset.id} className="flex min-h-[280px] flex-col overflow-hidden rounded-[24px] border border-stone-200 bg-white shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
                    {preset.imageUrl ? <img src={preset.imageUrl} alt={preset.title} className="h-36 w-full object-cover" loading="lazy" /> : null}
                    <div className="flex flex-1 flex-col gap-3 p-4">
                      <div>
                        <div className="inline-flex items-center gap-1 rounded-full bg-rose-50 px-2 py-1 text-[11px] font-medium text-rose-600"><Sparkles className="size-3" />模板</div>
                        <h2 className="mt-2 line-clamp-2 text-base font-semibold text-stone-950 dark:text-[var(--studio-text-strong)]">{preset.title}</h2>
                        <p className="mt-1 line-clamp-2 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">{preset.description}</p>
                      </div>
                      <div className="max-h-28 flex-1 overflow-auto rounded-2xl border border-stone-100 bg-stone-50 p-3 text-xs leading-5 text-stone-600 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]">{preset.prompt}</div>
                      <div className="grid grid-cols-3 gap-2">
                        <Button type="button" className="h-9 rounded-full bg-stone-950 px-3 text-xs text-white hover:bg-stone-800" onClick={() => useTemplate(preset)}><ImagePlus className="size-3.5" />生图引用</Button>
                        <Button type="button" variant="outline" className="h-9 rounded-full border-stone-200 bg-white px-3 text-xs shadow-none" onClick={() => void copyPrompt(preset.prompt)}><Copy className="size-3.5" />复制</Button>
                        <Button type="button" variant="outline" className="h-9 rounded-full border-red-200 bg-white px-3 text-xs text-red-600 shadow-none hover:bg-red-50" onClick={() => void removeTemplateFavorite(preset)} disabled={updatingKey === `template:${preset.id}`}>
                          {updatingKey === `template:${preset.id}` ? <LoaderCircle className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}移除
                        </Button>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            )
          ) : (
            isLoadingImages ? (
              <div className="grid min-h-[280px] place-items-center text-sm text-stone-500"><LoaderCircle className="mr-2 inline size-4 animate-spin" />正在读取图片收藏...</div>
            ) : imageItems.length === 0 ? (
              <div className="grid min-h-[280px] place-items-center rounded-[28px] border border-dashed border-stone-200 bg-white/70 text-sm text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">没有匹配的图片收藏。</div>
            ) : (
              <div className="grid gap-3 pb-3 sm:grid-cols-2 xl:grid-cols-4">
                {imageItems.map((item) => (
                  <article key={item.id || item.name} className="flex min-h-[300px] flex-col overflow-hidden rounded-[24px] border border-stone-200 bg-white shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
                    <div className="relative h-44 bg-stone-100 dark:bg-[var(--studio-panel)]">
                      <img src={item.thumbUrl || item.url} alt={basename(item.name)} className="h-full w-full object-cover" loading="lazy" />
                      <span className="absolute left-2 top-2 rounded-full border border-white/25 bg-white/20 px-2 py-1 text-[11px] text-white shadow-sm backdrop-blur-md">{formatTime(item.createdAt)}</span>
                    </div>
                    <div className="flex flex-1 flex-col gap-3 p-4">
                      <div>
                        <div className="inline-flex items-center gap-1 rounded-full bg-rose-50 px-2 py-1 text-[11px] font-medium text-rose-600"><ImageIcon className="size-3" />图片</div>
                        <h2 className="mt-2 truncate text-sm font-semibold text-stone-950 dark:text-[var(--studio-text-strong)]" title={item.name}>{basename(item.name)}</h2>
                      </div>
                      <div className="max-h-24 flex-1 overflow-auto rounded-2xl border border-stone-100 bg-stone-50 p-3 text-xs leading-5 text-stone-600 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]">{item.prompt || "未绑定提示词，仍可把图片作为参考图带入生图对话。"}</div>
                      <div className="grid grid-cols-3 gap-2">
                        <Button type="button" className="h-9 rounded-full bg-stone-950 px-3 text-xs text-white hover:bg-stone-800" onClick={() => useImage(item)}><ImagePlus className="size-3.5" />生图引用</Button>
                        <Button type="button" variant="outline" className="h-9 rounded-full border-stone-200 bg-white px-3 text-xs shadow-none" onClick={() => void copyPrompt(item.prompt)} disabled={!item.prompt}><Copy className="size-3.5" />复制</Button>
                        <Button type="button" variant="outline" className="h-9 rounded-full border-red-200 bg-white px-3 text-xs text-red-600 shadow-none hover:bg-red-50" onClick={() => void removeImageFavorite(item)} disabled={updatingKey === `image:${item.name}`}>
                          {updatingKey === `image:${item.name}` ? <LoaderCircle className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}移除
                        </Button>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            )
          )}
        </div>

        {activeTab === "images" ? (
          <div className="mt-auto flex shrink-0 items-center justify-between pt-3 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
            <span>图片收藏第 {imagePage}/{imagePageCount} 页 · 共 {imageTotal} 张</span>
            <div className="flex gap-2">
              <Button type="button" variant="outline" className="h-9 rounded-full border-stone-200 bg-white px-4 text-xs shadow-none" onClick={() => setImagePage((current) => Math.max(1, current - 1))} disabled={imagePage <= 1 || isLoadingImages}>上一页</Button>
              <Button type="button" variant="outline" className="h-9 rounded-full border-stone-200 bg-white px-4 text-xs shadow-none" onClick={() => setImagePage((current) => Math.min(imagePageCount, current + 1))} disabled={imagePage >= imagePageCount || isLoadingImages}>下一页</Button>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
