"use client";

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Copy, Download, ImageIcon, ImagePlus, Info, LoaderCircle, Pencil, RefreshCw, Search, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";

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
import { deleteImageGalleryItems, listImageGallery, type ImageGalleryItem } from "@/lib/api";
import { listImageConversations, type ImageConversation } from "@/store/image-conversations";

const GALLERY_ROWS = 3;

function getGalleryColumnCount() {
  if (typeof window === "undefined") return 6;
  if (window.matchMedia("(min-width: 1536px)").matches) return 6;
  if (window.matchMedia("(min-width: 1280px)").matches) return 4;
  if (window.matchMedia("(min-width: 640px)").matches) return 3;
  return 2;
}

function getGalleryPageSize(columnCount: number) {
  if (columnCount <= 2) return 8;
  return GALLERY_ROWS * columnCount;
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
}

function formatDateGroup(value: string, groupMode: "user" | "month" | "day") {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知日期";
  if (groupMode === "month") return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit" }).format(date);
  if (groupMode === "day") return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" }).format(date);
  return "";
}

function optionTextMatches(option: { value: string; label: string }, query: string) {
  const keyword = query.trim().toLowerCase();
  if (!keyword) return true;
  return `${option.value} ${option.label}`.toLowerCase().includes(keyword);
}

function formatSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = size;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  return `${value >= 10 || unitIndex === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unitIndex]}`;
}

function formatResolution(item: ImageGalleryItem) {
  if (!item.width || !item.height) return "未知分辨率";
  return `${item.width}×${item.height}`;
}

function gcd(a: number, b: number): number {
  return b === 0 ? a : gcd(b, a % b);
}

function formatAspectRatio(item: ImageGalleryItem) {
  if (!item.width || !item.height) return "未知比例";
  const divisor = gcd(item.width, item.height);
  return `${item.width / divisor}:${item.height / divisor}`;
}

function basename(name: string) {
  return name.split("/").pop() || name;
}

