"use client";

import { useEffect, useRef, useState, type ReactNode, type TouchEvent as ReactTouchEvent } from "react";
import { createPortal } from "react-dom";
import { ChevronLeft, ChevronRight, LoaderCircle } from "lucide-react";

const ORIGINAL_IMAGE_CACHE = "chatgpt-image-studio-original-images-v1";

type CachedImageResult = {
  url: string;
  revoke: boolean;
};

type OriginalImagePreviewItem = {
  originalUrl: string;
  title?: string;
};

type OriginalImagePreviewProps = {
  originalUrl: string;
  title?: string;
  children: ReactNode;
  className?: string;
  items?: OriginalImagePreviewItem[];
  initialIndex?: number;
};

function clampScale(scale: number) {
  return Math.min(4, Math.max(0.5, scale));
}

function touchDistance(first: Touch, second: Touch) {
  const deltaX = second.clientX - first.clientX;
  const deltaY = second.clientY - first.clientY;
  return Math.hypot(deltaX, deltaY);
}

function touchCenter(first: Touch, second: Touch) {
  return {
    x: (first.clientX + second.clientX) / 2,
    y: (first.clientY + second.clientY) / 2,
  };
}

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

export function OriginalImagePreview({ originalUrl, title = "查看原图", children, className, items = [], initialIndex = 0 }: OriginalImagePreviewProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(initialIndex);
  const [displayUrl, setDisplayUrl] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [scale, setScale] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const imageRef = useRef<HTMLImageElement | null>(null);
  const dragStartRef = useRef<{ pointerX: number; pointerY: number; offsetX: number; offsetY: number } | null>(null);
  const touchPanStartRef = useRef<{ pointerX: number; pointerY: number; offsetX: number; offsetY: number } | null>(null);
  const pinchStartRef = useRef<{ distance: number; scale: number; offsetX: number; offsetY: number; centerX: number; centerY: number } | null>(null);
  const dragMovedRef = useRef(false);
  const suppressNextClickRef = useRef(false);
  const scaleRef = useRef(scale);
  const offsetRef = useRef(offset);

  const resetGestureState = () => {
    dragStartRef.current = null;
    touchPanStartRef.current = null;
    pinchStartRef.current = null;
    dragMovedRef.current = false;
  };

  const isPointInsideImage = (clientX: number, clientY: number) => {
    const imageRect = imageRef.current?.getBoundingClientRect();
    if (!imageRect) return false;
    return clientX >= imageRect.left && clientX <= imageRect.right && clientY >= imageRect.top && clientY <= imageRect.bottom;
  };

  useEffect(() => {
    scaleRef.current = scale;
  }, [scale]);

  useEffect(() => {
    offsetRef.current = offset;
  }, [offset]);
  const previewItems = items.length > 0 ? items : [{ originalUrl, title }];
  const boundedActiveIndex = Math.min(Math.max(activeIndex, 0), previewItems.length - 1);
  const activeItem = previewItems[boundedActiveIndex] ?? { originalUrl, title };
  const activeTitle = activeItem.title || title;
  const hasPrevious = boundedActiveIndex > 0;
  const hasNext = boundedActiveIndex < previewItems.length - 1;

  useEffect(() => {
    if (!isOpen) return;
    const handleWheel = (event: WheelEvent) => {
      if (!event.ctrlKey) return;
      event.preventDefault();
      setScale((current) => {
        const nextScale = clampScale(current + (event.deltaY < 0 ? 0.12 : -0.12));
        if (nextScale <= 1) {
          setOffset({ x: 0, y: 0 });
        }
        return nextScale;
      });
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setIsOpen(false);
      }
    };
    window.addEventListener("wheel", handleWheel, { passive: false });
    window.addEventListener("keydown", handleKeyDown, true);
    document.addEventListener("keydown", handleKeyDown, true);
    return () => {
      window.removeEventListener("wheel", handleWheel);
      window.removeEventListener("keydown", handleKeyDown, true);
      document.removeEventListener("keydown", handleKeyDown, true);
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) return;
    const handleMouseMove = (event: MouseEvent) => {
      const dragStart = dragStartRef.current;
      if (!dragStart) return;
      const nextOffset = {
        x: dragStart.offsetX + event.clientX - dragStart.pointerX,
        y: dragStart.offsetY + event.clientY - dragStart.pointerY,
      };
      if (Math.abs(event.clientX - dragStart.pointerX) > 3 || Math.abs(event.clientY - dragStart.pointerY) > 3) {
        dragMovedRef.current = true;
      }
      setOffset(nextOffset);
    };
    const handleMouseUp = () => {
      if (dragMovedRef.current) {
        suppressNextClickRef.current = true;
      }
      dragStartRef.current = null;
      dragMovedRef.current = false;
    };
    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    let cancelled = false;
    let objectUrl = "";
    setIsLoading(true);
    setDisplayUrl("");
    setScale(1);
    setOffset({ x: 0, y: 0 });
    resetGestureState();
    suppressNextClickRef.current = false;

    loadOriginalImage(activeItem.originalUrl)
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
          setDisplayUrl(activeItem.originalUrl);
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
  }, [activeItem.originalUrl, isOpen]);

  const handleTouchStart = (event: ReactTouchEvent<HTMLDivElement>) => {
    if (event.touches.length === 2) {
      const [first, second] = Array.from(event.touches);
      if (!isPointInsideImage(first.clientX, first.clientY) || !isPointInsideImage(second.clientX, second.clientY)) {
        return;
      }
      event.preventDefault();
      const center = touchCenter(first, second);
      pinchStartRef.current = {
        distance: touchDistance(first, second),
        scale: scaleRef.current,
        offsetX: offsetRef.current.x,
        offsetY: offsetRef.current.y,
        centerX: center.x,
        centerY: center.y,
      };
      touchPanStartRef.current = null;
      dragMovedRef.current = false;
      return;
    }
    if (event.touches.length !== 1 || scaleRef.current <= 1) {
      return;
    }
    const [touch] = Array.from(event.touches);
    if (!isPointInsideImage(touch.clientX, touch.clientY)) {
      return;
    }
    touchPanStartRef.current = {
      pointerX: touch.clientX,
      pointerY: touch.clientY,
      offsetX: offsetRef.current.x,
      offsetY: offsetRef.current.y,
    };
    dragMovedRef.current = false;
  };

  const handleTouchMove = (event: ReactTouchEvent<HTMLDivElement>) => {
    if (event.touches.length === 2 && pinchStartRef.current) {
      const [first, second] = Array.from(event.touches);
      event.preventDefault();
      const nextDistance = touchDistance(first, second);
      const nextCenter = touchCenter(first, second);
      const nextScale = clampScale(pinchStartRef.current.scale * (nextDistance / Math.max(pinchStartRef.current.distance, 1)));
      const scaleRatio = nextScale / Math.max(pinchStartRef.current.scale, 0.0001);
      const nextOffset = {
        x: nextCenter.x - (pinchStartRef.current.centerX - pinchStartRef.current.offsetX) * scaleRatio,
        y: nextCenter.y - (pinchStartRef.current.centerY - pinchStartRef.current.offsetY) * scaleRatio,
      };
      dragMovedRef.current = true;
      setScale(nextScale);
      setOffset(nextScale <= 1 ? { x: 0, y: 0 } : nextOffset);
      return;
    }
    if (event.touches.length !== 1 || !touchPanStartRef.current || scaleRef.current <= 1) {
      return;
    }
    const [touch] = Array.from(event.touches);
    event.preventDefault();
    if (Math.abs(touch.clientX - touchPanStartRef.current.pointerX) > 3 || Math.abs(touch.clientY - touchPanStartRef.current.pointerY) > 3) {
      dragMovedRef.current = true;
    }
    setOffset({
      x: touchPanStartRef.current.offsetX + touch.clientX - touchPanStartRef.current.pointerX,
      y: touchPanStartRef.current.offsetY + touch.clientY - touchPanStartRef.current.pointerY,
    });
  };

  const handleTouchEnd = () => {
    if (dragMovedRef.current) {
      suppressNextClickRef.current = true;
    }
    if (scaleRef.current <= 1) {
      setOffset({ x: 0, y: 0 });
    }
    resetGestureState();
  };

  return (
    <>
      <button
        type="button"
        title={title}
        className={className}
        onClick={() => {
          setActiveIndex(initialIndex);
          setIsOpen(true);
        }}
      >
        {children}
      </button>
      {isOpen && typeof document !== "undefined"
        ? createPortal(
            <div
              className={`fixed inset-0 z-[80] flex items-center justify-center bg-stone-950/78 p-4 backdrop-blur-sm ${scale > 1 ? "cursor-grab active:cursor-grabbing" : ""}`}
              onClick={(event) => {
                if (suppressNextClickRef.current) {
                  suppressNextClickRef.current = false;
                  return;
                }
                const imageRect = imageRef.current?.getBoundingClientRect();
                if (!imageRect || event.clientX < imageRect.left || event.clientX > imageRect.right || event.clientY < imageRect.top || event.clientY > imageRect.bottom) {
                  setIsOpen(false);
                }
              }}
              onMouseDown={(event) => {
                if (scaleRef.current <= 1 || event.button !== 0) return;
                const imageRect = imageRef.current?.getBoundingClientRect();
                if (!imageRect || event.clientX < imageRect.left || event.clientX > imageRect.right || event.clientY < imageRect.top || event.clientY > imageRect.bottom) {
                  return;
                }
                event.preventDefault();
                dragMovedRef.current = false;
                dragStartRef.current = {
                  pointerX: event.clientX,
                  pointerY: event.clientY,
                  offsetX: offset.x,
                  offsetY: offset.y,
                };
              }}
              onTouchStart={handleTouchStart}
              onTouchMove={handleTouchMove}
              onTouchEnd={handleTouchEnd}
              onTouchCancel={handleTouchEnd}
              style={{ touchAction: "none" }}
            >
              <style>{`@keyframes original-preview-pop { from { opacity: 0; transform: scale(0.96); } to { opacity: 1; transform: scale(1); } }`}</style>
              <div
                className="pointer-events-none relative flex h-full w-full items-center justify-center"
                style={{ animation: "original-preview-pop 180ms ease-out" }}
              >
                <div className="relative flex h-full w-full items-center justify-center overflow-visible rounded-[18px]">
                  {isLoading && !displayUrl ? (
                    <div className="inline-flex items-center gap-2 rounded-full bg-white/90 px-4 py-2 text-sm font-medium text-stone-700 shadow-sm">
                      <LoaderCircle className="size-4 animate-spin" />
                      正在加载原图
                    </div>
                  ) : null}
                  {displayUrl ? (
                    <img
                      ref={imageRef}
                      src={displayUrl}
                      alt={activeTitle}
                      className={`pointer-events-auto max-h-[92vh] max-w-[calc(96vw-7rem)] rounded-[18px] bg-white object-contain shadow-2xl transition-transform duration-100 ${scale > 1 ? "cursor-grab active:cursor-grabbing" : "cursor-zoom-in"}`}
                      style={{ transform: `translate(${offset.x}px, ${offset.y}px) scale(${scale})`, touchAction: "none" }}
                      onClick={(event) => event.stopPropagation()}
                      draggable={false}
                    />
                  ) : null}
                  {previewItems.length > 1 ? (
                    <>
                      <button
                        type="button"
                        className="pointer-events-auto absolute left-[max(1rem,calc(50%-48vw+1rem))] top-1/2 z-10 inline-flex size-11 -translate-y-1/2 items-center justify-center rounded-full bg-white/90 text-stone-700 shadow-sm transition hover:bg-white hover:text-stone-950 disabled:cursor-not-allowed disabled:opacity-35"
                        onMouseDown={(event) => event.stopPropagation()}
                        onClick={(event) => {
                          event.stopPropagation();
                          setActiveIndex((current) => Math.max(0, current - 1));
                        }}
                        disabled={!hasPrevious}
                        aria-label="查看上一张原图"
                        title="上一张"
                      >
                        <ChevronLeft className="size-6" />
                      </button>
                      <button
                        type="button"
                        className="pointer-events-auto absolute right-[max(1rem,calc(50%-48vw+1rem))] top-1/2 z-10 inline-flex size-11 -translate-y-1/2 items-center justify-center rounded-full bg-white/90 text-stone-700 shadow-sm transition hover:bg-white hover:text-stone-950 disabled:cursor-not-allowed disabled:opacity-35"
                        onMouseDown={(event) => event.stopPropagation()}
                        onClick={(event) => {
                          event.stopPropagation();
                          setActiveIndex((current) => Math.min(previewItems.length - 1, current + 1));
                        }}
                        disabled={!hasNext}
                        aria-label="查看下一张原图"
                        title="下一张"
                      >
                        <ChevronRight className="size-6" />
                      </button>
                      <div className="absolute bottom-3 left-1/2 z-10 -translate-x-1/2 rounded-full bg-white/90 px-3 py-1 text-xs font-medium text-stone-600 shadow-sm">
                        {boundedActiveIndex + 1} / {previewItems.length}
                      </div>
                    </>
                  ) : null}
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
