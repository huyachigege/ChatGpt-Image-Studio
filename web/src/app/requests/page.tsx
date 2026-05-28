"use client";

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Activity, ChevronLeft, ChevronRight, Copy, Info, RefreshCw, Search, Trash2 } from "lucide-react";
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
import { deleteFailedRequestLogs, deleteRequestLogs, deleteRequestLogsBefore, fetchRequestLogDetail, fetchRequestLogFilters, fetchRequestLogs, type RequestLogDetail, type RequestLogFilterOptions, type RequestLogItem, type RetentionMonths } from "@/lib/api";
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
  if (typeof item.promptLength === "number" && item.promptLength > 0) return `${item.promptLength} 字`;
  return "—";
}

function looksLikeAccessToken(value?: string) {
  const trimmed = value?.trim() || "";
  return trimmed.length > 80 || trimmed.startsWith("eyJ");
}

function accountMainText(item: RequestLogItem | RequestLogDetail) {
  if (item.accountEmail?.trim()) return item.accountEmail.trim();
  if (!looksLikeAccessToken(item.accountFile)) return item.accountFile?.trim() || "—";
  return "—";
}

function accountSubText(item: RequestLogItem | RequestLogDetail) {
  const accountFile = looksLikeAccessToken(item.accountFile) ? "" : item.accountFile?.trim() || "";
  if (item.accountType && accountFile) return `${item.accountType} · ${accountFile}`;
  return item.accountType || accountFile || "—";
}

function optionTextMatches(option: { value: string; label: string }, query: string) {
  const keyword = query.trim().toLowerCase();
  if (!keyword) return true;
  return `${option.value} ${option.label}`.toLowerCase().includes(keyword);
}

