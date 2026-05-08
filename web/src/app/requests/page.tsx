"use client";

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Activity, ChevronLeft, ChevronRight, Copy, Info, RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { fetchRequestLogs, type RequestLogItem } from "@/lib/api";
import { cn } from "@/lib/utils";

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value || "—";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(date);
}

function promptText(item: RequestLogItem) {
  if (item.prompt?.trim()) return item.prompt.trim();
  if (typeof item.promptLength === "number" && item.promptLength > 0) return `${item.promptLength} 字`;
  return "—";
}

export default function RequestsPage() {
  const [items, setItems] = useState<RequestLogItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState("20");
  const [total, setTotal] = useState(0);
  const [userDraft, setUserDraft] = useState("");
  const [accountDraft, setAccountDraft] = useState("");
  const [promptDraft, setPromptDraft] = useState("");
  const [filters, setFilters] = useState({ user: "", account: "", prompt: "" });
  const [detailItem, setDetailItem] = useState<RequestLogItem | null>(null);

  const numericPageSize = Number(pageSize);
  const pageCount = Math.max(1, Math.ceil(total / numericPageSize));
  const safePage = Math.min(page, pageCount);
  const startIndex = total === 0 ? 0 : (safePage - 1) * numericPageSize + 1;
  const endIndex = Math.min(safePage * numericPageSize, total);

  const loadItems = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await fetchRequestLogs({ page: safePage, pageSize: numericPageSize, ...filters });
      setItems(data.items || []);
      setTotal(data.total || 0);
      setPage(data.page || safePage);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取调用请求失败");
    } finally {
      setIsLoading(false);
    }
  }, [filters, numericPageSize, safePage]);

  useEffect(() => {
    void loadItems();
  }, [loadItems]);

  const paginationItems = useMemo(() => {
    const nextItems: (number | "...")[] = [];
    const start = Math.max(1, safePage - 1);
    const end = Math.min(pageCount, safePage + 1);

    if (start > 1) nextItems.push(1);
    if (start > 2) nextItems.push("...");
    for (let current = start; current <= end; current += 1) {
      nextItems.push(current);
    }
    if (end < pageCount - 1) nextItems.push("...");
    if (end < pageCount) nextItems.push(pageCount);

    return nextItems;
  }, [pageCount, safePage]);

  const handleFilterSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPage(1);
    setFilters({ user: userDraft.trim(), account: accountDraft.trim(), prompt: promptDraft.trim() });
  };

  const copyPrompt = async (item: RequestLogItem) => {
    const prompt = item.prompt?.trim();
    if (!prompt) {
      toast.error("当前记录没有可复制的提示词");
      return;
    }
    await navigator.clipboard.writeText(prompt);
    toast.success("提示词已复制");
  };

  return (
    <section className="h-full">
      <div className="hide-scrollbar h-full min-h-0 overflow-y-auto rounded-[30px] border border-stone-200 bg-[#fcfcfb] px-4 py-5 shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 sm:py-6 lg:flex lg:min-h-0 lg:flex-col lg:px-6 lg:py-7">
        <section className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
          <div className="flex items-center gap-4">
            <div className="inline-flex size-12 items-center justify-center rounded-[18px] bg-stone-950 text-white shadow-sm">
              <Activity className="size-5" />
            </div>
            <div className="space-y-1">
              <h1 className="text-2xl font-semibold tracking-tight text-stone-950">调用请求</h1>
              <p className="text-xs text-stone-500">共 {total} 条持久化记录</p>
            </div>
          </div>
          <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
            <form className="grid gap-2 sm:grid-cols-3 lg:flex" onSubmit={handleFilterSubmit}>
              <Input value={userDraft} onChange={(event) => setUserDraft(event.target.value)} placeholder="按用户筛选" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none" />
              <Input value={accountDraft} onChange={(event) => setAccountDraft(event.target.value)} placeholder="按账号筛选" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none" />
              <Input value={promptDraft} onChange={(event) => setPromptDraft(event.target.value)} placeholder="搜索提示词" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none" />
              <Button type="submit" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none">
                <Search className="size-4" />筛选
              </Button>
            </form>
            <Button type="button" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-stone-700 shadow-none" onClick={() => void loadItems()} disabled={isLoading}>
              {isLoading ? <RefreshCw className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
              刷新记录
            </Button>
          </div>
        </section>

        <Card className="mt-5 overflow-hidden rounded-2xl border-white/80 bg-white/90 shadow-sm lg:flex-1 lg:min-h-0">
          <CardContent className="p-0 lg:flex lg:h-full lg:min-h-0 lg:flex-col">
            <div className="space-y-4 p-4 lg:hidden">
              {items.map((item) => (
                <div key={item.id} className="rounded-2xl border border-stone-200/80 bg-stone-50/60 p-4">
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="font-medium text-stone-700">{formatTime(item.startedAt)}</div>
                      <div className="text-xs text-stone-400">{item.finishedAt ? formatTime(item.finishedAt) : "进行中"}</div>
                    </div>
                    <Badge variant={item.success ? "success" : "danger"} className="w-fit shrink-0 rounded-md px-2 py-1">
                      {item.success ? "成功" : "失败"}
                    </Badge>
                  </div>

                  <div className="mt-3 flex flex-wrap gap-2">
                    <Badge variant="secondary" className="rounded-md bg-stone-100 text-stone-700">{item.operation || "—"}</Badge>
                    <Badge variant={item.success ? "success" : "danger"} className="rounded-md px-2 py-1">{item.success ? "成功" : "失败"}</Badge>
                    <Button type="button" variant="outline" size="sm" className="h-7 rounded-lg border-stone-200 bg-white px-2 text-xs" onClick={() => setDetailItem(item)}>
                      <Info className="size-3.5" />详情
                    </Button>
                  </div>

                  <div className="mt-3 grid grid-cols-1 gap-3 text-sm text-stone-600 sm:grid-cols-2">
                    <InfoBox title="路由" main={item.route || "—"} sub={`CPA 子路由：${item.cpaSubroute || "—"}`} />
                    <InfoBox title="参数" main={item.size || "—"} sub={item.quality ? `quality: ${item.quality}` : "quality: —"} />
                    <InfoBox title="结果" main={item.success ? "成功" : "失败"} />
                    <InfoBox title="错误" main={item.error || "—"} breakAll />
                    <PromptBox item={item} onCopy={copyPrompt} className="sm:col-span-2" />
                    <InfoBox title="用户" main={item.username || item.userId || "—"} sub={item.userRole || "—"} />
                    <InfoBox title="账号" main={item.accountEmail || "—"} sub={item.accountType ? `${item.accountType} · ${item.accountFile || "—"}` : item.accountFile || "—"} />
                  </div>
                </div>
              ))}
            </div>

            <div className="hidden lg:flex lg:min-h-0 lg:flex-1 lg:flex-col">
              <div className="min-h-0 flex-1 overflow-auto border-t border-stone-100">
                <div className="min-h-full">
                  <table className="w-full min-w-[1180px] text-left">
                    <thead className="sticky top-0 z-10 border-b border-stone-100 bg-white/95 text-[11px] uppercase tracking-[0.18em] text-stone-400 backdrop-blur-sm">
                      <tr>
                        <th className="px-4 py-3 whitespace-nowrap">时间</th>
                        <th className="px-4 py-3 whitespace-nowrap">操作</th>
                        <th className="px-4 py-3 whitespace-nowrap">路由</th>
                        <th className="px-4 py-3 whitespace-nowrap">参数</th>
                        <th className="px-4 py-3 whitespace-nowrap">结果</th>
                        <th className="px-4 py-3">错误</th>
                        <th className="px-4 py-3 whitespace-nowrap">提示词</th>
                        <th className="px-4 py-3 whitespace-nowrap">用户</th>
                        <th className="px-4 py-3 whitespace-nowrap">账号</th>
                        <th className="px-4 py-3 whitespace-nowrap">详情</th>
                      </tr>
                    </thead>
                    <tbody>
                      {items.map((item) => (
                        <tr key={item.id} className="border-b border-stone-100/80 text-sm text-stone-600 hover:bg-stone-50/70">
                          <td className="px-4 py-3 whitespace-nowrap">
                            <div className="font-medium text-stone-700">{formatTime(item.startedAt)}</div>
                            <div className="text-xs text-stone-400">{item.finishedAt ? formatTime(item.finishedAt) : "进行中"}</div>
                          </td>
                          <td className="px-4 py-3 whitespace-nowrap">{item.operation || "—"}</td>
                          <td className="px-4 py-3 whitespace-nowrap">
                            <div>{item.route || "—"}</div>
                            <div className="text-xs text-stone-400">{item.cpaSubroute || "—"}</div>
                          </td>
                          <td className="px-4 py-3 whitespace-nowrap">
                            <div className="text-stone-700">{item.size || "—"}</div>
                            <div className="text-xs text-stone-400">{item.quality ? `quality: ${item.quality}` : "quality: —"}</div>
                            <div className="text-xs text-stone-400">{typeof item.promptLength === "number" && item.promptLength > 0 ? `${item.promptLength} 字` : "prompt: —"}</div>
                          </td>
                          <td className="px-4 py-3 whitespace-nowrap"><Badge variant={item.success ? "success" : "danger"} className="rounded-md px-2 py-1">{item.success ? "成功" : "失败"}</Badge></td>
                          <td className="px-4 py-3"><div className="max-w-[220px] truncate text-xs text-stone-500" title={item.error || ""}>{item.error || "—"}</div></td>
                          <td className="px-4 py-3"><PromptCell item={item} onCopy={copyPrompt} /></td>
                          <td className="px-4 py-3 whitespace-nowrap">
                            <div className="truncate text-stone-700" title={item.username || item.userId || ""}>{item.username || item.userId || "—"}</div>
                            <div className="truncate text-xs text-stone-400">{item.userRole || "—"}</div>
                          </td>
                          <td className="px-4 py-3 whitespace-nowrap">
                            <div className="truncate text-stone-700" title={item.accountEmail || item.accountFile || ""}>{item.accountEmail || "—"}</div>
                            <div className="truncate text-xs text-stone-400" title={item.accountFile || ""}>{item.accountType ? `${item.accountType} · ${item.accountFile || "—"}` : item.accountFile || "—"}</div>
                          </td>
                          <td className="px-4 py-3 whitespace-nowrap">
                            <Button type="button" variant="outline" size="sm" className="h-8 rounded-lg border-stone-200 bg-white px-2 text-xs" onClick={() => setDetailItem(item)}>
                              <Info className="size-3.5" />详情
                            </Button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>

            {total > 0 ? (
              <div className="border-t border-stone-100 px-4 py-4">
                <div className="flex items-center justify-center gap-3 overflow-x-auto whitespace-nowrap">
                  <div className="shrink-0 text-sm text-stone-500">显示第 {startIndex} - {endIndex} 条，共 {total} 条</div>
                  <span className="shrink-0 text-sm leading-none text-stone-500">{safePage} / {pageCount} 页</span>
                  <Select value={pageSize} onValueChange={(value) => { setPageSize(value); setPage(1); }}>
                    <SelectTrigger className="h-10 w-[108px] shrink-0 rounded-lg border-stone-200 bg-white text-sm leading-none"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="10">10 / 页</SelectItem>
                      <SelectItem value="20">20 / 页</SelectItem>
                      <SelectItem value="50">50 / 页</SelectItem>
                      <SelectItem value="100">100 / 页</SelectItem>
                    </SelectContent>
                  </Select>
                  <Button variant="outline" size="icon" className="size-10 shrink-0 rounded-lg border-stone-200 bg-white" disabled={safePage <= 1 || isLoading} onClick={() => setPage((prev) => Math.max(1, prev - 1))}><ChevronLeft className="size-4" /></Button>
                  {paginationItems.map((item, index) => item === "..." ? (
                    <span key={`ellipsis-${index}`} className="px-1 text-sm text-stone-400">...</span>
                  ) : (
                    <Button key={item} variant={item === safePage ? "default" : "outline"} className={cn("h-10 min-w-10 shrink-0 rounded-lg px-3", item === safePage ? "bg-stone-950 text-white hover:bg-stone-800" : "border-stone-200 bg-white text-stone-700")} onClick={() => setPage(item)} disabled={isLoading}>{item}</Button>
                  ))}
                  <Button variant="outline" size="icon" className="size-10 shrink-0 rounded-lg border-stone-200 bg-white" disabled={safePage >= pageCount || isLoading} onClick={() => setPage((prev) => Math.min(pageCount, prev + 1))}><ChevronRight className="size-4" /></Button>
                </div>
              </div>
            ) : null}

            {detailItem ? (
              <div className="fixed inset-0 z-50 grid place-items-center bg-black/30 px-4" onClick={() => setDetailItem(null)}>
                <div className="w-full max-w-2xl rounded-[24px] border border-stone-200 bg-white p-5 shadow-xl" onClick={(event) => event.stopPropagation()}>
                  <div className="mb-4 flex items-center justify-between gap-3">
                    <h2 className="text-base font-semibold text-stone-950">调用详情</h2>
                    <Button type="button" variant="outline" className="h-8 rounded-full px-3 text-xs" onClick={() => setDetailItem(null)}>关闭</Button>
                  </div>
                  <div className="grid gap-3 text-sm sm:grid-cols-2">
                    <InfoBox title="模式" main={detailItem.imageMode || "studio"} />
                    <InfoBox title="方向" main={detailItem.direction === "cpa" ? "CPA" : detailItem.direction || "官方"} />
                    <InfoBox title="接口" main={detailItem.endpoint || "—"} className="sm:col-span-2" breakAll />
                    <InfoBox title="请求模型" main={detailItem.requestedModel || "—"} />
                    <InfoBox title="上游模型" main={detailItem.upstreamModel || "—"} />
                    <InfoBox title="工具模型" main={detailItem.imageToolModel || "—"} />
                    <InfoBox title="排队" main={`${detailItem.queueWaitMs ?? 0} ms`} sub={`并发：${detailItem.inflightCountAtStart ?? 0}`} />
                    <InfoBox title="路由策略" main={detailItem.routingPolicyApplied ? "已应用" : "未应用"} sub={`分组：${detailItem.routingGroupIndex ?? "—"} · 排序：${detailItem.routingSortMode || "—"}`} />
                    <InfoBox title="错误码" main={detailItem.errorCode || "—"} />
                  </div>
                </div>
              </div>
            ) : null}

            {!isLoading && total === 0 ? (
              <div className="flex flex-col items-center justify-center gap-3 px-6 py-16 text-center lg:flex-1">
                <div className="rounded-2xl bg-stone-100 p-3 text-stone-500"><Activity className="size-5" /></div>
                <div className="space-y-1">
                  <p className="text-sm font-medium text-stone-700">还没有调用记录</p>
                  <p className="text-sm text-stone-500">发起一次图片请求后，这里会显示它到底走的是官方还是 CPA。</p>
                </div>
              </div>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </section>
  );
}

function PromptCell({ item, onCopy }: { item: RequestLogItem; onCopy: (item: RequestLogItem) => void }) {
  return (
    <div className="flex max-w-[280px] items-center gap-2">
      <div className="min-w-0 flex-1 truncate text-xs text-stone-500" title={item.prompt || ""}>{promptText(item)}</div>
      {item.prompt?.trim() ? (
        <button type="button" className="inline-flex size-7 shrink-0 items-center justify-center rounded-lg text-stone-400 hover:bg-stone-100 hover:text-stone-700" onClick={() => void onCopy(item)} title="复制提示词" aria-label="复制提示词">
          <Copy className="size-3.5" />
        </button>
      ) : null}
    </div>
  );
}

function PromptBox({ item, onCopy, className }: { item: RequestLogItem; onCopy: (item: RequestLogItem) => void; className?: string }) {
  return (
    <div className={cn("rounded-xl bg-white px-3 py-2", className)}>
      <div className="flex items-center justify-between gap-2">
        <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">提示词</div>
        {item.prompt?.trim() ? (
          <button type="button" className="inline-flex size-7 items-center justify-center rounded-lg text-stone-400 hover:bg-stone-100 hover:text-stone-700" onClick={() => void onCopy(item)} title="复制提示词" aria-label="复制提示词">
            <Copy className="size-3.5" />
          </button>
        ) : null}
      </div>
      <div className="mt-1 break-all text-stone-700" title={item.prompt || ""}>{promptText(item)}</div>
    </div>
  );
}

function InfoBox({ title, main, sub, className, breakAll }: { title: string; main: string; sub?: string; className?: string; breakAll?: boolean }) {
  return (
    <div className={cn("rounded-xl bg-white px-3 py-2", className)}>
      <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">{title}</div>
      <div className={cn("mt-1 text-stone-700", breakAll ? "break-all" : "truncate")} title={main}>{main}</div>
      {sub ? <div className="mt-1 truncate text-xs text-stone-400" title={sub}>{sub}</div> : null}
    </div>
  );
}
