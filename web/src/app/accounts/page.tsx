"use client";

import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import type { ComponentProps } from "react";
import { Link } from "react-router-dom";
import {
  Ban,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  CircleOff,
  Copy,
  FileUp,
  LoaderCircle,
  Pencil,
  Play,
  RefreshCw,
  Search,
  Square,
  Trash2,
  Shield,
  UserRound,
} from "lucide-react";
import { toast } from "sonner";

import { ImagePolicyCard } from "@/app/accounts/image-policy-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  createAccounts,
  deleteAccounts,
  fetchAccountRefreshProgress,
  fetchAccountQuota,
  fetchAccounts,
  fetchConfig,
  fetchSyncStatus,
  importAccountFiles,
  refreshAllAccounts,
  refreshAccounts,
  runSync,
  stopAccountRefresh,
  testAccountImage,
  updateAccount,
  type AccountImportResponse,
  type Account,
  type AccountRefreshProgress,
  type AccountQuotaResponse,
  type AccountSourceKind,
  type AccountStatus,
  type SyncSource,
  type AccountType,
  type SyncStatus,
  type SyncStatusResponse,
} from "@/lib/api";
import { cn } from "@/lib/utils";
import { Textarea } from "@/components/ui/textarea";

const accountTypeOptions: { label: string; value: AccountType | "all" }[] = [
  { label: "全部类型", value: "all" },
  { label: "Free", value: "Free" },
  { label: "Plus", value: "Plus" },
  { label: "Team", value: "Team" },
  { label: "Pro", value: "Pro" },
];

const accountStatusOptions: { label: string; value: AccountStatus | "all" }[] =
  [
    { label: "全部状态", value: "all" },
    { label: "正常", value: "正常" },
    { label: "限流", value: "限流" },
    { label: "异常", value: "异常" },
    { label: "禁用", value: "禁用" },
  ];

const accountSortOptions = [
  { label: "默认排序", value: "default" },
  { label: "额度从高到低", value: "quota_desc" },
  { label: "额度从低到高", value: "quota_asc" },
] as const;

type AccountSortMode = (typeof accountSortOptions)[number]["value"];

const statusMeta: Record<
  AccountStatus,
  {
    icon: typeof CheckCircle2;
    badge: ComponentProps<typeof Badge>["variant"];
  }
> = {
  正常: { icon: CheckCircle2, badge: "success" },
  限流: { icon: CircleAlert, badge: "warning" },
  异常: { icon: CircleOff, badge: "danger" },
  禁用: { icon: Ban, badge: "secondary" },
};

const syncMeta: Record<
  SyncStatus,
  {
    label: string;
    badge: ComponentProps<typeof Badge>["variant"];
  }
> = {
  synced: { label: "已同步", badge: "success" },
  pending_upload: { label: "待上传", badge: "warning" },
  remote_only: { label: "远端独有", badge: "info" },
  remote_deleted: { label: "远端已删", badge: "danger" },
};

const metricCards = [
  { key: "total", label: "账户总数", color: "text-stone-900", icon: UserRound },
  { key: "authFile", label: "认证文件", color: "text-sky-600", icon: FileUp },
  { key: "token", label: "Token", color: "text-violet-600", icon: Shield },
  {
    key: "active",
    label: "正常账户",
    color: "text-emerald-600",
    icon: CheckCircle2,
  },
  {
    key: "limited",
    label: "限流账户",
    color: "text-orange-500",
    icon: CircleAlert,
  },
  {
    key: "abnormal",
    label: "异常账户",
    color: "text-rose-500",
    icon: CircleOff,
  },
  { key: "disabled", label: "禁用账户", color: "text-stone-500", icon: Ban },
  { key: "quota", label: "剩余额度", color: "text-blue-500", icon: RefreshCw },
] as const;

const sourceKindMeta: Record<
  AccountSourceKind,
  {
    label: string;
    badge: ComponentProps<typeof Badge>["variant"];
  }
> = {
  auth_file: { label: "认证文件", badge: "secondary" },
  token: { label: "Token", badge: "info" },
};

const syncSourceOptions: Array<{ label: string; value: SyncSource }> = [
  { label: "CPA", value: "cpa" },
  { label: "NewAPI", value: "newapi" },
  { label: "Sub2API", value: "sub2api" },
];

function formatCompact(value: number) {
  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}k`;
  }
  return String(value);
}

function formatQuota(value: number) {
  return String(Math.max(0, value));
}

function formatCodexPercent(value?: number | null) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "—";
  }
  return `${Math.max(0, Math.min(100, value)).toFixed(1).replace(/\.0$/, "")}%`;
}

function formatRestoreAt(value?: string | null) {
  if (!value) {
    return { absolute: "—", absoluteShort: "—", relative: "" };
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return { absolute: value, absoluteShort: value, relative: "" };
  }

  const diffMs = Math.max(0, date.getTime() - Date.now());
  const totalHours = Math.ceil(diffMs / (1000 * 60 * 60));
  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;
  const relative = diffMs > 0 ? `剩余 ${days}d ${hours}h` : "已到恢复时间";

  const pad = (num: number) => String(num).padStart(2, "0");
  const absoluteShort = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`;
  const absolute = `${absoluteShort}:${pad(date.getSeconds())}`;

  return { absolute, absoluteShort, relative };
}

function percentAvailable(value?: number | null) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return 0;
  }
  return Math.max(0, Math.min(100, Math.round(100 - value)));
}

function getAccountConversationAvailability(account: Account) {
  const fiveHourAvailable = percentAvailable(account.codex_5h_used_percent);
  const weekAvailable = percentAvailable(account.codex_7d_used_percent);
  return {
    hasAvailableFiveHour: account.codex_5h_window_minutes != null && fiveHourAvailable > 0,
    fiveHourAvailable,
    weekAvailable,
  };
}

function formatQuotaSummary(accounts: Account[]) {
  return formatCompact(
    accounts.reduce((sum, account) => {
      const availability = getAccountConversationAvailability(account);
      return sum + Math.max(availability.fiveHourAvailable, availability.weekAvailable);
    }, 0),
  );
}

function maskToken(token?: string) {
  if (!token) return "—";
  if (token.length <= 18) return token;
  return `${token.slice(0, 16)}...${token.slice(-8)}`;
}

function accountSupportsRoute(account: Account, route: "legacy" | "responses") {
  const routes = account.imageRoutes ?? (account.type === "Free" ? ["legacy"] : ["legacy", "responses"]);
  return routes.includes(route);
}

function defaultImageTestRoute(account: Account) {
  return accountSupportsRoute(account, "responses") ? "responses" : "legacy";
}

function normalizeAccounts(items: Account[]): Account[] {
  return items.map((item) => ({
    ...item,
    sourceKind: item.sourceKind === "token" ? "token" : "auth_file",
    type:
      item.type === "Plus" ||
      item.type === "Team" ||
      item.type === "Pro" ||
      item.type === "Free"
        ? item.type
        : "Free",
  }));
}