function buildGallerySourceImage(item: ImageGalleryItem, index = 0) {
  return {
    id: `gallery-${item.id || item.name}-${index}`,
    role: "image" as const,
    name: basename(item.name),
    url: item.url,
  };
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

export default function ImageGalleryPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<ImageGalleryItem[]>([]);
  const [selectedNames, setSelectedNames] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [deletingName, setDeletingName] = useState("");
  const [isBatchDeleting, setIsBatchDeleting] = useState(false);
  const [page, setPage] = useState(1);
  const [columnCount, setColumnCount] = useState(getGalleryColumnCount());
  const [pageSize, setPageSize] = useState(getGalleryPageSize(getGalleryColumnCount()));
  const [total, setTotal] = useState(0);
  const [searchDraft, setSearchDraft] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [folderFilter, setFolderFilter] = useState("");
  const [folderOptionQuery, setFolderOptionQuery] = useState("");
  const [groupMode, setGroupMode] = useState<"user" | "month" | "day">("user");
  const [folderOptions, setFolderOptions] = useState<{ value: string; label: string }[]>([]);
  const [detailItem, setDetailItem] = useState<ImageGalleryItem | null>(null);

  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  const loadItems = useCallback(async (nextPage: number, nextQuery: string, nextPageSize: number, nextFolder: string, nextGroupMode: "user" | "month" | "day") => {
    setIsLoading(true);
    try {
      const [payload, conversations] = await Promise.all([
        listImageGallery({ page: nextPage, pageSize: nextPageSize, q: nextQuery, folder: nextFolder || undefined, group: nextGroupMode }),
        listImageConversations().catch(() => []),
      ]);
      const nextItems = attachLocalPrompts(payload.items || [], conversations);
      setItems(nextItems);
      setTotal(payload.total || 0);
      setPage(payload.page || nextPage);
      setSelectedNames([]);
      if (payload.folders) {
        setFolderOptions(payload.folders);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取历史图库失败");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    const updateLayout = () => {
      const nextColumnCount = getGalleryColumnCount();
      const nextPageSize = getGalleryPageSize(nextColumnCount);
      setColumnCount((current) => (current === nextColumnCount ? current : nextColumnCount));
      setPageSize((current) => (current === nextPageSize ? current : nextPageSize));
    };
    updateLayout();
    window.addEventListener("resize", updateLayout);
    return () => window.removeEventListener("resize", updateLayout);
  }, []);

  useEffect(() => {
    void loadItems(page, searchQuery, pageSize, folderFilter, groupMode);
  }, [loadItems, page, pageSize, searchQuery, folderFilter, groupMode]);

  const totalSize = useMemo(() => items.reduce((sum, item) => sum + (Number.isFinite(item.size) ? item.size : 0), 0), [items]);
  const previewItems = useMemo(() => items.map((item) => ({ originalUrl: item.url, title: basename(item.name) })), [items]);
  const previewIndexByName = useMemo(() => new Map(items.map((item, index) => [item.name, index])), [items]);
  const selectedSet = useMemo(() => new Set(selectedNames), [selectedNames]);
  const selectedItems = useMemo(() => items.filter((item) => selectedSet.has(item.name)), [items, selectedSet]);
  const visibleFolderOptions = useMemo(() => folderOptions.filter((option) => optionTextMatches(option, folderOptionQuery)), [folderOptions, folderOptionQuery]);
  const groupedItems = useMemo(() => {
    if (groupMode === "user") return items;
    return [...items].sort((a, b) => String(b.createdAt || "").localeCompare(String(a.createdAt || "")));
  }, [groupMode, items]);
  const missingDimensionItems = useMemo(() => items.filter((item) => !item.width || !item.height), [items]);

  useEffect(() => {
    if (missingDimensionItems.length === 0 || typeof window === "undefined") return;
    let cancelled = false;
    for (const item of missingDimensionItems) {
      const image = new window.Image();
      image.onload = () => {
        if (cancelled || !image.naturalWidth || !image.naturalHeight) return;
        setItems((current) => current.map((entry) => (entry.name === item.name && (!entry.width || !entry.height) ? { ...entry, width: image.naturalWidth, height: image.naturalHeight } : entry)));
      };
      image.src = item.url;
    }
    return () => {
      cancelled = true;
    };
  }, [missingDimensionItems]);

  const toggleSelected = (name: string) => {
    setSelectedNames((current) => (current.includes(name) ? current.filter((item) => item !== name) : [...current, name]));
  };

  const handleSearchSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPage(1);
    setSearchQuery(searchDraft.trim());
  };

  const handleDelete = async (item: ImageGalleryItem) => {
    setDeletingName(item.name);
    try {
      await deleteImageGalleryItems([item.name]);
      toast.success("图片已删除");
      void loadItems(page, searchQuery, pageSize, folderFilter, groupMode);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除图片失败");
    } finally {
      setDeletingName("");
    }
  };

  const copyPrompt = async (prompt?: string) => {
    const text = prompt?.trim();
    if (!text) {
      toast.error("当前图片没有可复制的提示词");
      return;
    }
    await navigator.clipboard.writeText(text);
    toast.success("提示词已复制");
  };

  const handleEditImage = (item: ImageGalleryItem) => {
    navigate("/image/workspace", {
      state: {
        mode: "edit",
        prompt: item.prompt || "",
        sourceImages: [buildGallerySourceImage(item)],
      },
    });
  };

  const handleReferenceImages = (references: ImageGalleryItem[]) => {
    if (references.length === 0) return;
    navigate("/image/workspace", {
      state: {
        mode: "generate",
        sourceImages: references.map((item, index) => buildGallerySourceImage(item, index)),
      },
    });
  };

  const handleBatchDelete = async () => {
    if (selectedNames.length === 0) return;
    setIsBatchDeleting(true);
    try {
      const payload = await deleteImageGalleryItems(selectedNames);
      const deleted = payload.deleted || selectedNames;
      toast.success(`已删除 ${deleted.length} 张图片`);
      void loadItems(page, searchQuery, pageSize, folderFilter, groupMode);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "批量删除失败");
    } finally {
      setIsBatchDeleting(false);
    }
  };

  const galleryRows = useMemo(() => {
    const rows: { groupItems: ImageGalleryItem[]; label: string; rowItems: ImageGalleryItem[] }[] = [];
    const minRowCount = Math.max(1, Math.ceil(pageSize / Math.max(1, columnCount)));
    let cursor = 0;

    while (cursor < groupedItems.length) {
      const rowItems: ImageGalleryItem[] = [];
      const rowGroup = groupMode === "user" ? groupedItems[cursor]?.folder || "" : formatDateGroup(groupedItems[cursor]?.createdAt || "", groupMode);
      const previousGroup = cursor > 0
        ? groupMode === "user"
          ? groupedItems[cursor - 1]?.folder || ""
          : formatDateGroup(groupedItems[cursor - 1]?.createdAt || "", groupMode)
        : "";

      while (cursor < groupedItems.length && rowItems.length < columnCount) {
        const itemGroup = groupMode === "user" ? groupedItems[cursor].folder || "" : formatDateGroup(groupedItems[cursor].createdAt, groupMode);
        if (itemGroup !== rowGroup) break;
        rowItems.push(groupedItems[cursor]);
        cursor += 1;
      }

      rows.push({
        groupItems: rowGroup ? groupedItems.filter((item) => (groupMode === "user" ? item.folder || "" : formatDateGroup(item.createdAt, groupMode)) === rowGroup) : [],
        label: rowGroup && rowGroup !== previousGroup ? rowGroup : "",
        rowItems,
      });
    }

    while (rows.length < minRowCount) {
      rows.push({ groupItems: [], label: "", rowItems: [] });
    }

    return rows;
  }, [columnCount, groupMode, groupedItems, pageSize]);

  return (
    <section className="h-full">
      <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-[30px] border border-stone-200 bg-[#fcfcfb] px-4 pb-4 pt-0 shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 lg:px-6">
        <div className="sticky top-0 z-20 -mx-4 bg-[#fcfcfb] px-4 pt-5 pb-4 transition-colors duration-200 dark:bg-[var(--studio-panel)] sm:-mx-5 sm:px-5 sm:pt-6 sm:pb-4 lg:-mx-6 lg:px-6 lg:pt-7 lg:pb-5">
          <section className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div className="flex items-center gap-4">
              <div className="inline-flex size-12 items-center justify-center rounded-[18px] bg-stone-950 text-white shadow-sm">
                <ImageIcon className="size-5" />
              </div>
              <div>
                <h1 className="text-2xl font-semibold tracking-tight text-stone-950 dark:text-[var(--studio-text-strong)]">历史图库</h1>
                <p className="mt-1 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">共 {total} 张 · 当前页 {items.length} 张 · {formatSize(totalSize)}</p>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {folderOptions.length > 0 ? (
                <Select value={folderFilter || "all"} onValueChange={(value) => { setFolderFilter(value === "all" ? "" : value); setPage(1); }}>
                  <SelectTrigger className="h-10 w-[150px] rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none"><SelectValue placeholder="选择用户" /></SelectTrigger>
                  <SelectContent>
                    <div className="p-1" onKeyDown={(event) => event.stopPropagation()}>
                      <Input value={folderOptionQuery} onChange={(event) => setFolderOptionQuery(event.target.value)} placeholder="搜索用户" className="h-8 rounded-xl border-stone-200 px-3 text-xs shadow-none" />
                    </div>
                    <SelectItem value="all">全部用户</SelectItem>
                    {visibleFolderOptions.length > 0 ? visibleFolderOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>) : <div className="px-3 py-2 text-xs text-stone-400">无匹配用户</div>}
                  </SelectContent>
                </Select>
              ) : null}
              {folderOptions.length > 0 ? (
                <Select value={groupMode} onValueChange={(value) => { setGroupMode(value as "user" | "month" | "day"); setPage(1); }}>
                  <SelectTrigger className="h-10 w-[136px] rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none"><SelectValue placeholder="分组方式" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="user">按用户分组</SelectItem>
                    <SelectItem value="month">按月分组</SelectItem>
                    <SelectItem value="day">按日分组</SelectItem>
                  </SelectContent>
                </Select>
              ) : null}
              <form className="flex items-center gap-2" onSubmit={handleSearchSubmit}>
                <Input value={searchDraft} onChange={(event) => setSearchDraft(event.target.value)} placeholder="搜索提示词" className="h-10 w-44 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none" />
                <Button type="submit" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none">
                  <Search className="size-4" />搜索
                </Button>
              </form>
              <Button type="button" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none" onClick={() => handleReferenceImages(selectedItems)} disabled={selectedItems.length === 0}>
                <ImagePlus className="size-4" />参考选中 {selectedItems.length > 0 ? selectedItems.length : ""}
              </Button>
              <Button type="button" variant="outline" className="h-10 rounded-full border-red-200 bg-white px-4 text-[13px] text-red-600 shadow-none hover:bg-red-50" onClick={() => void handleBatchDelete()} disabled={selectedNames.length === 0 || isBatchDeleting}>
                {isBatchDeleting ? <LoaderCircle className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                删除选中 {selectedNames.length > 0 ? selectedNames.length : ""}
              </Button>
              <Button type="button" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none" onClick={() => void loadItems(page, searchQuery, pageSize, folderFilter, groupMode)} disabled={isLoading}>
                {isLoading ? <LoaderCircle className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                刷新
              </Button>
            </div>
          </section>
        </div>

        <div className="min-h-0 flex-1 overflow-hidden">
          {isLoading ? (
          <div className="grid min-h-[280px] place-items-center text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">
            <div className="flex items-center gap-2"><LoaderCircle className="size-4 animate-spin" />正在读取历史图库...</div>
          </div>
        ) : items.length === 0 ? (
          <div className="grid min-h-[280px] place-items-center rounded-[28px] border border-dashed border-stone-200 bg-white/70 text-sm text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">{searchQuery ? "没有匹配的图片。" : "当前还没有可展示的图片。"}</div>
        ) : (
          <div className="grid h-full min-h-0 gap-2 pt-2" style={{ gridTemplateRows: `repeat(${Math.max(galleryRows.length, 1)}, minmax(0, 1fr))` }}>
            {galleryRows.map((row, rowIndex) => (
              <section key={rowIndex} className="flex min-h-0 flex-col">
                <div className={`flex h-4 shrink-0 items-center justify-between border-b text-[11px] ${row.label ? "border-stone-200 dark:border-[var(--studio-border)]" : "border-transparent"}`}>
                  {row.label ? <span className="font-semibold text-stone-700 dark:text-[var(--studio-text)]">{row.label}</span> : <span />}
                  {row.label ? <button type="button" className="text-stone-500 underline underline-offset-4" onClick={() => setSelectedNames((current) => {
                    const names = row.groupItems.map((item) => item.name);
                    const allSelected = names.every((name) => current.includes(name));
                    return allSelected ? current.filter((name) => !names.includes(name)) : Array.from(new Set([...current, ...names]));
                  })}>全选/取消本组</button> : <span />}
                </div>
                <div className="grid min-h-0 flex-1 gap-2" style={{ gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))` }}>
                  {row.rowItems.map((item) => (
                    <article key={item.id} className="group flex min-h-0 flex-col overflow-hidden rounded-[18px] border border-stone-200 bg-white shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
                      <div className="relative min-h-0 flex-1">
                        <OriginalImagePreview
                          originalUrl={item.url}
                          title={basename(item.name)}
                          items={previewItems}
                          initialIndex={previewIndexByName.get(item.name) ?? 0}
                          className="group relative block h-full w-full overflow-hidden bg-stone-100 text-left dark:bg-[var(--studio-panel)]"
                        >
                          <img src={item.thumbUrl || item.url} alt={basename(item.name)} className="h-full w-full object-cover transition duration-200 hover:scale-[1.02]" loading="lazy" />
                        </OriginalImagePreview>
                        <label className={`absolute left-2 top-2 rounded-full border border-white/25 bg-white/18 px-2 py-1 text-[11px] text-white shadow-[0_8px_24px_rgba(15,23,42,0.18)] backdrop-blur-md transition dark:border-white/10 dark:bg-stone-950/20 ${selectedSet.has(item.name) ? "opacity-100" : "opacity-0 group-hover:opacity-100"}`}>
                          <input type="checkbox" className="mr-1.5 align-middle accent-white" checked={selectedSet.has(item.name)} onChange={() => toggleSelected(item.name)} />选择
                        </label>
                        <div className="pointer-events-none absolute inset-x-2 bottom-2 flex items-center gap-1.5 rounded-full border border-white/25 bg-white/18 px-2 py-1 text-[11px] text-white opacity-0 shadow-[0_8px_24px_rgba(15,23,42,0.18)] backdrop-blur-md transition group-hover:opacity-100 dark:border-white/10 dark:bg-stone-950/20">
                          <span className="min-w-0 flex-1 truncate">
                            {formatTime(item.createdAt)} · {formatSize(item.size)} · {formatResolution(item)}
                          </span>
                          <button type="button" className="pointer-events-auto inline-flex size-5 shrink-0 items-center justify-center rounded-full text-white/85 transition hover:bg-white/20 hover:text-white" onClick={() => setDetailItem(item)} aria-label="查看图片详情" title="详情">
                            <Info className="size-3.5" />
                          </button>
                        </div>
                      </div>
                      <div className="flex shrink-0 flex-col gap-1 p-2">
                        <div className="flex h-6 items-center gap-1 rounded-xl border border-stone-100 bg-stone-50 px-2 text-[11px] text-stone-600 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]" title={item.prompt || "未绑定提示词"}>
                          {item.prompt ? <div className="min-w-0 flex-1 truncate">{item.prompt}</div> : <div className="min-w-0 flex-1 text-stone-400">未绑定提示词</div>}
                          {item.prompt ? (
                            <button type="button" className="inline-flex size-4 shrink-0 items-center justify-center rounded text-stone-400 hover:bg-stone-200 hover:text-stone-700" onClick={() => void copyPrompt(item.prompt)} title="复制提示词" aria-label="复制提示词">
                              <Copy className="size-3" />
                            </button>
                          ) : null}
                        </div>
                        <div className="grid shrink-0 grid-cols-2 gap-1 pt-0.5">
                          <Button type="button" variant="outline" className="h-7 rounded-xl border-stone-200 bg-white px-2 text-[11px] text-stone-700 shadow-none" onClick={() => handleEditImage(item)}>
                            <Pencil className="size-3.5" />编辑
                          </Button>
                          <Button type="button" variant="outline" className="h-7 rounded-xl border-stone-200 bg-white px-2 text-[11px] text-stone-700 shadow-none" onClick={() => handleReferenceImages([item])}>
                            <ImagePlus className="size-3.5" />参考
                          </Button>
                          <Button type="button" variant="outline" className="h-7 rounded-xl border-stone-200 bg-white px-2 text-[11px] text-stone-700 shadow-none" asChild>
                            <a href={item.url} download={basename(item.name)}><Download className="size-3.5" />下载</a>
                          </Button>
                          <Button type="button" variant="outline" className="h-7 rounded-xl border-red-200 bg-white px-2 text-[11px] text-red-600 shadow-none hover:bg-red-50 hover:text-red-700" onClick={() => void handleDelete(item)} disabled={deletingName === item.name}>
                            {deletingName === item.name ? <LoaderCircle className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}删除
                          </Button>
                        </div>
                      </div>
                    </article>
                  ))}
                </div>
              </section>
            ))}
          </div>
          )}
        </div>

        <div className="mt-auto flex shrink-0 items-center justify-between pt-3 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
          <span>第 {page}/{pageCount} 页</span>
          <div className="flex gap-2">
            <Button type="button" variant="outline" className="h-9 rounded-full border-stone-200 bg-white px-4 text-xs shadow-none" onClick={() => setPage((current) => Math.max(1, current - 1))} disabled={page <= 1 || isLoading}>上一页</Button>
            <Button type="button" variant="outline" className="h-9 rounded-full border-stone-200 bg-white px-4 text-xs shadow-none" onClick={() => setPage((current) => Math.min(pageCount, current + 1))} disabled={page >= pageCount || isLoading}>下一页</Button>
          </div>
        </div>

        {detailItem ? (
          <div className="fixed inset-0 z-50 grid place-items-center bg-black/30 px-4" onClick={() => setDetailItem(null)}>
            <div className="w-full max-w-lg rounded-[24px] border border-stone-200 bg-white p-5 shadow-xl dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]" onClick={(event) => event.stopPropagation()}>
              <div className="mb-4 flex items-center justify-between">
                <h2 className="text-base font-semibold text-stone-950 dark:text-[var(--studio-text-strong)]">图片详情</h2>
                <Button type="button" variant="outline" className="h-8 rounded-full px-3 text-xs" onClick={() => setDetailItem(null)}>关闭</Button>
              </div>
              <dl className="space-y-3 text-sm">
                <div><dt className="text-xs text-stone-500">文件名</dt><dd className="mt-1 break-all font-mono text-xs text-stone-800 dark:text-[var(--studio-text)]">{detailItem.name}</dd></div>
                <div><dt className="text-xs text-stone-500">所属用户</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{detailItem.userLabel || detailItem.folder || detailItem.userId || "—"}</dd></div>
                <div><dt className="text-xs text-stone-500">分组</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{groupMode === "user" ? detailItem.folder || "—" : formatDateGroup(detailItem.createdAt, groupMode)}</dd></div>
                <div><dt className="text-xs text-stone-500">创建时间</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{new Date(detailItem.createdAt).toLocaleString("zh-CN")}</dd></div>
                <div><dt className="text-xs text-stone-500">大小</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{formatSize(detailItem.size)} ({detailItem.size} B)</dd></div>
                <div><dt className="text-xs text-stone-500">分辨率</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{formatResolution(detailItem)}</dd></div>
                <div><dt className="text-xs text-stone-500">长宽比</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{formatAspectRatio(detailItem)}</dd></div>
                <div>
                  <dt className="text-xs text-stone-500">提示词</dt>
                  <dd className="mt-1 flex items-start gap-2 text-stone-800 dark:text-[var(--studio-text)]">
                    <span className="min-w-0 flex-1 break-all">{detailItem.prompt || "—"}</span>
                    {detailItem.prompt ? (
                      <Button type="button" variant="outline" className="h-7 rounded-full px-2 text-xs" onClick={() => void copyPrompt(detailItem.prompt)}>
                        <Copy className="size-3.5" />复制
                      </Button>
                    ) : null}
                  </dd>
                </div>
                <div><dt className="text-xs text-stone-500">URL</dt><dd className="mt-1 break-all font-mono text-xs text-stone-800 dark:text-[var(--studio-text)]">{detailItem.url}</dd></div>
              </dl>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
