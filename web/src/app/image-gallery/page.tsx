"use client";

import { useEffect, useMemo, useState } from "react";
import { Download, ImageIcon, LoaderCircle, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { deleteImageGalleryItems, listImageGallery, type ImageGalleryItem } from "@/lib/api";

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

export default function ImageGalleryPage() {
  const [items, setItems] = useState<ImageGalleryItem[]>([]);
  const [selectedNames, setSelectedNames] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [deletingName, setDeletingName] = useState("");
  const [isBatchDeleting, setIsBatchDeleting] = useState(false);

  const loadItems = async () => {
    setIsLoading(true);
    try {
      const payload = await listImageGallery();
      setItems(payload.items || []);
      setSelectedNames([]);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取历史图库失败");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadItems();
  }, []);

  const totalSize = useMemo(() => items.reduce((sum, item) => sum + (Number.isFinite(item.size) ? item.size : 0), 0), [items]);
  const selectedSet = useMemo(() => new Set(selectedNames), [selectedNames]);

  const toggleSelected = (name: string) => {
    setSelectedNames((current) => (current.includes(name) ? current.filter((item) => item !== name) : [...current, name]));
  };

  const handleDelete = async (item: ImageGalleryItem) => {
    setDeletingName(item.name);
    try {
      await deleteImageGalleryItems([item.name]);
      setItems((current) => current.filter((entry) => entry.name !== item.name));
      setSelectedNames((current) => current.filter((name) => name !== item.name));
      toast.success("图片已删除");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除图片失败");
    } finally {
      setDeletingName("");
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
      const folder = item.folder || "根目录";
      groups.set(folder, [...(groups.get(folder) || []), item]);
    }
    return Array.from(groups.entries());
  }, [items]);

  return (
    <section className="h-full">
      <div className="hide-scrollbar h-full min-h-0 overflow-y-auto rounded-[30px] border border-stone-200 bg-[#fcfcfb] px-4 pb-5 pt-0 shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 sm:pb-6 lg:px-6 lg:pb-7">
        <div className="sticky top-0 z-20 -mx-4 bg-[#fcfcfb] px-4 pt-5 pb-4 transition-colors duration-200 dark:bg-[var(--studio-panel)] sm:-mx-5 sm:px-5 sm:pt-6 sm:pb-4 lg:-mx-6 lg:px-6 lg:pt-7 lg:pb-5">
          <section className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex items-center gap-4">
              <div className="inline-flex size-12 items-center justify-center rounded-[18px] bg-stone-950 text-white shadow-sm">
                <ImageIcon className="size-5" />
              </div>
              <div className="space-y-1">
                <h1 className="text-2xl font-semibold tracking-tight text-stone-950 dark:text-[var(--studio-text-strong)]">历史图库</h1>
                <p className="text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">普通用户看自己的目录；管理员按文件夹查看 data/image 下所有图片。</p>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <div className="rounded-full border border-stone-200 bg-white px-4 py-2 text-xs text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">
                共 {items.length} 张 · {formatSize(totalSize)}
              </div>
              <Button type="button" variant="outline" className="h-10 rounded-full border-red-200 bg-white px-4 text-[13px] text-red-600 shadow-none hover:bg-red-50" onClick={() => void handleBatchDelete()} disabled={selectedNames.length === 0 || isBatchDeleting}>
                {isBatchDeleting ? <LoaderCircle className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                删除选中 {selectedNames.length > 0 ? selectedNames.length : ""}
              </Button>
              <Button type="button" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none" onClick={() => void loadItems()} disabled={isLoading}>
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
          <div className="grid min-h-[280px] place-items-center rounded-[28px] border border-dashed border-stone-200 bg-white/70 text-sm text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">当前还没有可展示的图片。</div>
        ) : (
          <div className="mt-5 space-y-8">
            {groupedItems.map(([folder, groupItems]) => (
              <section key={folder} className="space-y-3">
                <div className="flex items-center justify-between">
                  <h2 className="text-sm font-semibold text-stone-700 dark:text-[var(--studio-text)]">{folder}</h2>
                  <button type="button" className="text-xs text-stone-500 underline underline-offset-4" onClick={() => setSelectedNames((current) => {
                    const names = groupItems.map((item) => item.name);
                    const allSelected = names.every((name) => current.includes(name));
                    return allSelected ? current.filter((name) => !names.includes(name)) : Array.from(new Set([...current, ...names]));
                  })}>全选/取消本组</button>
                </div>
                <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                  {groupItems.map((item) => (
                    <article key={item.id} className="overflow-hidden rounded-[28px] border border-stone-200 bg-white shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
                      <div className="relative">
                        <a
                          href={item.url}
                          target="_blank"
                          rel="noreferrer"
                          title="当前显示缩略图，点击查看原图"
                          className="group relative block aspect-square overflow-hidden bg-stone-100 dark:bg-[var(--studio-panel)]"
                        >
                          <img src={item.thumbUrl || item.url} alt={item.name} className="h-full w-full object-cover transition duration-200 hover:scale-[1.02]" loading="lazy" />
                          <span className="pointer-events-none absolute bottom-3 right-3 rounded-full bg-white/85 px-2 py-0.5 text-[11px] font-medium text-stone-600 shadow-sm backdrop-blur">
                            缩略图
                          </span>
                        </a>
                        <label className="absolute left-3 top-3 rounded-full bg-white/90 px-3 py-2 text-xs text-stone-700 shadow-sm">
                          <input type="checkbox" className="mr-2 align-middle" checked={selectedSet.has(item.name)} onChange={() => toggleSelected(item.name)} />选择
                        </label>
                      </div>
                      <div className="space-y-3 p-4">
                        <div>
                          <div className="truncate text-sm font-medium text-stone-950 dark:text-[var(--studio-text-strong)]" title={item.name}>{item.name.split("/").pop()}</div>
                          <div className="mt-1 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">{formatTime(item.createdAt)} · {formatSize(item.size)}</div>
                        </div>
                        <div className="flex gap-2">
                          <Button type="button" variant="outline" className="h-10 flex-1 rounded-2xl border-stone-200 bg-white text-stone-700 shadow-none" asChild>
                            <a href={item.url} download={item.name.split("/").pop()}><Download className="size-4" />下载</a>
                          </Button>
                          <Button type="button" variant="outline" className="h-10 rounded-2xl border-red-200 bg-white px-3 text-red-600 shadow-none hover:bg-red-50 hover:text-red-700" onClick={() => void handleDelete(item)} disabled={deletingName === item.name}>
                            {deletingName === item.name ? <LoaderCircle className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
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
    </section>
  );
}