function parseTokenInput(value: string) {
  return Array.from(
    new Set(
      value
        .split(/[\s,]+/g)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  );
}

function buildImportSummary(data: AccountImportResponse) {
  const imported = data.imported ?? 0;
  const duplicates = data.duplicates?.length ?? 0;
  const failed = data.failed?.length ?? 0;
  const refreshed = data.refreshed ?? 0;
  return `导入 ${imported} 个，刷新 ${refreshed} 个，重复 ${duplicates} 个，失败 ${failed} 个`;
}

function getAccountRemainingQuota(account: Account) {
  const availability = getAccountConversationAvailability(account);
  if (availability.fiveHourAvailable > 0) {
    return availability.fiveHourAvailable;
  }
  return availability.weekAvailable;
}

function applyQuotaResultToAccount(
  account: Account,
  quota: AccountQuotaResponse,
): Account {
  return {
    ...account,
    status: quota.status,
    type: quota.type,
    quota: quota.quota,
    codex_quota_known: quota.codex_quota_known,
    codex_7d_used_percent: quota.codex_7d_used_percent,
    codex_7d_reset_after_seconds: quota.codex_7d_reset_after_seconds,
    codex_7d_window_minutes: quota.codex_7d_window_minutes,
    codex_5h_used_percent: quota.codex_5h_used_percent,
    codex_5h_reset_after_seconds: quota.codex_5h_reset_after_seconds,
    codex_5h_window_minutes: quota.codex_5h_window_minutes,
  };
}

function normalizeSyncStatus(payload: SyncStatusResponse | null) {
  return {
    source: payload?.source ?? "cpa",
    label: payload?.label ?? "CPA",
    configured: payload?.configured ?? false,
    pullSupported: payload?.pullSupported ?? true,
    pushSupported: payload?.pushSupported ?? true,
    local: payload?.local ?? 0,
    remote: payload?.remote ?? 0,
    pendingPush: payload?.pendingPush ?? 0,
    pendingPull: payload?.pendingPull ?? 0,
    inaccessibleRemote: payload?.inaccessibleRemote ?? 0,
    notes: payload?.notes ?? [],
    lastRun: payload?.lastRun ?? null,
  };
}

export default function AccountsPage() {
  const didLoadRef = useRef(false);
  const importInputRef = useRef<HTMLInputElement>(null);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [query, setQuery] = useState("");
  const [typeFilter, setTypeFilter] = useState<AccountType | "all">("all");
  const [statusFilter, setStatusFilter] = useState<AccountStatus | "all">(
    "all",
  );
  const [sortMode, setSortMode] = useState<AccountSortMode>("default");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState("10");
  const [editingAccount, setEditingAccount] = useState<Account | null>(null);
  const [editType, setEditType] = useState<AccountType>("Free");
  const [editStatus, setEditStatus] = useState<AccountStatus>("正常");
  const [editQuota, setEditQuota] = useState("0");
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [isImporting, setIsImporting] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isUpdating, setIsUpdating] = useState(false);
  const [quotaRefreshingId, setQuotaRefreshingId] = useState<string | null>(
    null,
  );
  const [imageTestingId, setImageTestingId] = useState<string | null>(null);
  const [imageTestResult, setImageTestResult] = useState<Record<string, any>>({});
  const [accountQuotaMap, setAccountQuotaMap] = useState<
    Record<string, AccountQuotaResponse>
  >({});
  const [syncStatus, setSyncStatus] = useState<SyncStatusResponse | null>(null);
  const [syncSource, setSyncSource] = useState<SyncSource>("cpa");
  const [isSyncLoading, setIsSyncLoading] = useState(true);
  const [syncRunningDirection, setSyncRunningDirection] = useState<
    "pull" | "push" | null
  >(null);
  const [imageMode, setImageMode] = useState<"studio" | "cpa" | null>(null);
  const [isTokenDialogOpen, setIsTokenDialogOpen] = useState(false);
  const [tokenInput, setTokenInput] = useState("");
  const [isTokenImporting, setIsTokenImporting] = useState(false);
  const [refreshAllRun, setRefreshAllRun] =
    useState<AccountRefreshProgress | null>(null);
  const [isStoppingRefreshAll, setIsStoppingRefreshAll] = useState(false);

  const loadAccounts = async (silent = false) => {
    if (!silent) {
      setIsLoading(true);
    }
    try {
      const data = await fetchAccounts();
      setAccounts(normalizeAccounts(data.items));
      setAccountQuotaMap((prev) =>
        Object.fromEntries(
          Object.entries(prev).filter(([id]) =>
            data.items.some((item) => item.id === id),
          ),
        ),
      );
      setSelectedIds((prev) =>
        prev.filter((id) => data.items.some((item) => item.id === id)),
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : "加载账户失败";
      toast.error(message);
    } finally {
      if (!silent) {
        setIsLoading(false);
      }
    }
  };

  const loadSync = async (
    source: SyncSource,
    {
      silent = false,
      suppressError = false,
      progressOnly = false,
    }: {
      silent?: boolean;
      suppressError?: boolean;
      progressOnly?: boolean;
    } = {},
  ) => {
    if (!silent) {
      setIsSyncLoading(true);
    }
    try {
      const data = await fetchSyncStatus(source, { progressOnly });
      setSyncStatus((prev) => {
        if (!progressOnly) {
          return data;
        }
        if (!prev) {
          return data;
        }
        return {
          ...prev,
          source: data.source || prev.source,
          label: data.label || prev.label,
          lastRun: data.lastRun ?? prev.lastRun,
        };
      });
    } catch (error) {
      if (!suppressError) {
        const message =
          error instanceof Error ? error.message : "读取同步状态失败";
        toast.error(message);
      }
    } finally {
      if (!silent) {
        setIsSyncLoading(false);
      }
    }
  };

  const loadRuntimeConfig = async () => {
    try {
      const data = await fetchConfig();
      setImageMode(data.chatgpt.imageMode);
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "读取运行配置失败";
      toast.error(message);
    }
  };

  const loadRefreshAllProgress = async () => {
    try {
      const data = await fetchAccountRefreshProgress();
      setRefreshAllRun(data.progress ?? null);
    } catch {
      // 页面首次加载时忽略批量刷新进度读取失败，避免遮盖主流程。
    }
  };

  useEffect(() => {
    if (didLoadRef.current) {
      return;
    }
    didLoadRef.current = true;
    void Promise.all([
      loadAccounts(),
      loadSync("cpa"),
      loadRuntimeConfig(),
      loadRefreshAllProgress(),
    ]);
  }, []);

  useEffect(() => {
    if (!didLoadRef.current) {
      return;
    }
    void loadSync(syncSource, { silent: false, suppressError: false });
  }, [syncSource]);

  useEffect(() => {
    if (!didLoadRef.current) {
      return;
    }
    if (!refreshAllRun?.running) {
      return;
    }

    let cancelled = false;
    let timer: number | undefined;

    const tick = async () => {
      if (cancelled) {
        return;
      }
      try {
        const data = await fetchAccountRefreshProgress();
        if (cancelled) {
          return;
        }
        setRefreshAllRun(data.progress ?? null);
        if (data.progress && !data.progress.running) {
          await loadAccounts(true);
          if (data.progress.error) {
            toast.error(data.progress.error);
          } else if (data.progress.failed > 0) {
            toast.error(
              `批量刷新完成，成功 ${data.progress.refreshed} 个，失败 ${data.progress.failed} 个`,
            );
          } else {
            toast.success(
              `批量刷新完成，共刷新 ${data.progress.refreshed} 个账户`,
            );
          }
          return;
        }
      } catch (error) {
        if (!cancelled) {
          const message =
            error instanceof Error ? error.message : "读取批量刷新进度失败";
          toast.error(message);
        }
        return;
      }
      timer = window.setTimeout(tick, 900);
    };

    timer = window.setTimeout(tick, 400);
    return () => {
      cancelled = true;
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
    };
  }, [refreshAllRun?.running]);

  const filteredAccounts = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    const filtered = accounts.filter((account) => {
      const searchMatched =
        normalizedQuery.length === 0 ||
        (account.email ?? "").toLowerCase().includes(normalizedQuery) ||
        (account.fileName ?? "").toLowerCase().includes(normalizedQuery) ||
        (account.note ?? "").toLowerCase().includes(normalizedQuery);
      const typeMatched = typeFilter === "all" || account.type === typeFilter;
      const statusMatched =
        statusFilter === "all" || account.status === statusFilter;
      return searchMatched && typeMatched && statusMatched;
    });

    if (sortMode === "default") {
      return filtered;
    }

    return [...filtered].sort((left, right) => {
      const leftAvailability = getAccountConversationAvailability(left);
      const rightAvailability = getAccountConversationAvailability(right);
      if (sortMode === "quota_desc" && leftAvailability.hasAvailableFiveHour !== rightAvailability.hasAvailableFiveHour) {
        return leftAvailability.hasAvailableFiveHour ? -1 : 1;
      }
      const diff = getAccountRemainingQuota(right) - getAccountRemainingQuota(left);
      if (diff !== 0) {
        return sortMode === "quota_desc" ? diff : -diff;
      }
      return (left.email || left.fileName).localeCompare(right.email || right.fileName);
    });
  }, [accounts, query, sortMode, statusFilter, typeFilter]);

  const pageCount = Math.max(
    1,
    Math.ceil(filteredAccounts.length / Number(pageSize)),
  );
  const safePage = Math.min(page, pageCount);
  const startIndex = (safePage - 1) * Number(pageSize);
  const currentRows = filteredAccounts.slice(
    startIndex,
    startIndex + Number(pageSize),
  );
  const allCurrentSelected =
    currentRows.length > 0 &&
    currentRows.every((row) => selectedIds.includes(row.id));

  const summary = useMemo(() => {
    const total = accounts.length;
    const authFile = accounts.filter(
      (item) => item.sourceKind !== "token",
    ).length;
    const token = accounts.filter((item) => item.sourceKind === "token").length;
    const active = accounts.filter((item) => item.status === "正常").length;
    const limited = accounts.filter((item) => item.status === "限流").length;
    const abnormal = accounts.filter((item) => item.status === "异常").length;
    const disabled = accounts.filter((item) => item.status === "禁用").length;
    const quota = formatQuotaSummary(accounts);

    return {
      total,
      authFile,
      token,
      active,
      limited,
      abnormal,
      disabled,
      quota,
    };
  }, [accounts]);

  const selectedTokens = useMemo(() => {
    const selectedSet = new Set(selectedIds);
    return accounts
      .filter((item) => selectedSet.has(item.id))
      .map((item) => item.access_token);
  }, [accounts, selectedIds]);

  const abnormalTokens = useMemo(() => {
    return accounts
      .filter((item) => item.status === "异常")
      .map((item) => item.access_token);
  }, [accounts]);

  const syncView = useMemo(() => normalizeSyncStatus(syncStatus), [syncStatus]);
  const selectedSyncSourceLabel = useMemo(
    () =>
      syncSourceOptions.find((option) => option.value === syncSource)?.label ??
      syncView.label,
    [syncSource, syncView.label],
  );
  const syncProgress = syncView.lastRun;
  const syncIsRunning = Boolean(syncRunningDirection || syncProgress?.running);
  const refreshAllRunning = Boolean(refreshAllRun?.running);
  const isRefreshingAllAccounts =
    refreshAllRunning && refreshAllRun?.scope !== "routing_groups";
  const isRefreshingRoutingGroups =
    refreshAllRunning && refreshAllRun?.scope === "routing_groups";
  const refreshAllPercent =
    refreshAllRun && refreshAllRun.total > 0
      ? Math.max(
          0,
          Math.min(
            100,
            Math.round((refreshAllRun.processed / refreshAllRun.total) * 100),
          ),
        )
      : 0;
  const isStudioMode = imageMode === "studio";
  const syncProgressTotal = Math.max(
    syncProgress?.total ?? 0,
    syncProgress?.processed ?? 0,
    0,
  );
  const syncProcessedValue = syncProgress?.processed ?? 0;
  const syncProgressProcessed = Math.min(
    syncProcessedValue,
    syncProgressTotal > 0 ? syncProgressTotal : syncProcessedValue,
  );
  const syncProgressPercent =
    syncProgressTotal > 0
      ? Math.max(
          0,
          Math.min(
            100,
            Math.round((syncProgressProcessed / syncProgressTotal) * 100),
          ),
        )
      : 0;
  const syncActiveLabel = useMemo(() => {
    const direction = syncRunningDirection ?? syncProgress?.direction;
    return direction === "pull"
      ? `正在从${selectedSyncSourceLabel}同步`
      : `正在推送至${selectedSyncSourceLabel}`;
  }, [selectedSyncSourceLabel, syncProgress?.direction, syncRunningDirection]);

  useEffect(() => {
    if (!didLoadRef.current) {
      return;
    }
    if (!(syncRunningDirection !== null || syncView.lastRun?.running)) {
      return;
    }

    let cancelled = false;
    let timer: number | undefined;

    const tick = async () => {
      if (cancelled) {
        return;
      }
      await loadSync(syncSource, {
        silent: true,
        suppressError: true,
        progressOnly: true,
      });
      if (cancelled) {
        return;
      }
      timer = window.setTimeout(tick, 900);
    };

    timer = window.setTimeout(tick, 400);
    return () => {
      cancelled = true;
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
    };
  }, [syncRunningDirection, syncSource, syncView.lastRun?.running]);

  const paginationItems = useMemo(() => {
    const items: (number | "...")[] = [];
    const start = Math.max(1, safePage - 1);
    const end = Math.min(pageCount, safePage + 1);

    if (start > 1) items.push(1);
    if (start > 2) items.push("...");
    for (let current = start; current <= end; current += 1) items.push(current);
    if (end < pageCount - 1) items.push("...");
    if (end < pageCount) items.push(pageCount);

    return items;
  }, [pageCount, safePage]);

  const handleImportFiles = async (files: FileList | null) => {
    const normalizedFiles = files ? Array.from(files) : [];
    if (normalizedFiles.length === 0) {
      return;
    }

    setIsImporting(true);
    try {
      const data = await importAccountFiles(normalizedFiles);
      setAccounts(normalizeAccounts(data.items));
      setSelectedIds((prev) =>
        prev.filter((id) => data.items.some((item) => item.id === id)),
      );
      setPage(1);
      await loadSync(syncSource, { silent: true });

      const failedMessage = data.failed?.[0]?.error;
      if ((data.failed?.length ?? 0) > 0) {
        toast.error(
          `${buildImportSummary(data)}${failedMessage ? `，首个错误：${failedMessage}` : ""}`,
        );
      } else if ((data.duplicates?.length ?? 0) > 0) {
        toast.success(`${buildImportSummary(data)}。重复文件已跳过`);
      } else {
        toast.success(buildImportSummary(data));
      }
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "导入认证文件失败";
      toast.error(message);
    } finally {
      setIsImporting(false);
    }
  };

  const handleImportTokens = async () => {
    const tokens = parseTokenInput(tokenInput);
    if (tokens.length === 0) {
      toast.error("请先输入至少一个 access_token");
      return;
    }

    setIsTokenImporting(true);
    try {
      const data = await createAccounts(tokens);
      setAccounts(normalizeAccounts(data.items));
      setSelectedIds((prev) =>
        prev.filter((id) => data.items.some((item) => item.id === id)),
      );
      setTokenInput("");
      setIsTokenDialogOpen(false);
      setPage(1);
      await loadSync(syncSource, { silent: true });

      if (data.errors?.length) {
        const firstError = data.errors[0]?.error;
        toast.error(
          `导入 ${data.added ?? 0} 个，刷新 ${data.refreshed ?? 0} 个，重复 ${data.skipped ?? 0} 个${
            firstError ? `，首个错误：${firstError}` : ""
          }`,
        );
      } else {
        toast.success(
          `导入 ${data.added ?? 0} 个 Token，刷新 ${data.refreshed ?? 0} 个，重复 ${data.skipped ?? 0} 个`,
        );
      }
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "导入 Token 失败";
      toast.error(message);
    } finally {
      setIsTokenImporting(false);
    }
  };

  const handleDeleteTokens = async (tokens: string[]) => {
    if (tokens.length === 0) {
      toast.error("请先选择要删除的账户");
      return;
    }

    setIsDeleting(true);
    try {
      const data = await deleteAccounts(tokens);
      setAccounts(normalizeAccounts(data.items));
      setSelectedIds((prev) =>
        prev.filter((id) => data.items.some((item) => item.id === id)),
      );
      await loadSync(syncSource, { silent: true });
      toast.success(`删除 ${data.removed ?? 0} 个账户`);
    } catch (error) {
      const message = error instanceof Error ? error.message : "删除账户失败";
      toast.error(message);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleRefreshSelectedAccounts = async (accessTokens: string[]) => {
    if (accessTokens.length === 0) {
      toast.error("没有需要刷新的账户");
      return;
    }

    setIsRefreshing(true);
    try {
      const data = await refreshAccounts(accessTokens);
      setAccounts(normalizeAccounts(data.items));
      setSelectedIds((prev) =>
        prev.filter((id) => data.items.some((item) => item.id === id)),
      );
      if (data.errors.length > 0) {
        const firstError = data.errors[0]?.error;
        toast.error(
          `刷新成功 ${data.refreshed} 个，失败 ${data.errors.length} 个${firstError ? `，首个错误：${firstError}` : ""}`,
        );
      } else {
        toast.success(`刷新成功 ${data.refreshed} 个账户`);
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : "刷新账户失败";
      toast.error(message);
    } finally {
      setIsRefreshing(false);
    }
  };

  const handleRefreshAllAccounts = async (
    scope: "all" | "routing_groups" = "all",
  ) => {
    if (accounts.length === 0) {
      toast.error("当前没有可刷新的账户");
      return;
    }
    if (refreshAllRunning || isRefreshing || quotaRefreshingId !== null) {
      toast.error("请等待当前刷新任务完成");
      return;
    }

    try {
      const data = await refreshAllAccounts({ scope });
      setRefreshAllRun(data.progress ?? null);
      if (data.alreadyRunning) {
        toast.success("批量刷新任务已在进行中");
      } else if (scope === "routing_groups") {
        toast.success("已开始刷新参与轮询分组的账号额度");
      } else {
        toast.success("已开始按批次刷新所有账户额度");
      }
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "启动批量刷新失败";
      toast.error(message);
    }
  };

  const handleStopAccountRefresh = async () => {
    if (!refreshAllRunning) {
      return;
    }
    setIsStoppingRefreshAll(true);
    try {
      const data = await stopAccountRefresh();
      setRefreshAllRun(data.progress ?? null);
      toast.success(data.stopped ? "已请求停止刷新任务" : "当前没有正在运行的刷新任务");
    } catch (error) {
      const message = error instanceof Error ? error.message : "停止刷新任务失败";
      toast.error(message);
    } finally {
      setIsStoppingRefreshAll(false);
    }
  };

  const handleRefreshAccountQuota = async (account: Account) => {
    setQuotaRefreshingId(account.id);
    try {
      const data = await fetchAccountQuota(account.id);
      setAccountQuotaMap((prev) => ({
        ...prev,
        [account.id]: data,
      }));
      setAccounts((prev) =>
        prev.map((item) =>
          item.id === account.id ? applyQuotaResultToAccount(item, data) : item,
        ),
      );

      if (data.refresh_error) {
        toast.error(data.refresh_error);
        return;
      }
      toast.success(`对话额度已刷新，5h ${formatCodexPercent(data.codex_5h_used_percent)} · 7d ${formatCodexPercent(data.codex_7d_used_percent)}`);
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "刷新对话额度失败";
      toast.error(message);
    } finally {
      setQuotaRefreshingId(null);
    }
  };

  const handleImageTest = async (account: Account, route = defaultImageTestRoute(account)) => {
    if (route === "responses" && !accountSupportsRoute(account, "responses")) {
      toast.error("Free 号只支持 legacy 测试");
      return;
    }
    setImageTestingId(account.id);
    setImageTestResult((prev) => { const next = { ...prev }; delete next[account.id]; return next; });
    toast(`正在检测 ${route} 路线...`, { duration: 60000, id: `image-test-${account.id}` });
    try {
      const result = await testAccountImage(account.id, route);
      setImageTestResult((prev) => ({ ...prev, [account.id]: result }));
      toast.dismiss(`image-test-${account.id}`);
      if (result.ok) {
        toast.success(`${route} 检测成功 (${result.latencyMs}ms)`);
      } else {
        toast.error(`${route} 检测失败: ${result.error?.slice(0, 100) || "未知错误"}`);
      }
    } catch (error) {
      toast.dismiss(`image-test-${account.id}`);
      const message = error instanceof Error ? error.message : "检测请求失败";
      toast.error(message);
    } finally {
      setImageTestingId(null);
    }
  };

  const handleRunSync = async (direction: "pull" | "push") => {
    setSyncRunningDirection(direction);
    void loadSync(syncSource, {
      silent: true,
      suppressError: true,
      progressOnly: true,
    });
    try {
      const data = await runSync(direction, syncSource);
      if (data.status) {
        setSyncStatus(data.status);
      } else {
        setSyncStatus((prev) =>
          prev
            ? {
                ...prev,
                lastRun: data.result,
              }
            : {
                ...normalizeSyncStatus(null),
                source: syncSource,
                label: selectedSyncSourceLabel,
                lastRun: data.result,
              },
        );
        await loadSync(syncSource, {
          silent: true,
          suppressError: true,
          progressOnly: false,
        });
      }
      const result = data.result;
      if (!result.ok && result.error) {
        toast.error(result.error);
        return;
      }
      if (direction === "pull") {
        toast.success(
          `从 ${selectedSyncSourceLabel} 同步完成：导入 ${result.imported} 个，跳过 ${result.skipped} 个，失败 ${result.failed} 个`,
        );
      } else {
        toast.success(
          `推送至 ${selectedSyncSourceLabel} 完成：导出 ${result.exported} 个，跳过 ${result.skipped} 个，失败 ${result.failed} 个`,
        );
      }
      await loadAccounts(true);
    } catch (error) {
      const message = error instanceof Error ? error.message : "执行同步失败";
      toast.error(message);
    } finally {
      setSyncRunningDirection(null);
    }
  };

  const openEditDialog = (account: Account) => {
    setEditingAccount(account);
    setEditType(account.type);
    setEditStatus(account.status);
    setEditQuota(String(account.quota));
  };

  const handleUpdateAccount = async () => {
    if (!editingAccount) {
      return;
    }

    setIsUpdating(true);
    try {
      const data = await updateAccount(editingAccount.access_token, {
        type: editType,
        status: editStatus,
        quota: Number(editQuota || 0),
      });
      setAccounts(normalizeAccounts(data.items));
      setSelectedIds((prev) =>
        prev.filter((id) => data.items.some((item) => item.id === id)),
      );
      setEditingAccount(null);
      toast.success("账号信息已更新");
    } catch (error) {
      const message = error instanceof Error ? error.message : "更新账号失败";
      toast.error(message);
    } finally {
      setIsUpdating(false);
    }
  };

  const toggleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedIds((prev) =>
        Array.from(new Set([...prev, ...currentRows.map((item) => item.id)])),
      );
      return;
    }
    setSelectedIds((prev) =>
      prev.filter((id) => !currentRows.some((row) => row.id === id)),
    );
  };

  return (
    <div className="hide-scrollbar h-full min-h-0 overflow-y-auto rounded-[30px] border border-stone-200 bg-[#fcfcfb] px-4 py-5 shadow-[0_14px_40px_rgba(15,23,42,0.05)] sm:px-5 sm:py-6 lg:px-6 lg:py-7">
      <section className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-center gap-4">
          <div className="inline-flex size-12 items-center justify-center rounded-[18px] bg-stone-950 text-white shadow-sm">
            <Shield className="size-5" />
          </div>
          <div className="space-y-1">
            <h1 className="text-2xl font-semibold tracking-tight text-stone-950">
              号池管理
            </h1>
          </div>
        </div>
      </section>

      <input
        ref={importInputRef}
        type="file"
        accept="application/json,.json"
        multiple
        className="hidden"
        onChange={(event) => {
          void handleImportFiles(event.target.files);
          event.currentTarget.value = "";
        }}
      />

      <Dialog open={isTokenDialogOpen} onOpenChange={setIsTokenDialogOpen}>
        <DialogContent className="rounded-2xl p-6 sm:max-w-[640px]">
          <DialogHeader className="gap-2">
            <DialogTitle>导入 Token</DialogTitle>
            <DialogDescription className="text-sm leading-6">
              仅 Studio 模式可用。支持一次粘贴多个
              `access_token`，按换行、空格或逗号分隔。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <Textarea
              value={tokenInput}
              onChange={(event) => setTokenInput(event.target.value)}
              placeholder="粘贴一个或多个 access_token"
              className="min-h-[180px] rounded-xl border-stone-200 bg-white text-sm leading-6"
            />
            <div className="text-xs leading-5 text-stone-500">
              Token 账号会与认证文件账号分开统计，不参与 CPA / NewAPI / Sub2API
              的同步和推送，实际出图前会先校验有效性。
            </div>
          </div>
          <DialogFooter className="gap-2 sm:justify-end">
            <Button
              type="button"
              variant="outline"
              className="h-10 rounded-xl border-stone-200 bg-white px-4 text-stone-700"
              onClick={() => setIsTokenDialogOpen(false)}
              disabled={isTokenImporting}
            >
              取消
            </Button>
            <Button
              type="button"
              className="h-10 rounded-xl bg-stone-950 px-4 text-white hover:bg-stone-800"
              onClick={() => void handleImportTokens()}
              disabled={isTokenImporting}
            >
              {isTokenImporting ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <FileUp className="size-4" />
              )}
              导入 Token
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(editingAccount)}
        onOpenChange={(open) => (!open ? setEditingAccount(null) : null)}
      >
        <DialogContent showCloseButton={false} className="rounded-2xl p-6">
          <DialogHeader className="gap-2">
            <DialogTitle>编辑账户</DialogTitle>
            <DialogDescription className="text-sm leading-6">
              手动修改账号状态、类型和额度。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-sm font-medium text-stone-700">状态</label>
              <Select
                value={editStatus}
                onValueChange={(value) => setEditStatus(value as AccountStatus)}
              >
                <SelectTrigger className="h-11 rounded-xl border-stone-200 bg-white">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {accountStatusOptions
                    .filter((option) => option.value !== "all")
                    .map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium text-stone-700">类型</label>
              <Select
                value={editType}
                onValueChange={(value) => setEditType(value as AccountType)}
              >
                <SelectTrigger className="h-11 rounded-xl border-stone-200 bg-white">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {accountTypeOptions
                    .filter((option) => option.value !== "all")
                    .map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium text-stone-700">额度</label>
              <Input
                value={editQuota}
                onChange={(event) => setEditQuota(event.target.value)}
                className="h-11 rounded-xl border-stone-200 bg-white"
              />
            </div>
          </div>
          <DialogFooter className="pt-2">
            <Button
              variant="secondary"
              className="h-10 rounded-xl bg-stone-100 px-5 text-stone-700 hover:bg-stone-200"
              onClick={() => setEditingAccount(null)}
              disabled={isUpdating}
            >
              取消
            </Button>
            <Button
              className="h-10 rounded-xl bg-stone-950 px-5 text-white hover:bg-stone-800"
              onClick={() => void handleUpdateAccount()}
              disabled={isUpdating}
            >
              {isUpdating ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : null}
              保存修改
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <section className="mt-6 space-y-4">
        <Card className="rounded-2xl border-white/80 bg-white/90 shadow-sm">
          <CardContent className="space-y-4 p-5">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
              <div className="space-y-1">
                <h2 className="text-lg font-semibold tracking-tight">
                  {selectedSyncSourceLabel} 同步
                </h2>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Select
                  value={syncSource}
                  onValueChange={(value) => setSyncSource(value as SyncSource)}
                  disabled={syncIsRunning}
                >
                  <SelectTrigger className="h-10 w-[140px] rounded-xl border-stone-200 bg-white px-3 text-stone-700">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {syncSourceOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  variant="outline"
                  className="h-10 rounded-xl border-stone-200 bg-white px-4 text-stone-700"
                  onClick={() => void loadSync(syncSource)}
                  disabled={isSyncLoading || syncIsRunning}
                >
                  <RefreshCw
                    className={cn(
                      "size-4",
                      isSyncLoading ? "animate-spin" : "",
                    )}
                  />
                  刷新同步状态
                </Button>
                <Button
                  className="h-10 rounded-xl bg-stone-950 px-4 text-white hover:bg-stone-800"
                  onClick={() => void handleRunSync("pull")}
                  disabled={
                    !syncView.configured ||
                    !syncView.pullSupported ||
                    isSyncLoading ||
                    syncIsRunning
                  }
                >
                  {syncRunningDirection === "pull" ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <RefreshCw className="size-4" />
                  )}
                  从{selectedSyncSourceLabel}同步
                </Button>
                <Button
                  className="h-10 rounded-xl bg-stone-900 px-4 text-white hover:bg-stone-800"
                  onClick={() => void handleRunSync("push")}
                  disabled={
                    !syncView.configured ||
                    !syncView.pushSupported ||
                    isSyncLoading ||
                    syncIsRunning
                  }
                >
                  {syncRunningDirection === "push" ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <RefreshCw className="size-4" />
                  )}
                  推送至{selectedSyncSourceLabel}
                </Button>
              </div>
            </div>

            {isSyncLoading ? (
              <div className="rounded-2xl border border-stone-200 bg-stone-50 px-4 py-4 text-sm leading-6 text-stone-500">
                正在读取 {syncView.label} 同步状态...
              </div>
            ) : !syncView.configured ? (
              <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-4 text-sm leading-6 text-amber-700">
                当前还没有配置 {syncView.label} 连接。请前往
                <Link
                  to="/settings"
                  className="mx-1 font-medium underline decoration-amber-400 underline-offset-4"
                >
                  配置管理
                </Link>
                补齐对应来源的地址与鉴权信息后再试。
              </div>
            ) : (
              <>
                {syncIsRunning ? (
                  <div className="rounded-2xl border border-sky-200 bg-sky-50 px-4 py-4">
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                      <div className="space-y-1">
                        <div className="flex items-center gap-2 text-sm font-medium text-sky-800">
                          <LoaderCircle className="size-4 animate-spin" />
                          {syncActiveLabel}
                        </div>
                        <div className="text-xs leading-5 text-sky-700">
                          {syncProgress?.phase
                            ? `${syncProgress.phase}`
                            : "正在准备同步任务"}
                          {syncProgress?.current
                            ? ` · 当前处理：${syncProgress.current}`
                            : ""}
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center gap-2 text-xs text-sky-700">
                        <Badge variant="info" className="rounded-lg px-3 py-1">
                          已处理 {syncProgressProcessed}/
                          {syncProgressTotal || "?"}
                        </Badge>
                        <Badge
                          variant="success"
                          className="rounded-lg px-3 py-1"
                        >
                          成功{" "}
                          {(syncProgress?.imported ?? 0) +
                            (syncProgress?.exported ?? 0)}
                        </Badge>
                        <Badge
                          variant="warning"
                          className="rounded-lg px-3 py-1"
                        >
                          跳过 {syncProgress?.skipped ?? 0}
                        </Badge>
                        <Badge
                          variant="danger"
                          className="rounded-lg px-3 py-1"
                        >
                          失败{" "}
                          {(syncProgress?.failed ?? 0) +
                            (syncProgress?.inaccessible ?? 0)}
                        </Badge>
                      </div>
                    </div>
                    <div className="mt-3 h-2 overflow-hidden rounded-full bg-sky-100">
                      {syncProgressTotal > 0 ? (
                        <div
                          className="h-full rounded-full bg-sky-500 transition-[width] duration-300"
                          style={{ width: `${syncProgressPercent}%` }}
                        />
                      ) : (
                        <div className="h-full w-1/3 rounded-full bg-sky-500/80 animate-pulse" />
                      )}
                    </div>
                    <div className="mt-2 flex flex-wrap gap-2 text-[11px] leading-5 text-sky-700">
                      <span>导入 {syncProgress?.imported ?? 0}</span>
                      <span>导出 {syncProgress?.exported ?? 0}</span>
                      <span>不可直拉 {syncProgress?.inaccessible ?? 0}</span>
                      {syncProgress?.updated_at ? (
                        <span>
                          最近更新{" "}
                          {new Date(syncProgress.updated_at).toLocaleTimeString(
                            "zh-CN",
                          )}
                        </span>
                      ) : null}
                    </div>
                  </div>
                ) : null}

                <div className="grid grid-cols-3 gap-2 md:grid-cols-5 md:gap-3">
                  {(
                    [
                      ["本地", syncView.local],
                      ["远端", syncView.remote],
                      ["待推送", syncView.pendingPush],
                      ["待拉取", syncView.pendingPull],
                      ["无法直拉", syncView.inaccessibleRemote],
                    ] as const
                  ).map(([label, value]) => (
                    <div
                      key={label}
                      className="rounded-2xl border border-stone-100 bg-stone-50 px-3 py-3 md:px-4 md:py-4"
                    >
                      <div className="text-xs font-medium text-stone-400">
                        {label}
                      </div>
                      <div className="mt-1 text-xl font-semibold tracking-tight text-stone-900 md:mt-2 md:text-2xl">
                        {value}
                      </div>
                    </div>
                  ))}
                </div>

                <div className="flex flex-wrap gap-2">
                  {syncView.notes.map((note) => (
                    <Badge
                      key={note}
                      variant="warning"
                      className="rounded-lg px-3 py-1"
                    >
                      {note}
                    </Badge>
                  ))}
                  {syncView.lastRun &&
                  !syncView.lastRun.running &&
                  syncView.lastRun.finished_at ? (
                    <Badge
                      variant={syncView.lastRun.ok ? "success" : "danger"}
                      className="rounded-lg px-3 py-1"
                    >
                      最近一次操作：
                      {new Date(syncView.lastRun.finished_at).toLocaleString(
                        "zh-CN",
                      )}
                    </Badge>
                  ) : null}
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="mt-5 space-y-4">
        <div className="grid grid-cols-3 gap-2 md:grid-cols-4 md:gap-4 xl:grid-cols-8">
          {metricCards.map((item) => {
            const Icon = item.icon;
            const value = summary[item.key];
            return (
              <Card
                key={item.key}
                className="rounded-2xl border-white/80 bg-white/90 shadow-sm"
              >
                <CardContent className="p-2.5 md:p-4">
                  <div className="mb-2 flex items-start justify-between md:mb-4">
                    <span className="text-[11px] font-medium text-stone-400 md:text-xs">
                      {item.label}
                    </span>
                    <Icon className="size-4 text-stone-400" />
                  </div>
                  <div
                    className={cn(
                      "text-[1.1rem] font-semibold tracking-tight md:text-[1.75rem]",
                      item.color,
                    )}
                  >
                    <span
                      className={
                        typeof value === "number"
                          ? ""
                          : "text-[0.9rem] md:text-[1.1rem]"
                      }
                    >
                      {typeof value === "number" ? formatCompact(value) : value}
                    </span>
                  </div>
                </CardContent>
              </Card>
            );
          })}
        </div>
      </section>

      {isStudioMode ? (
        <section className="mt-5">
          <ImagePolicyCard accounts={accounts} />
        </section>
      ) : null}

      <section className="mt-5 space-y-5">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold tracking-tight">账户列表</h2>
            <Badge
              variant="secondary"
              className="rounded-lg bg-stone-200 px-2 py-0.5 text-stone-700"
            >
              {filteredAccounts.length}
            </Badge>
          </div>

          <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
            <div className="relative w-full min-w-0 lg:w-[260px]">
              <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-stone-400" />
              <Input
                value={query}
                onChange={(event) => {
                  setQuery(event.target.value);
                  setPage(1);
                }}
                placeholder="搜索邮箱 / 文件名 / 备注"
                className="h-10 rounded-xl border-stone-200 bg-white/85 pl-10"
              />
            </div>
            <Select
              value={typeFilter}
              onValueChange={(value) => {
                setTypeFilter(value as AccountType | "all");
                setPage(1);
              }}
            >
              <SelectTrigger className="h-10 w-full rounded-xl border-stone-200 bg-white/85 lg:w-[150px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {accountTypeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={statusFilter}
              onValueChange={(value) => {
                setStatusFilter(value as AccountStatus | "all");
                setPage(1);
              }}
            >
              <SelectTrigger className="h-10 w-full rounded-xl border-stone-200 bg-white/85 lg:w-[150px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {accountStatusOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={sortMode}
              onValueChange={(value) => {
                setSortMode(value as AccountSortMode);
                setPage(1);
              }}
            >
              <SelectTrigger className="h-10 w-full rounded-xl border-stone-200 bg-white/85 lg:w-[160px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {accountSortOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              className="h-10 w-full rounded-xl bg-stone-950 px-4 text-white hover:bg-stone-800 sm:w-auto"
              onClick={() => importInputRef.current?.click()}
              disabled={isImporting}
            >
              {isImporting ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <FileUp className="size-4" />
              )}
              导入认证文件
            </Button>
            {isStudioMode ? (
              <Button
                variant="outline"
                className="h-10 w-full rounded-xl border-stone-200 bg-white px-4 text-stone-700 sm:w-auto"
                onClick={() => setIsTokenDialogOpen(true)}
                disabled={isTokenImporting}
              >
                {isTokenImporting ? (
                  <LoaderCircle className="size-4 animate-spin" />
                ) : (
                  <FileUp className="size-4" />
                )}
                导入 Token
              </Button>
            ) : null}
          </div>
        </div>

        {isLoading && accounts.length === 0 ? (
          <Card className="rounded-2xl border-white/80 bg-white/90 shadow-sm">
            <CardContent className="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
              <div className="rounded-xl bg-stone-100 p-3 text-stone-500">
                <LoaderCircle className="size-5 animate-spin" />
              </div>
              <div className="space-y-1">
                <p className="text-sm font-medium text-stone-700">
                  正在加载账户
                </p>
                <p className="text-sm text-stone-500">
                  从后端读取本地 auth 文件和运行状态。
                </p>
              </div>
            </CardContent>
          </Card>
        ) : null}

        {refreshAllRun ? (
          <Card className="rounded-2xl border-white/80 bg-white/90 shadow-sm">
            <CardContent className="space-y-3 p-4">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <div className="space-y-1">
                  <div className="flex items-center gap-2 text-sm font-medium text-stone-800">
                    {refreshAllRunning ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : (
                      <RefreshCw className="size-4" />
                    )}
                    {refreshAllRunning
                      ? refreshAllRun.scope === "routing_groups"
                        ? "正在刷新轮询分组额度"
                        : "正在按批次刷新全部额度"
                      : "最近一次批量刷新"}
                  </div>
                  <div className="text-xs leading-5 text-stone-500">
                    {refreshAllRun.cancelRequested
                      ? "正在停止刷新任务"
                      : refreshAllRun.current
                        ? `当前处理：${refreshAllRun.current}`
                        : refreshAllRun.scope === "routing_groups"
                          ? "仅刷新当前轮询策略启用分组中的账号"
                          : "等待下一次批量刷新"}
                  </div>
                </div>
                <div className="flex flex-wrap items-center gap-2 text-xs text-stone-600">
                  <Badge variant="info" className="rounded-lg px-3 py-1">
                    已处理 {refreshAllRun.processed}/
                    {refreshAllRun.total || "?"}
                  </Badge>
                  <Badge variant="success" className="rounded-lg px-3 py-1">
                    成功 {refreshAllRun.refreshed}
                  </Badge>
                  <Badge variant="danger" className="rounded-lg px-3 py-1">
                    失败 {refreshAllRun.failed}
                  </Badge>
                  {refreshAllRunning ? (
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8 rounded-lg border-rose-200 bg-white px-3 text-rose-600 hover:bg-rose-50"
                      onClick={() => void handleStopAccountRefresh()}
                      disabled={isStoppingRefreshAll || refreshAllRun.cancelRequested}
                    >
                      {isStoppingRefreshAll || refreshAllRun.cancelRequested ? (
                        <LoaderCircle className="size-3.5 animate-spin" />
                      ) : (
                        <Square className="size-3.5" />
                      )}
                      停止刷新
                    </Button>
                  ) : null}
                </div>
              </div>
              <div className="h-2 overflow-hidden rounded-full bg-stone-100">
                {refreshAllRun.total > 0 ? (
                  <div
                    className={cn(
                      "h-full rounded-full transition-[width] duration-300",
                      refreshAllRunning
                        ? "bg-stone-900"
                        : refreshAllRun.failed > 0
                          ? "bg-amber-500"
                          : "bg-emerald-500",
                    )}
                    style={{ width: `${refreshAllPercent}%` }}
                  />
                ) : (
                  <div className="h-full w-1/3 rounded-full bg-stone-300 animate-pulse" />
                )}
              </div>
              <div className="flex flex-wrap gap-2 text-[11px] leading-5 text-stone-500">
                {refreshAllRun.updated_at ? (
                  <span>
                    最近更新{" "}
                    {new Date(refreshAllRun.updated_at).toLocaleTimeString(
                      "zh-CN",
                    )}
                  </span>
                ) : null}
                {refreshAllRun.finished_at ? (
                  <span>
                    结束时间{" "}
                    {new Date(refreshAllRun.finished_at).toLocaleTimeString(
                      "zh-CN",
                    )}
                  </span>
                ) : null}
                {refreshAllRun.error ? (
                  <span className="text-rose-500">{refreshAllRun.error}</span>
                ) : null}
              </div>
            </CardContent>
          </Card>
        ) : null}

        <Card
          className={cn(
            "overflow-hidden rounded-2xl border-white/80 bg-white/90 shadow-sm",
            isLoading && accounts.length === 0 ? "hidden" : "",
          )}
        >
          <CardContent className="space-y-0 p-0">
            <div className="flex flex-col gap-3 border-b border-stone-100 px-4 py-3 lg:flex-row lg:items-center lg:justify-between">
              <div className="flex flex-wrap items-center gap-2 text-sm text-stone-500">
                <Button
                  variant="ghost"
                  className="h-8 rounded-lg px-3 text-stone-500 hover:bg-stone-100"
                  onClick={() =>
                    void handleRefreshSelectedAccounts(selectedTokens)
                  }
                  disabled={
                    selectedTokens.length === 0 ||
                    isRefreshing ||
                    refreshAllRunning
                  }
                >
                  {isRefreshing ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <RefreshCw className="size-4" />
                  )}
                  刷新选中账号信息
                </Button>
                <Button
                  variant="ghost"
                  className="h-8 rounded-lg px-3 text-stone-500 hover:bg-stone-100"
                  onClick={() => void handleRefreshAllAccounts()}
                  disabled={
                    accounts.length === 0 ||
                    refreshAllRunning ||
                    isRefreshing ||
                    quotaRefreshingId !== null
                  }
                >
                  {isRefreshingAllAccounts ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <RefreshCw className="size-4" />
                  )}
                  一键刷新所有额度
                </Button>
                <Button
                  variant="ghost"
                  className="h-8 rounded-lg px-3 text-stone-500 hover:bg-stone-100"
                  onClick={() => void handleRefreshAllAccounts("routing_groups")}
                  disabled={
                    accounts.length === 0 ||
                    refreshAllRunning ||
                    isRefreshing ||
                    quotaRefreshingId !== null
                  }
                >
                  {isRefreshingRoutingGroups ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <RefreshCw className="size-4" />
                  )}
                  刷新轮询分组额度
                </Button>
                <Button
                  variant="ghost"
                  className="h-8 rounded-lg px-3 text-rose-500 hover:bg-rose-50 hover:text-rose-600"
                  onClick={() => void handleDeleteTokens(abnormalTokens)}
                  disabled={abnormalTokens.length === 0 || isDeleting}
                >
                  {isDeleting ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <Trash2 className="size-4" />
                  )}
                  移除异常账号
                </Button>
                <Button
                  variant="ghost"
                  className="h-8 rounded-lg px-3 text-rose-500 hover:bg-rose-50 hover:text-rose-600"
                  onClick={() => void handleDeleteTokens(selectedTokens)}
                  disabled={selectedTokens.length === 0 || isDeleting}
                >
                  {isDeleting ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <Trash2 className="size-4" />
                  )}
                  删除所选
                </Button>
                {selectedIds.length > 0 ? (
                  <span className="rounded-lg bg-stone-100 px-2.5 py-1 text-xs font-medium text-stone-600">
                    已选择 {selectedIds.length} 项
                  </span>
                ) : null}
              </div>
            </div>

            <div className="lg:hidden">
              {currentRows.length > 0 ? (
                <div className="space-y-3 px-4 py-4">
                  {currentRows.map((account) => {
                    const status = statusMeta[account.status];
                    const StatusIcon = status.icon;
                    const syncState =
                      syncSource === "cpa" ? account.syncStatus : null;
                    const liveQuota = accountQuotaMap[account.id];
                    const codex5hUsedPercent =
                      liveQuota?.codex_5h_used_percent ?? account.codex_5h_used_percent;
                    const codex7dUsedPercent =
                      liveQuota?.codex_7d_used_percent ?? account.codex_7d_used_percent;
                    const isQuotaRefreshing = quotaRefreshingId === account.id;

                    return (
                      <div
                        key={account.id}
                        className="rounded-2xl border border-stone-200/80 bg-stone-50/60 p-3.5"
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex min-w-0 flex-1 items-start gap-3">
                            <Checkbox
                              checked={selectedIds.includes(account.id)}
                              onCheckedChange={(checked) => {
                                setSelectedIds((prev) =>
                                  checked
                                    ? Array.from(new Set([...prev, account.id]))
                                    : prev.filter(
                                        (item) => item !== account.id,
                                      ),
                                );
                              }}
                              className="mt-1 shrink-0"
                            />
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="truncate font-medium tracking-tight text-stone-700">
                                  {maskToken(account.access_token)}
                                </span>
                                <button
                                  type="button"
                                  className="shrink-0 rounded-lg p-1 text-stone-400 transition hover:bg-stone-100 hover:text-stone-700"
                                  onClick={() => {
                                    void navigator.clipboard.writeText(
                                      account.access_token,
                                    );
                                    toast.success("token 已复制");
                                  }}
                                >
                                  <Copy className="size-4" />
                                </button>
                              </div>
                              <div className="mt-1 space-y-0.5 text-[11px] leading-5">
                                <div
                                  className="truncate text-stone-500"
                                  title={account.email ?? ""}
                                >
                                  {account.email ?? "—"}
                                </div>
                                <div
                                  className="truncate text-stone-400"
                                  title={
                                    account.note
                                      ? `${account.fileName} · ${account.note}`
                                      : account.fileName
                                  }
                                >
                                  {account.note
                                    ? `${account.fileName} · ${account.note}`
                                    : account.fileName}
                                </div>
                              </div>
                            </div>
                          </div>
                          <button
                            type="button"
                            className="shrink-0 rounded-lg p-2 text-stone-400 transition hover:bg-stone-100 hover:text-stone-700"
                            onClick={() =>
                              void handleRefreshAccountQuota(account)
                            }
                            disabled={isQuotaRefreshing || refreshAllRunning}
                            aria-label="刷新对话额度"
                          >
                            <RefreshCw
                              className={cn(
                                "size-4",
                                isQuotaRefreshing ? "animate-spin" : "",
                              )}
                            />
                          </button>
                          <button
                            type="button"
                            className="shrink-0 rounded-lg p-2 text-stone-400 transition hover:bg-emerald-100 hover:text-emerald-700"
                            onClick={() =>
                              void handleImageTest(account, defaultImageTestRoute(account))
                            }
                            disabled={imageTestingId === account.id}
                            aria-label="检测生图"
                            title="检测生图"
                          >
                            <Play
                              className={cn(
                                "size-4",
                                imageTestingId === account.id ? "animate-pulse" : "",
                              )}
                            />
                          </button>
                        </div>

                        <div className="mt-2.5 flex flex-wrap gap-1.5">
                          <Badge
                            variant="secondary"
                            className="rounded-md bg-stone-100 px-2 py-0.5 text-[11px] text-stone-700"
                          >
                            {account.type}
                          </Badge>
                          <Badge
                            variant={
                              sourceKindMeta[account.sourceKind ?? "auth_file"]
                                .badge
                            }
                            className="rounded-md px-2 py-0.5 text-[11px]"
                          >
                            {
                              sourceKindMeta[account.sourceKind ?? "auth_file"]
                                .label
                            }
                          </Badge>
                          <Badge
                            variant={status.badge}
                            className="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-[11px]"
                          >
                            <StatusIcon className="size-3.5" />
                            {account.status}
                          </Badge>
                          {syncState ? (
                            <Badge
                              variant={syncMeta[syncState].badge}
                              className="rounded-md px-2 py-0.5 text-[11px]"
                            >
                              {syncMeta[syncState].label}
                            </Badge>
                          ) : null}
                        </div>

                        <div className="mt-4 grid grid-cols-2 gap-2 text-xs text-stone-500">
                          <div className="rounded-xl bg-white px-3 py-2.5">
                            <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">
                              对话额度
                            </div>
                            <div className="mt-1 text-sm font-semibold text-stone-700">
                              5h {formatCodexPercent(codex5hUsedPercent)}
                            </div>
                            <div className="mt-1 text-[11px] text-stone-400">
                              7d {formatCodexPercent(codex7dUsedPercent)}
                            </div>
                          </div>
                          <div className="rounded-xl bg-white px-3 py-2.5">
                            <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">
                              成功
                            </div>
                            <div className="mt-1 text-sm font-semibold text-stone-700">
                              {account.success}
                            </div>
                          </div>
                          <div className="rounded-xl bg-white px-3 py-2.5">
                            <div className="text-[11px] uppercase tracking-[0.14em] text-stone-400">
                              失败
                            </div>
                            <div className="mt-1 text-sm font-semibold text-stone-700">
                              {account.fail}
                            </div>
                          </div>
                        </div>

                        {imageTestResult[account.id] ? (
                          <div className="mt-3 rounded-xl border border-stone-200 bg-white p-3 text-xs">
                            <div className="flex items-center gap-2">
                              <span className={imageTestResult[account.id].ok ? "font-semibold text-emerald-600" : "font-semibold text-rose-600"}>
                                {imageTestResult[account.id].ok ? "检测成功" : "检测失败"}
                              </span>
                              <span className="text-stone-400">{imageTestResult[account.id].latencyMs}ms</span>
                            </div>
                            {imageTestResult[account.id].error ? (
                              <div className="mt-2 break-all text-rose-600">{imageTestResult[account.id].error}</div>
                            ) : null}
                            {imageTestResult[account.id].requestBody ? (
                              <details className="mt-2">
                                <summary className="cursor-pointer text-stone-500 hover:text-stone-700">请求体</summary>
                                <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded bg-stone-50 p-2 text-[11px] text-stone-600">{imageTestResult[account.id].requestBody}</pre>
                              </details>
                            ) : null}
                            {imageTestResult[account.id].responseBody ? (
                              <details className="mt-2">
                                <summary className="cursor-pointer text-stone-500 hover:text-stone-700">响应体 (HTTP {imageTestResult[account.id].responseStatus || "?"})</summary>
                                <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded bg-stone-50 p-2 text-[11px] text-stone-600">{imageTestResult[account.id].responseBody}</pre>
                              </details>
                            ) : null}
                          </div>
                        ) : null}

                        <div className="mt-4 flex items-center justify-end gap-1 border-t border-stone-200/70 pt-3 text-stone-400">
                          <button
                            type="button"
                            className="rounded-lg p-2 transition hover:bg-stone-100 hover:text-stone-700"
                            onClick={() => openEditDialog(account)}
                            disabled={isUpdating}
                          >
                            <Pencil className="size-4" />
                          </button>
                          <button
                            type="button"
                            className="rounded-lg p-2 transition hover:bg-rose-50 hover:text-rose-500"
                            onClick={() =>
                              void handleDeleteTokens([account.access_token])
                            }
                            disabled={isDeleting}
                          >
                            <Trash2 className="size-4" />
                          </button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : !isLoading ? (
                <div className="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
                  <div className="rounded-xl bg-stone-100 p-3 text-stone-500">
                    <Search className="size-5" />
                  </div>
                  <div className="space-y-1">
                    <p className="text-sm font-medium text-stone-700">
                      没有匹配的账户
                    </p>
                    <p className="text-sm text-stone-500">
                      调整筛选条件或搜索关键字后重试。
                    </p>
                  </div>
                </div>
              ) : null}
            </div>

            <div className="hidden overflow-x-auto lg:block">
              <table className="w-full min-w-[1120px] text-left">
                <thead className="border-b border-stone-100 text-[11px] text-stone-400 uppercase tracking-[0.18em]">
                  <tr>
                    <th className="w-12 px-4 py-3 text-center">
                      <Checkbox
                        checked={allCurrentSelected}
                        onCheckedChange={(checked) =>
                          toggleSelectAll(Boolean(checked))
                        }
                      />
                    </th>
                    <th className="w-80 px-4 py-3 whitespace-nowrap">
                      账号 / Token
                    </th>
                    <th className="w-28 px-4 py-3 text-center whitespace-nowrap">
                      类型 / 来源
                    </th>
                    <th className="w-24 px-4 py-3 text-center whitespace-nowrap">
                      状态
                    </th>
                    <th className="w-28 px-4 py-3 text-center whitespace-nowrap">
                      同步
                    </th>
                    <th className="w-32 px-4 py-3 text-center whitespace-nowrap">
                      对话额度
                    </th>
                    <th className="w-18 px-4 py-3 text-center whitespace-nowrap">
                      成功
                    </th>
                    <th className="w-18 px-4 py-3 text-center whitespace-nowrap">
                      失败
                    </th>
                    <th className="w-24 px-4 py-3 text-center whitespace-nowrap">
                      操作
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {currentRows.map((account) => {
                    const status = statusMeta[account.status];
                    const StatusIcon = status.icon;
                    const syncState =
                      syncSource === "cpa" ? account.syncStatus : null;
                    const liveQuota = accountQuotaMap[account.id];
                    const codex5hUsedPercent =
                      liveQuota?.codex_5h_used_percent ?? account.codex_5h_used_percent;
                    const codex7dUsedPercent =
                      liveQuota?.codex_7d_used_percent ?? account.codex_7d_used_percent;
                    const isQuotaRefreshing = quotaRefreshingId === account.id;

                    return (
                      <Fragment key={account.id}>
                      <tr
                        className="border-b border-stone-100/80 text-sm text-stone-600 transition-colors hover:bg-stone-50/70"
                      >
                        <td className="px-4 py-3 text-center">
                          <Checkbox
                            checked={selectedIds.includes(account.id)}
                            onCheckedChange={(checked) => {
                              setSelectedIds((prev) =>
                                checked
                                  ? Array.from(new Set([...prev, account.id]))
                                  : prev.filter((item) => item !== account.id),
                              );
                            }}
                          />
                        </td>
                        <td className="px-4 py-3">
                          <div className="flex items-center gap-2 whitespace-nowrap">
                            <span className="font-medium tracking-tight text-stone-700">
                              {maskToken(account.access_token)}
                            </span>
                            <button
                              type="button"
                              className="rounded-lg p-1 text-stone-400 transition hover:bg-stone-100 hover:text-stone-700"
                              onClick={() => {
                                void navigator.clipboard.writeText(
                                  account.access_token,
                                );
                                toast.success("token 已复制");
                              }}
                            >
                              <Copy className="size-4" />
                            </button>
                          </div>
                          <div className="mt-1 space-y-0.5 text-xs">
                            <div
                              className="truncate text-stone-500"
                              title={account.email ?? ""}
                            >
                              {account.email ?? "—"}
                            </div>
                            <div
                              className="truncate text-stone-400"
                              title={account.fileName}
                            >
                              {account.fileName}
                            </div>
                            {account.note ? (
                              <div
                                className="truncate text-stone-400"
                                title={account.note}
                              >
                                {account.note}
                              </div>
                            ) : null}
                          </div>
                        </td>
                        <td className="px-4 py-3 text-center whitespace-nowrap">
                          <div className="flex flex-col items-center gap-1">
                            <Badge
                              variant="secondary"
                              className="rounded-md bg-stone-100 text-stone-700"
                            >
                              {account.type}
                            </Badge>
                            <Badge
                              variant={
                                sourceKindMeta[
                                  account.sourceKind ?? "auth_file"
                                ].badge
                              }
                              className="rounded-md px-2 py-0.5 text-[11px]"
                            >
                              {
                                sourceKindMeta[
                                  account.sourceKind ?? "auth_file"
                                ].label
                              }
                            </Badge>
                          </div>
                        </td>
                        <td className="px-4 py-3 text-center whitespace-nowrap">
                          <Badge
                            variant={status.badge}
                            className="inline-flex items-center gap-1 rounded-md px-2 py-1"
                          >
                            <StatusIcon className="size-3.5" />
                            {account.status}
                          </Badge>
                        </td>
                        <td className="px-4 py-3 text-center whitespace-nowrap">
                          {syncState ? (
                            <Badge
                              variant={syncMeta[syncState].badge}
                              className="rounded-md px-2 py-1"
                            >
                              {syncMeta[syncState].label}
                            </Badge>
                          ) : (
                            <span className="text-xs text-stone-400">—</span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-center">
                          <div className="flex flex-col items-center gap-2">
                            <div className="flex items-center gap-2">
                              <Badge variant="info" className="rounded-md">
                                5h {formatCodexPercent(codex5hUsedPercent)}
                              </Badge>
                              <button
                                type="button"
                                className="rounded-lg p-1 text-stone-400 transition hover:bg-stone-100 hover:text-stone-700"
                                onClick={() =>
                                  void handleRefreshAccountQuota(account)
                                }
                                disabled={
                                  isQuotaRefreshing || refreshAllRunning
                                }
                              >
                                <RefreshCw
                                  className={cn(
                                    "size-3.5",
                                    isQuotaRefreshing ? "animate-spin" : "",
                                  )}
                                />
                              </button>
                            </div>
                            <div className="text-[11px] text-stone-400">
                              7d {formatCodexPercent(codex7dUsedPercent)}
                            </div>
                          </div>
                        </td>
                        <td className="px-4 py-3 text-center text-stone-500 whitespace-nowrap">
                          {account.success}
                        </td>
                        <td className="px-4 py-3 text-center text-stone-500 whitespace-nowrap">
                          {account.fail}
                        </td>
                        <td className="px-4 py-3 text-center">
                          <div className="flex items-center justify-center gap-1 text-stone-400">
                            <button
                              type="button"
                              className="rounded-lg p-2 transition hover:bg-stone-100 hover:text-stone-700"
                              onClick={() => openEditDialog(account)}
                              disabled={isUpdating}
                            >
                              <Pencil className="size-4" />
                            </button>
                            <button
                              type="button"
                              className="rounded-lg p-2 transition hover:bg-rose-50 hover:text-rose-500"
                              onClick={() =>
                                void handleDeleteTokens([account.access_token])
                              }
                              disabled={isDeleting}
                            >
                              <Trash2 className="size-4" />
                            </button>
                            {accountSupportsRoute(account, "responses") ? (
                              <button
                                type="button"
                                className={cn(
                                  "rounded-lg px-1.5 py-1 text-[10px] font-medium transition",
                                  imageTestingId === account.id
                                    ? "bg-emerald-100 text-emerald-700"
                                    : "text-stone-400 hover:bg-emerald-100 hover:text-emerald-700",
                                )}
                                onClick={() => void handleImageTest(account, "responses")}
                                disabled={imageTestingId !== null}
                                title="检测 responses 路线"
                              >
                                {imageTestingId === account.id ? <LoaderCircle className="inline size-3 animate-spin" /> : "R"}
                              </button>
                            ) : null}
                            <button
                              type="button"
                              className={cn(
                                "rounded-lg px-1.5 py-1 text-[10px] font-medium transition",
                                imageTestingId === account.id
                                  ? "bg-amber-100 text-amber-700"
                                  : "text-stone-400 hover:bg-amber-100 hover:text-amber-700",
                              )}
                              onClick={() => void handleImageTest(account, "legacy")}
                              disabled={imageTestingId !== null}
                              title="检测 legacy 路线"
                            >
                              L
                            </button>
                          </div>
                          {imageTestResult[account.id] ? (
                            <div className="mt-1 max-w-xs text-left text-[11px]">
                              <span className={imageTestResult[account.id].ok ? "font-semibold text-emerald-600" : "font-semibold text-rose-600"}>
                                {imageTestResult[account.id].ok ? "成功" : "失败"}
                              </span>
                              <span className="ml-1 text-stone-400">{imageTestResult[account.id].latencyMs}ms</span>
                            </div>
                          ) : null}
                        </td>
                      </tr>
                      {imageTestResult[account.id] ? (
                        <tr className="border-b border-stone-100 bg-stone-50">
                          <td colSpan={9} className="px-4 py-3">
                            <div className="text-xs">
                              <div className="flex items-center gap-3">
                                <span className={imageTestResult[account.id].ok ? "font-semibold text-emerald-600" : "font-semibold text-rose-600"}>
                                  {imageTestResult[account.id].ok ? "检测成功" : "检测失败"}
                                </span>
                                <span className="text-stone-400">耗时 {imageTestResult[account.id].latencyMs}ms</span>
                                <span className="text-stone-400">HTTP {imageTestResult[account.id].responseStatus || "—"}</span>
                                <button type="button" className="text-stone-400 hover:text-stone-700" onClick={() => setImageTestResult((prev) => { const next = { ...prev }; delete next[account.id]; return next; })}>关闭</button>
                              </div>
                              {imageTestResult[account.id].error ? (
                                <div className="mt-2 break-all text-rose-600">{imageTestResult[account.id].error}</div>
                              ) : null}
                              <details className="mt-2">
                                <summary className="cursor-pointer text-stone-500 hover:text-stone-700">请求体</summary>
                                <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-white p-2 text-[11px] text-stone-600 border border-stone-200">{imageTestResult[account.id].requestBody || "—"}</pre>
                              </details>
                              <details className="mt-2">
                                <summary className="cursor-pointer text-stone-500 hover:text-stone-700">响应体</summary>
                                <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-white p-2 text-[11px] text-stone-600 border border-stone-200">{imageTestResult[account.id].responseBody || "—"}</pre>
                              </details>
                            </div>
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  );
                })}
                </tbody>
              </table>

              {!isLoading && currentRows.length === 0 ? (
                <div className="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
                  <div className="rounded-xl bg-stone-100 p-3 text-stone-500">
                    <Search className="size-5" />
                  </div>
                  <div className="space-y-1">
                    <p className="text-sm font-medium text-stone-700">
                      没有匹配的账户
                    </p>
                    <p className="text-sm text-stone-500">
                      调整筛选条件或搜索关键字后重试。
                    </p>
                  </div>
                </div>
              ) : null}
            </div>

            <div className="border-t border-stone-100 px-4 py-4">
              <div className="flex items-center justify-center gap-3 overflow-x-auto whitespace-nowrap">
                <div className="shrink-0 text-sm text-stone-500">
                  显示第 {filteredAccounts.length === 0 ? 0 : startIndex + 1} -{" "}
                  {Math.min(
                    startIndex + Number(pageSize),
                    filteredAccounts.length,
                  )}{" "}
                  条，共 {filteredAccounts.length} 条
                </div>

                <span className="shrink-0 text-sm leading-none text-stone-500">
                  {safePage} / {pageCount} 页
                </span>
                <Select
                  value={pageSize}
                  onValueChange={(value) => {
                    setPageSize(value);
                    setPage(1);
                  }}
                >
                  <SelectTrigger className="h-10 w-[108px] shrink-0 rounded-lg border-stone-200 bg-white text-sm leading-none">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="10">10 / 页</SelectItem>
                    <SelectItem value="20">20 / 页</SelectItem>
                    <SelectItem value="50">50 / 页</SelectItem>
                    <SelectItem value="100">100 / 页</SelectItem>
                  </SelectContent>
                </Select>
                <Button
                  variant="outline"
                  size="icon"
                  className="size-10 shrink-0 rounded-lg border-stone-200 bg-white"
                  disabled={safePage <= 1}
                  onClick={() => setPage((prev) => Math.max(1, prev - 1))}
                >
                  <ChevronLeft className="size-4" />
                </Button>
                {paginationItems.map((item, index) =>
                  item === "..." ? (
                    <span
                      key={`ellipsis-${index}`}
                      className="px-1 text-sm text-stone-400"
                    >
                      ...
                    </span>
                  ) : (
                    <Button
                      key={item}
                      variant={item === safePage ? "default" : "outline"}
                      className={cn(
                        "h-10 min-w-10 shrink-0 rounded-lg px-3",
                        item === safePage
                          ? "bg-stone-950 text-white hover:bg-stone-800"
                          : "border-stone-200 bg-white text-stone-700",
                      )}
                      onClick={() => setPage(item)}
                    >
                      {item}
                    </Button>
                  ),
                )}
                <Button
                  variant="outline"
                  size="icon"
                  className="size-10 shrink-0 rounded-lg border-stone-200 bg-white"
                  disabled={safePage >= pageCount}
                  onClick={() =>
                    setPage((prev) => Math.min(pageCount, prev + 1))
                  }
                >
                  <ChevronRight className="size-4" />
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
