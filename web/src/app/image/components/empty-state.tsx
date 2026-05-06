"use client";

import { RefreshCw, Sparkles } from "lucide-react";

import { AppImage as Image } from "@/components/app-image";
import type { ImagePromptPreset } from "../prompt-presets";

type EmptyStateProps = {
  inspirationExamples: ImagePromptPreset[];
  onApplyPromptExample: (example: ImagePromptPreset) => void;
  onRefreshExamples: () => void;
};

export function EmptyState({ inspirationExamples, onApplyPromptExample, onRefreshExamples }: EmptyStateProps) {
  return (
    <div className="mx-auto flex max-w-[1120px] flex-col gap-8 px-4 py-8 sm:px-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="max-w-[760px]">
          <div className="inline-flex size-14 items-center justify-center rounded-[20px] bg-stone-950 text-white shadow-sm">
            <Sparkles className="size-5" />
          </div>
          <h1 className="mt-6 text-3xl font-semibold tracking-tight text-stone-950 lg:text-5xl">
            从一个提示词，开始完整的图像工作流。
          </h1>
        </div>
        <button
          type="button"
          onClick={onRefreshExamples}
          className="inline-flex h-10 w-fit items-center gap-2 rounded-full border border-stone-200 bg-white px-4 text-sm font-medium text-stone-700 shadow-sm transition hover:border-stone-300 hover:bg-stone-50 hover:text-stone-950"
        >
          <RefreshCw className="size-4" />
          随机换一组
        </button>
      </div>

      <div className="hide-scrollbar flex gap-3 overflow-x-auto pb-1 md:grid md:grid-cols-2 md:overflow-visible xl:grid-cols-4">
        {inspirationExamples.map((example) => (
          <button
            key={example.id}
            type="button"
            onClick={() => onApplyPromptExample(example)}
            className="w-[220px] shrink-0 overflow-hidden rounded-[22px] border border-stone-200 bg-white text-left transition hover:-translate-y-0.5 hover:border-stone-300 hover:shadow-sm md:w-auto"
          >
            {example.imageUrl ? (
              <Image
                src={example.imageUrl}
                alt={example.title}
                width={320}
                height={180}
                unoptimized
                className="h-[7.5rem] w-full bg-stone-100 object-cover md:h-32"
              />
            ) : (
              <div className="h-[7.5rem] bg-stone-100 md:h-32" />
            )}
            <div className="space-y-2 px-4 py-3.5">
              <div className="flex items-center gap-2 text-[11px] text-stone-500">
                <span className="rounded-full bg-stone-100 px-2 py-0.5 font-medium">{example.category}</span>
                <span className="rounded-full bg-stone-100 px-2 py-0.5 font-medium">{example.model}</span>
              </div>
              <div className="line-clamp-2 text-sm font-semibold leading-5 tracking-tight text-stone-900">{example.title}</div>
              <div className="line-clamp-2 text-sm leading-6 text-stone-600">{example.description || example.prompt}</div>
              <div className="border-t border-stone-100 pt-2 text-xs leading-5 text-stone-500">by {example.author}</div>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}