export default function RequestsPage() {
  const [items, setItems] = useState<RequestLogItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState("20");
  const [total, setTotal] = useState(0);
  const [userDraft, setUserDraft] = useState("all");
  const [accountDraft, setAccountDraft] = useState("all");
  const [userOptionQuery, setUserOptionQuery] = useState("");
  const [accountOptionQuery, setAccountOptionQuery] = useState("");
  const [promptDraft, setPromptDraft] = useState("");
  const [filters, setFilters] = useState({ user: "", account: "", prompt: "" });
  const [filterOptions, setFilterOptions] = useState<RequestLogFilterOptions>({ users: [], accounts: [] });
  const [detailItem, setDetailItem] = useState<RequestLogDetail | null>(null);
  const [isDetailLoading, setIsDetailLoading] = useState(false);
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [isDeleting, setIsDeleting] = useState(false);

  const numericPageSize = Number(pageSize);
  const pageCount = Math.max(1, Math.ceil(total / numericPageSize));
  const safePage = Math.min(page, pageCount);
  const startIndex = total === 0 ? 0 : (safePage - 1) * numericPageSize + 1;
  const endIndex = Math.min(safePage * numericPageSize, total);

  const loadItems = useCallback(async (targetPage = safePage) => {
    setIsLoading(true);
    try {
      const data = await fetchRequestLogs({ page: targetPage, pageSize: numericPageSize, ...filters });
      setItems(data.items || []);
      setSelectedIds((prev) => prev.filter((id) => (data.items || []).some((item) => item.id === id)));
      setTotal(data.total || 0);
      setPage(data.page || targetPage);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取调用请求失败");
    } finally {
      setIsLoading(false);
    }
  }, [filters, numericPageSize, safePage]);

  useEffect(() => {
    void loadItems();
  }, [loadItems]);

  useEffect(() => {
    fetchRequestLogFilters().then((data) => setFilterOptions(data)).catch(() => {});
  }, []);

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

  const visibleUserOptions = useMemo(() => filterOptions.users.filter((option) => optionTextMatches(option, userOptionQuery)), [filterOptions.users, userOptionQuery]);
  const visibleAccountOptions = useMemo(() => filterOptions.accounts.filter((option) => optionTextMatches(option, accountOptionQuery)), [filterOptions.accounts, accountOptionQuery]);

  const handleFilterSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPage(1);
    setFilters({
      user: userDraft === "all" ? "" : userDraft,
      account: accountDraft === "all" ? "" : accountDraft,
      prompt: promptDraft.trim(),
    });
  };

  const currentPageIds = useMemo(() => items.map((item) => item.id), [items]);
  const isAllCurrentPageSelected = currentPageIds.length > 0 && currentPageIds.every((id) => selectedIds.includes(id));

  const copyPrompt = async (prompt?: string) => {
    if (!prompt?.trim()) {
      toast.error("当前记录没有可复制的提示词");
      return;
    }
    await navigator.clipboard.writeText(prompt.trim());
    toast.success("提示词已复制");
  };

  const showDetail = async (id: string) => {
    setIsDetailLoading(true);
    setDetailItem(null);
    try {
      const detail = await fetchRequestLogDetail(id);
      setDetailItem(detail);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取详情失败");
    } finally {
      setIsDetailLoading(false);
    }
  };

  const toggleSelected = (id: string, checked: boolean) => {
    setSelectedIds((prev) => checked ? Array.from(new Set([...prev, id])) : prev.filter((item) => item !== id));
  };

  const toggleCurrentPageSelected = (checked: boolean) => {
    setSelectedIds((prev) => {
      if (!checked) {
        return prev.filter((id) => !currentPageIds.includes(id));
      }
      return Array.from(new Set([...prev, ...currentPageIds]));
    });
  };

  const handleDeleteSelected = async (ids: string[]) => {
    if (ids.length === 0 || isDeleting) {
      return;
    }
    if (!window.confirm(`确定删除 ${ids.length} 条调用请求记录吗？`)) {
      return;
    }
    setIsDeleting(true);
    try {
      const result = await deleteRequestLogs(ids);
      setSelectedIds((prev) => prev.filter((id) => !(result.deleted || []).includes(id)));
      toast.success(`已删除 ${result.deleted?.length || 0} 条记录`);
      await loadItems();
      fetchRequestLogFilters().then((data) => setFilterOptions(data)).catch(() => {});
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除调用请求失败");
    } finally {
      setIsDeleting(false);
    }
  };

  const handleDeleteFailed = async () => {
    if (isDeleting) {
      return;
    }
    if (!window.confirm("确定删除所有失败的调用请求记录吗？该操作不可恢复。")) {
      return;
    }
    setIsDeleting(true);
    try {
      const result = await deleteFailedRequestLogs();
      setSelectedIds([]);
      setPage(1);
      toast.success(`已删除 ${result.deletedCount || 0} 条失败记录`);
      await loadItems(1);
      fetchRequestLogFilters().then((data) => setFilterOptions(data)).catch(() => {});
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除失败调用请求失败");
    } finally {
      setIsDeleting(false);
    }
  };

  const handleDeleteBefore = async (months: RetentionMonths) => {
    if (isDeleting) {
      return;
    }
    const label = months === 1 ? "一个月" : "三个月";
    if (!window.confirm(`确定删除${label}前的调用请求记录吗？该操作不可恢复。`)) {
      return;
    }
    setIsDeleting(true);
    try {
      const result = await deleteRequestLogsBefore(months);
      setSelectedIds([]);
      setPage(1);
      toast.success(`已删除 ${result.deletedCount || 0} 条${label}前记录`);
      await loadItems(1);
      fetchRequestLogFilters().then((data) => setFilterOptions(data)).catch(() => {});
    } catch (error) {
      toast.error(error instanceof Error ? error.message : `删除${label}前调用请求失败`);
    } finally {
      setIsDeleting(false);
    }
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
              <Select value={userDraft} onValueChange={setUserDraft}>
                <SelectTrigger className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none lg:w-[170px]"><SelectValue placeholder="选择用户" /></SelectTrigger>
                <SelectContent>
                  <div className="p-1" onKeyDown={(event) => event.stopPropagation()}>
                    <Input value={userOptionQuery} onChange={(event) => setUserOptionQuery(event.target.value)} placeholder="搜索用户" className="h-8 rounded-xl border-stone-200 px-3 text-xs shadow-none" />
                  </div>
                  <SelectItem value="all">全部用户</SelectItem>
                  {visibleUserOptions.length > 0 ? visibleUserOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>) : <div className="px-3 py-2 text-xs text-stone-400">无匹配用户</div>}
                </SelectContent>
              </Select>
              <Select value={accountDraft} onValueChange={setAccountDraft}>
                <SelectTrigger className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none lg:w-[220px]"><SelectValue placeholder="选择账号" /></SelectTrigger>
                <SelectContent>
                  <div className="p-1" onKeyDown={(event) => event.stopPropagation()}>
                    <Input value={accountOptionQuery} onChange={(event) => setAccountOptionQuery(event.target.value)} placeholder="搜索账号" className="h-8 rounded-xl border-stone-200 px-3 text-xs shadow-none" />
                  </div>
                  <SelectItem value="all">全部账号</SelectItem>
                  {visibleAccountOptions.length > 0 ? visibleAccountOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>) : <div className="px-3 py-2 text-xs text-stone-400">无匹配账号</div>}
                </SelectContent>
              </Select>
              <Input value={promptDraft} onChange={(event) => setPromptDraft(event.target.value)} placeholder="搜索提示词" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] shadow-none" />
              <Button type="submit" variant="outline" className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none">
                <Search className="size-4" />筛选
              </Button>
            </form>
            {selectedIds.length > 0 ? (
              <Button type="button" variant="outline" className="h-10 rounded-full border-red-200 bg-white px-4 text-red-600 shadow-none hover:bg-red-50" onClick={() => void handleDeleteSelected(selectedIds)} disabled={isDeleting}>
                <Trash2 className="size-4" />删除选中({selectedIds.length})
              </Button>
            ) : null}
            <Button type="button" variant="outline" className="h-10 rounded-full border-red-200 bg-white px-4 text-red-600 shadow-none hover:bg-red-50" onClick={() => void handleDeleteFailed()} disabled={isDeleting || isLoading || total === 0}>
              <Trash2 className="size-4" />删除失败日志
            </Button>
            <Button type="button" variant="outline" className="h-10 rounded-full border-red-200 bg-white px-4 text-red-600 shadow-none hover:bg-red-50" onClick={() => void handleDeleteBefore(1)} disabled={isDeleting || isLoading || total === 0}>
              <Trash2 className="size-4" />删除一个月前
            </Button>
            <Button type="button" variant="outline" className="h-10 rounded-full border-red-200 bg-white px-4 text-red-600 shadow-none hover:bg-red-50" onClick={() => void handleDeleteBefore(3)} disabled={isDeleting || isLoading || total === 0}>
              <Trash2 className="size-4" />删除三个月前
            </Button>
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
                  <div className="mb-3 flex items-center justify-between gap-2">
                    <label className="inline-flex items-center gap-2 text-xs text-stone-500">
                      <input type="checkbox" className="size-4 rounded border-stone-300" checked={selectedIds.includes(item.id)} onChange={(event) => toggleSelected(item.id, event.target.checked)} />
                      选择
                    </label>
                    <Button type="button" variant="outline" size="sm" className="h-7 rounded-lg border-red-200 bg-white px-2 text-xs text-red-600" onClick={() => void handleDeleteSelected([item.id])} disabled={isDeleting}>
                      <Trash2 className="size-3.5" />删除
                    </Button>
                  </div>
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
                    <Button type="button" variant="outline" size="sm" className="h-7 rounded-lg border-stone-200 bg-white px-2 text-xs" onClick={() => void showDetail(item.id)}>
                      <Info className="size-3.5" />详情
                    </Button>
                  </div>

                  <div className="mt-3 grid grid-cols-1 gap-3 text-sm text-stone-600 sm:grid-cols-2">
                    <InfoBox title="路由" main={item.route || "—"} sub={`CPA 子路由：${item.cpaSubroute || "—"}`} />
                    <InfoBox title="参数" main={item.size || "—"} sub={item.quality ? `quality: ${item.quality}` : "quality: —"} />
                    <InfoBox title="结果" main={item.success ? "成功" : "失败"} />
                    <InfoBox title="错误" main={item.error || "—"} sub={item.errorCode ? `错误码：${item.errorCode}` : undefined} breakAll />
                    <InfoBox title="提示词" main={promptText(item)} />
                    <InfoBox title="用户" main={item.username || item.userId || "—"} sub={item.userRole || "—"} />
                    <InfoBox title="账号" main={accountMainText(item)} sub={accountSubText(item)} />
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
                        <th className="px-4 py-3 whitespace-nowrap">
                          <input type="checkbox" className="size-4 rounded border-stone-300" checked={isAllCurrentPageSelected} onChange={(event) => toggleCurrentPageSelected(event.target.checked)} aria-label="选择当前页" />
                        </th>
                        <th className="px-4 py-3 whitespace-nowrap">时间</th>
                        <th className="px-4 py-3 whitespace-nowrap">操作</th>
                        <th className="px-4 py-3 whitespace-nowrap">路由</th>
                        <th className="px-4 py-3 whitespace-nowrap">参数</th>
                        <th className="px-4 py-3 whitespace-nowrap">结果</th>
                        <th className="px-4 py-3">错误</th>
                        <th className="px-4 py-3 whitespace-nowrap">提示词</th>
                        <th className="px-4 py-3 whitespace-nowrap">用户</th>
                        <th className="px-4 py-3 whitespace-nowrap">账号</th>
                        <th className="px-4 py-3 whitespace-nowrap">操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {items.map((item) => (
                        <tr key={item.id} className="border-b border-stone-100/80 text-sm text-stone-600 hover:bg-stone-50/70">
                          <td className="px-4 py-3 whitespace-nowrap">
                            <input type="checkbox" className="size-4 rounded border-stone-300" checked={selectedIds.includes(item.id)} onChange={(event) => toggleSelected(item.id, event.target.checked)} aria-label="选择记录" />
                          </td>
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
                          <td className="px-4 py-3 whitespace-nowrap"><div className="max-w-[80px] truncate text-xs text-stone-500" title={item.error || ""}>{item.error || "—"}</div></td>
                          <td className="px-4 py-3 whitespace-nowrap"><div className="text-xs text-stone-500">{promptText(item)}</div></td>
                          <td className="px-4 py-3 whitespace-nowrap">
                            <div className="truncate text-stone-700" title={item.username || item.userId || ""}>{item.username || item.userId || "—"}</div>
                            <div className="truncate text-xs text-stone-400">{item.userRole || "—"}</div>
                          </td>
                          <td className="max-w-[180px] px-4 py-3 whitespace-nowrap">
                            <div className="truncate text-stone-700" title={accountMainText(item)}>{accountMainText(item)}</div>
                            <div className="truncate text-xs text-stone-400" title={accountSubText(item)}>{accountSubText(item)}</div>
                          </td>
                          <td className="px-4 py-3 whitespace-nowrap">
                            <div className="flex items-center gap-2">
                              <Button type="button" variant="outline" size="sm" className="h-8 rounded-lg border-stone-200 bg-white px-2 text-xs" onClick={() => void showDetail(item.id)}>
                                <Info className="size-3.5" />详情
                              </Button>
                              <Button type="button" variant="outline" size="sm" className="h-8 rounded-lg border-red-200 bg-white px-2 text-xs text-red-600" onClick={() => void handleDeleteSelected([item.id])} disabled={isDeleting}>
                                <Trash2 className="size-3.5" />删除
                              </Button>
                            </div>
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

            {(detailItem || isDetailLoading) ? (
              <div className="fixed inset-0 z-50 grid place-items-center bg-black/30 px-4" onClick={() => setDetailItem(null)}>
                <div className="hide-scrollbar max-h-[80vh] w-full max-w-2xl overflow-y-auto rounded-[24px] border border-stone-200 bg-white p-5 shadow-xl" onClick={(event) => event.stopPropagation()}>
                  <div className="mb-4 flex items-center justify-between gap-3">
                    <h2 className="text-base font-semibold text-stone-950">调用详情</h2>
                    <Button type="button" variant="outline" className="h-8 rounded-full px-3 text-xs" onClick={() => setDetailItem(null)}>关闭</Button>
                  </div>
                  {isDetailLoading ? (
                    <div className="flex items-center justify-center py-8 text-sm text-stone-500">加载中...</div>
                  ) : detailItem ? (
                    <>
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
                      {detailItem.error ? (
                        <div className="mt-3 rounded-xl bg-rose-50 px-3 py-2">
                          <div className="text-[11px] uppercase tracking-[0.14em] text-rose-400">完整错误</div>
                          <div className="mt-1 break-all text-xs text-rose-700">{detailItem.error}</div>
                        </div>
                      ) : null}
                      {detailItem.prompt ? (
                        <div className="mt-3 rounded-xl bg-stone-50 px-3 py-2">
                          <div className="flex items-center justify-between">
                            <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">提示词</div>
                            <button type="button" className="inline-flex size-6 items-center justify-center rounded-lg text-stone-400 hover:bg-stone-200 hover:text-stone-700" onClick={() => void copyPrompt(detailItem.prompt)} title="复制提示词" aria-label="复制提示词">
                              <Copy className="size-3.5" />
                            </button>
                          </div>
                          <div className="mt-1 break-all text-xs text-stone-700">{detailItem.prompt}</div>
                        </div>
                      ) : null}
                      {detailItem.upstreamRequest ? (
                        <details className="mt-3 rounded-xl bg-stone-50 px-3 py-2">
                          <summary className="cursor-pointer text-[11px] uppercase tracking-[0.14em] text-stone-400 hover:text-stone-600">上游请求体</summary>
                          <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-all text-[11px] text-stone-600">{detailItem.upstreamRequest}</pre>
                        </details>
                      ) : null}
                      {detailItem.imageNames && detailItem.imageNames.filter((n) => !n.startsWith("data:")).length > 0 ? (
                        <div className="mt-3 rounded-xl bg-stone-50 px-3 py-2">
                          <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">生成文件</div>
                          <div className="mt-1 space-y-1">
                            {detailItem.imageNames.filter((n) => !n.startsWith("data:")).map((name) => (
                              <div key={name} className="break-all text-xs text-stone-700">{name}</div>
                            ))}
                          </div>
                        </div>
                      ) : null}
                      {detailItem.imageUrls && detailItem.imageUrls.filter((u) => !u.startsWith("data:")).length > 0 ? (
                        <div className="mt-3 rounded-xl bg-stone-50 px-3 py-2">
                          <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">文件路径</div>
                          <div className="mt-1 space-y-1">
                            {detailItem.imageUrls.filter((u) => !u.startsWith("data:")).map((url) => (
                              <div key={url} className="break-all text-xs text-stone-700">{url}</div>
                            ))}
                          </div>
                        </div>
                      ) : null}
                    </>
                  ) : null}
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

function InfoBox({ title, main, sub, className, breakAll }: { title: string; main: string; sub?: string; className?: string; breakAll?: boolean }) {
  return (
    <div className={cn("rounded-xl bg-white px-3 py-2", className)}>
      <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">{title}</div>
      <div className={cn("mt-1 text-stone-700", breakAll ? "break-all" : "truncate")} title={main}>{main}</div>
      {sub ? <div className="mt-1 truncate text-xs text-stone-400" title={sub}>{sub}</div> : null}
    </div>
  );
}
