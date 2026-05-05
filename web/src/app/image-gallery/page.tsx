"use client";

import { useEffect, useMemo, useState } from "react";
import { Download, ImageIcon, LoaderCircle, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { deleteImageGalleryItem, listImageGallery, type ImageGalleryItem } from "@/lib/api";

function formatTime(value: string) {
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

function formatSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) {
    return "0 B";
  }
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
  const [isLoading, setIsLoading] = useState(true);
  const [deletingName, setDeletingName] = useState("");

  const loadItems = async () => {
    setIsLoading(true);
    try {
      const payload = await listImageGallery();
      setItems(payload.items || []);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取历史图库失败");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadItems();
  }, []);

  const totalSize = useMemo(
    () => items.reduce((sum, item) => sum + (Number.isFinite(item.size) ? item.size : 0), 0),
    [items],
  );

  const handleDelete = async (item: ImageGalleryItem) => {
    setDeletingName(item.name);
    try {
      await deleteImageGalleryItem(item.name);
      setItems((current) => current.filter((entry) => entry.name !== item.name));
      toast.success("图片已删除");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除图片失败");
    } finally {
      setDeletingName("");
    }
  };

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
                <p className="text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">展示当前用户目录里的所有出图，包含通过 API 调用但没有会话记录的图片。</p>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <div className="rounded-full border border-stone-200 bg-white px-4 py-2 text-xs text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">
                共 {items.length} 张 · {formatSize(totalSize)}
              </div>
              <Button
                type="button"
                variant="outline"
                className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none"
                onClick={() => void loadItems()}
                disabled={isLoading}
              >
                {isLoading ? <LoaderCircle className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
                刷新
              </Button>
            </div>
          </section>
        </div>

        {isLoading ? (
          <div className="grid min-h-[280px] place-items-center text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">
            <div className="flex items-center gap-2">
              <LoaderCircle className="size-4 animate-spin" />
              正在读取历史图库...
            </div>
          </div>
        ) : items.length === 0 ? (
          <div className="grid min-h-[280px] place-items-center rounded-[28px] border border-dashed border-stone-200 bg-white/70 text-sm text-stone-500 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">
            当前还没有可展示的图片。
          </div>
        ) : (
          <div className="mt-5 grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
            {items.map((item) => (
              <article key={item.id} className="overflow-hidden rounded-[28px] border border-stone-200 bg-white shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
                <a href={item.url} target="_blank" rel="noreferrer" className="block aspect-square overflow-hidden bg-stone-100 dark:bg-[var(--studio-panel)]">
                  <img src={item.url} alt={item.name} className="h-full w-full object-cover transition duration-200 hover:scale-[1.02]" loading="lazy" />
                </a>
                <div className="space-y-3 p-4">
                  <div>
                    <div className="truncate text-sm font-medium text-stone-950 dark:text-[var(--studio-text-strong)]">{item.name}</div>
                    <div className="mt-1 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">{formatTime(item.createdAt)} · {formatSize(item.size)}</div>
                  </div>
                  <div className="flex gap-2">
                    <Button type="button" variant="outline" className="h-10 flex-1 rounded-2xl border-stone-200 bg-white text-stone-700 shadow-none" asChild>
                      <a href={item.url} download={item.name}>
                        <Download className="size-4" />
                        下载
                      </a>
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      className="h-10 rounded-2xl border-red-200 bg-white px-3 text-red-600 shadow-none hover:bg-red-50 hover:text-red-700"
                      onClick={() => void handleDelete(item)}
                      disabled={deletingName === item.name}
                    >
                      {deletingName === item.name ? <LoaderCircle className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                    </Button>
                  </div>
                </div>
              </article>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
