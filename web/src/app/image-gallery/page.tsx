"use client";

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Copy, Download, ImageIcon, ImagePlus, Info, LoaderCircle, Pencil, RefreshCw, Search, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { OriginalImagePreview } from "@/components/original-image-preview";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { deleteImageGalleryItems, listImageGallery, type ImageGalleryItem } from "@/lib/api";

const GALLERY_PAGE_SIZE = 24;

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
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

export default function ImageGalleryPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<ImageGalleryItem[]>([]);
  const [selectedNames, setSelectedNames] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [deletingName, setDeletingName] = useState("");
  const [isBatchDeleting, setIsBatchDeleting] = useState(false);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [searchDraft, setSearchDraft] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [detailItem, setDetailItem] = useState<ImageGalleryItem | null>(null);

  const pageCount = Math.max(1, Math.ceil(total / GALLERY_PAGE_SIZE));

  const loadItems = useCallback(async (nextPage: number, nextQuery: string) => {
    setIsLoading(true);
    try {
      const payload = await listImageGallery({ page: nextPage, pageSize: GALLERY_PAGE_SIZE, q: nextQuery });
      setItems(payload.items || []);
      setTotal(payload.total || 0);
      setPage(payload.page || nextPage);
      setSelectedNames([]);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取历史图库失败");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadItems(page, searchQuery);
  }, [loadItems, page, searchQuery]);

  const totalSize = useMemo(() => items.reduce((sum, item) => sum + (Number.isFinite(item.size) ? item.size : 0), 0), [items]);
  const selectedSet = useMemo(() => new Set(selectedNames), [selectedNames]);
  const selectedItems = useMemo(() => items.filter((item) => selectedSet.has(item.name)), [items, selectedSet]);

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
      setItems((current) => current.filter((entry) => entry.name !== item.name));
      setSelectedNames((current) => current.filter((name) => name !== item.name));
      setTotal((current) => Math.max(0, current - 1));
      toast.success("图片已删除");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除图片失败");
    } finally {
      setDeletingName("");
    }
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

  const handleCopyPrompt = async (prompt: string) => {
    try {
      await navigator.clipboard.writeText(prompt);
      toast.success("提示词已复制");
    } catch {
      toast.error("复制失败，请手动复制");
    }
  };

  const handleBatchDelete = async () => {
    if (selectedNames.length === 0) return;
    setIsBatchDeleting(true);
    try {
      const payload = await deleteImageGalleryItems(selectedNames);
      const deleted = new Set(payload.deleted || selectedNames);
      setItems((current) => current.filter((entry) => !deleted.has(entry.name)));
      setSelectedNames([]);
      setTotal((current) => Math.max(0, current - deleted.size));
      toast.success(`已删除 ${deleted.size} 张图片`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "批量删除失败");
    } finally {
      setIsBatchDeleting(false);
    }
  };

  const groupedItems = useMemo(() => {
    const groups = new Map<string, ImageGalleryItem[]>();
    for (const item of items) {
      const folder = item.folder || "";
      groups.set(folder, [...(groups.get(folder) || []), item]);
    }
    return Array.from(groups.entries());
  }, [items]);

  return (
    <section className="h-full">
      <div className="hide-scrollbar h-full min-h-0 overflow-y-auto rounded-[30px] border border-stone-200 bg-[#fcfcfb] px-4 pb-5 pt-0 shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 sm:pb-6 lg:px-6 lg:pb-7">
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
              <Button type="button" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none" onClick={() => void loadItems(page, searchQuery)} disabled={isLoading}>
                {isLoading ? <LoaderCircle className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                刷新
              </Button>
            </div>
          </section>
        </div>

        {isLoading ? (
          <div className="grid min-h-[280px] place-items-center text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">
            <div className="flex items-center gap-2"><LoaderCircle className="size-4 animate-spin" />正在读取历史图库...</div>
          </div>
        ) : items.length === 0 ? (
          <div className="grid min-h-[280px] place-items-center rounded-[28px] border border-dashed border-stone-200 bg-white/70 text-sm text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">{searchQuery ? "没有匹配的图片。" : "当前还没有可展示的图片。"}</div>
        ) : (
          <div className="mt-5 space-y-8">
            {groupedItems.map(([folder, groupItems]) => (
              <section key={folder || "all"} className="space-y-3">
                <div className="flex items-center justify-between">
                  {folder ? <h2 className="text-sm font-semibold text-stone-700 dark:text-[var(--studio-text)]">{folder}</h2> : <span />}
                  <button type="button" className="text-xs text-stone-500 underline underline-offset-4" onClick={() => setSelectedNames((current) => {
                    const names = groupItems.map((item) => item.name);
                    const allSelected = names.every((name) => current.includes(name));
                    return allSelected ? current.filter((name) => !names.includes(name)) : Array.from(new Set([...current, ...names]));
                  })}>全选/取消本组</button>
                </div>
                <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                  {groupItems.map((item) => (
                    <article key={item.id} className="flex overflow-hidden rounded-[28px] border border-stone-200 bg-white shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] flex-col">
                      <div className="relative">
                        <OriginalImagePreview
                          originalUrl={item.url}
                          title={basename(item.name)}
                          className="group relative block aspect-square w-full overflow-hidden bg-stone-100 text-left dark:bg-[var(--studio-panel)]"
                        >
                          <img src={item.thumbUrl || item.url} alt={basename(item.name)} className="h-full w-full object-cover transition duration-200 hover:scale-[1.02]" loading="lazy" />
                          <span className="pointer-events-none absolute bottom-3 right-3 rounded-full bg-white/85 px-2 py-0.5 text-[11px] font-medium text-stone-600 shadow-sm backdrop-blur">
                            缩略图
                          </span>
                        </OriginalImagePreview>
                        <label className="absolute left-3 top-3 rounded-full bg-white/90 px-3 py-2 text-xs text-stone-700 shadow-sm">
                          <input type="checkbox" className="mr-2 align-middle" checked={selectedSet.has(item.name)} onChange={() => toggleSelected(item.name)} />选择
                        </label>
                      </div>
                      <div className="flex flex-1 flex-col space-y-3 p-4">
                        <div className="text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">{formatTime(item.createdAt)} · {formatSize(item.size)}</div>
                        {item.prompt ? (
                          <div className="rounded-2xl border border-stone-100 bg-stone-50 px-3 py-2.5 text-xs leading-5 text-stone-600 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]" title={item.prompt}>
                            <div className="line-clamp-3 whitespace-pre-wrap">{item.prompt}</div>
                            <button type="button" className="mt-2 inline-flex items-center gap-1 text-[11px] font-medium text-stone-700 underline underline-offset-4 dark:text-[var(--studio-text)]" onClick={() => void handleCopyPrompt(item.prompt || "")}> <Copy className="size-3" />复制提示词</button>
                          </div>
                        ) : (
                          <div className="rounded-2xl border border-dashed border-stone-200 px-3 py-2.5 text-xs text-stone-400 dark:border-[var(--studio-border)]">未绑定提示词</div>
                        )}
                        <div className="mt-auto grid grid-cols-2 gap-2 pt-2">
                          <Button type="button" variant="outline" className="h-10 rounded-2xl border-stone-200 bg-white text-stone-700 shadow-none" onClick={() => handleEditImage(item)}>
                            <Pencil className="size-4" />修改
                          </Button>
                          <Button type="button" variant="outline" className="h-10 rounded-2xl border-stone-200 bg-white text-stone-700 shadow-none" onClick={() => handleReferenceImages([item])}>
                            <ImagePlus className="size-4" />参考
                          </Button>
                          <Button type="button" variant="outline" className="h-10 rounded-2xl border-stone-200 bg-white text-stone-700 shadow-none" onClick={() => setDetailItem(item)}>
                            <Info className="size-4" />详情
                          </Button>
                          <Button type="button" variant="outline" className="h-10 rounded-2xl border-stone-200 bg-white text-stone-700 shadow-none" asChild>
                            <a href={item.url} download={basename(item.name)}><Download className="size-4" />下载</a>
                          </Button>
                          <Button type="button" variant="outline" className="col-span-2 h-10 rounded-2xl border-red-200 bg-white px-3 text-red-600 shadow-none hover:bg-red-50 hover:text-red-700" onClick={() => void handleDelete(item)} disabled={deletingName === item.name}>
                            {deletingName === item.name ? <LoaderCircle className="size-4 animate-spin" /> : <Trash2 className="size-4" />}删除
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

        <div className="mt-5 flex items-center justify-between text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
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
                <div><dt className="text-xs text-stone-500">分组</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{detailItem.folder || "—"}</dd></div>
                <div><dt className="text-xs text-stone-500">创建时间</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{new Date(detailItem.createdAt).toLocaleString("zh-CN")}</dd></div>
                <div><dt className="text-xs text-stone-500">大小</dt><dd className="mt-1 text-stone-800 dark:text-[var(--studio-text)]">{formatSize(detailItem.size)} ({detailItem.size} B)</dd></div>
                <div><dt className="text-xs text-stone-500">URL</dt><dd className="mt-1 break-all font-mono text-xs text-stone-800 dark:text-[var(--studio-text)]">{detailItem.url}</dd></div>
              </dl>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
