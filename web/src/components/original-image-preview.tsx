"use client";

import { useEffect, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { LoaderCircle, X } from "lucide-react";

const ORIGINAL_IMAGE_CACHE = "chatgpt-image-studio-original-images-v1";

type CachedImageResult = {
  url: string;
  revoke: boolean;
};

type OriginalImagePreviewProps = {
  originalUrl: string;
  title?: string;
  children: ReactNode;
  className?: string;
};

async function loadOriginalImage(originalUrl: string): Promise<CachedImageResult> {
  if (typeof window === "undefined" || !("caches" in window)) {
    return { url: originalUrl, revoke: false };
  }

  const cache = await caches.open(ORIGINAL_IMAGE_CACHE);
  const cached = await cache.match(originalUrl);
  if (cached) {
    const blob = await cached.blob();
    return { url: URL.createObjectURL(blob), revoke: true };
  }

  const response = await fetch(originalUrl, { cache: "force-cache" });
  if (!response.ok) {
    return { url: originalUrl, revoke: false };
  }
  await cache.put(originalUrl, response.clone());
  const blob = await response.blob();
  return { url: URL.createObjectURL(blob), revoke: true };
}

export function OriginalImagePreview({ originalUrl, title = "查看原图", children, className }: OriginalImagePreviewProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [displayUrl, setDisplayUrl] = useState("");
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    let cancelled = false;
    let objectUrl = "";
    setIsLoading(true);
    setDisplayUrl("");

    loadOriginalImage(originalUrl)
      .then((result) => {
        if (cancelled) {
          if (result.revoke) {
            URL.revokeObjectURL(result.url);
          }
          return;
        }
        objectUrl = result.revoke ? result.url : "";
        setDisplayUrl(result.url);
      })
      .catch(() => {
        if (!cancelled) {
          setDisplayUrl(originalUrl);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl);
      }
    };
  }, [isOpen, originalUrl]);

  return (
    <>
      <button
        type="button"
        title={title}
        className={className}
        onClick={() => setIsOpen(true)}
      >
        {children}
      </button>
      {isOpen && typeof document !== "undefined"
        ? createPortal(
            <div
              className="fixed inset-0 z-[80] flex items-center justify-center bg-stone-950/78 p-4 backdrop-blur-sm"
              onClick={() => setIsOpen(false)}
            >
              <div className="absolute right-4 top-4 z-10">
                <button
                  type="button"
                  className="inline-flex size-10 items-center justify-center rounded-full bg-white/90 text-stone-700 shadow-sm transition hover:bg-white hover:text-stone-950"
                  onClick={() => setIsOpen(false)}
                  aria-label="关闭原图预览"
                >
                  <X className="size-5" />
                </button>
              </div>
              <div
                className="flex max-h-full max-w-full items-center justify-center"
                onClick={(event) => event.stopPropagation()}
              >
                {isLoading && !displayUrl ? (
                  <div className="inline-flex items-center gap-2 rounded-full bg-white/90 px-4 py-2 text-sm font-medium text-stone-700 shadow-sm">
                    <LoaderCircle className="size-4 animate-spin" />
                    正在加载原图
                  </div>
                ) : null}
                {displayUrl ? (
                  <img
                    src={displayUrl}
                    alt={title}
                    className="max-h-[92vh] max-w-[96vw] rounded-[18px] bg-white object-contain shadow-2xl"
                  />
                ) : null}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
