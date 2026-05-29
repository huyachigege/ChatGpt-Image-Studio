"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Copy,
  KeyRound,
  LoaderCircle,
  Megaphone,
  Plus,
  RefreshCcw,
  Trash2,
  UserRound,
  RefreshCw,
  Save,
  Search,
  Settings2,
  Stethoscope,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  adjustAllUsersQuota,
  adjustUserQuota,
  createInvite,
  deleteAnnouncement,
  deleteUser,
  fetchAnnouncement,
  fetchConfig,
  fetchDefaultConfig,
  fetchImageSystemHint,
  fetchInvites,
  fetchUsers,
  setAnnouncement,
  updateConfig,
  updateImageSystemHint,
  updateUserDisabled,
  type AppUserItem,
  type ConfigPayload,
  type InviteItem,
} from "@/lib/api";
import {
  exportLocalImageConversationsSnapshot,
  exportServerImageConversationsSnapshot,
  importImageConversationsToServerTarget,
  migrateImageConversationStorage,
  setCachedImageConversationStorageMode,
} from "@/store/image-conversations";
import { clearCachedSyncStatus } from "@/store/sync-status-cache";
import { ImageModeSection } from "./components/image-mode-section";
import { IntegrationSection } from "./components/integration-section";
import { RuntimeSection } from "./components/runtime-section";
import { ServicePathsSection } from "./components/service-paths-section";

const MANAGEMENT_PAGE_SIZE = 10;

const userSortOptions = [
  { label: "默认排序", value: "default" },
  { label: "最近使用从近到远", value: "last_used_desc" },
  { label: "最近使用从远到近", value: "last_used_asc" },
  { label: "可用额度从高到低", value: "quota_desc" },
  { label: "可用额度从低到高", value: "quota_asc" },
] as const;

type UserSortMode = (typeof userSortOptions)[number]["value"];

function joinDisplayPath(root: string, relativePath: string) {
  const normalizedRoot = String(root || "")
    .trim()
    .replace(/[\\/]+$/, "");
  const normalizedRelative = String(relativePath || "")
    .trim()
    .replace(/^[\\/]+/, "");
  if (!normalizedRoot) {
    return normalizedRelative;
  }
  if (!normalizedRelative) {
    return normalizedRoot;
  }
  const separator = normalizedRoot.includes("\\") ? "\\" : "/";
  return `${normalizedRoot}${separator}${normalizedRelative.replace(/[\\/]+/g, separator)}`;
}

function firstNonEmptyValue(...values: Array<string | null | undefined>) {
  for (const value of values) {
    const trimmed = String(value || "").trim();
    if (trimmed) {
      return trimmed;
    }
  }
  return "";
}

function formatManagementTime(value?: string) {
  if (!value?.trim()) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function getUserLastUsedTime(item: AppUserItem) {
  if (!item.lastUsedAt?.trim()) return 0;
  const value = new Date(item.lastUsedAt).getTime();
  return Number.isNaN(value) ? 0 : value;
}

function getUserAvailableQuota(item: AppUserItem) {
  return (item.quota?.freeRemaining ?? 0) + (item.quota?.paidRemaining ?? 0);
}

function defaultConfigPayload(): ConfigPayload {
  return {
    app: {
      name: "",
      version: "",
      apiKey: "",
      authKey: "",
      imageFormat: "url",
      maxUploadSizeMB: 50,
    },
    server: {
      host: "",
      port: 7000,
      staticDir: "",
      maxImageConcurrency: 8,
      imageQueueLimit: 32,
      imageQueueTimeoutSeconds: 20,
      imageTaskQueueTtlSeconds: 600,
      imageDownloadRateKbps: 0,
    },
    chatgpt: {
      model: "gpt-image-2",
      sseTimeout: 600,
      pollInterval: 3,
      pollMaxWait: 600,
      requestTimeout: 120,
      imageMode: "studio",
      freeImageRoute: "responses",
      freeImageModel: "auto",
      paidImageRoute: "responses",
      paidImageModel: "gpt-5.4-mini",
      studioAllowDisabledImageAccounts: false,
      imageAccountRetryTimes: 3,
      imageCommonSystemHint: "",
      imagePrivateSystemHint: "",
      imageSystemHint: "",
    },
    accounts: {
      defaultQuota: 5,
      preferRemoteRefresh: true,
      refreshWorkers: 6,
      imageQuotaRefreshTTLSeconds: 120,
      dailyFreeImageLimit: 120,
      dailyPaidImageLimit: 30,
    },
    storage: {
      backend: "current",
      configBackend: "file",
      authDir: "",
      stateFile: "",
      syncStateDir: "",
      imageDir: "",
      imageStorage: "browser",
      imageConversationStorage: "browser",
      imageDataStorage: "browser",
      sqlitePath: "",
      redisAddr: "127.0.0.1:6379",
      redisPassword: "",
      redisDb: 0,
      redisPrefix: "chatgpt2api:studio",
    },
    sync: {
      enabled: false,
      baseUrl: "",
      managementKey: "",
      requestTimeout: 20,
      concurrency: 4,
      providerType: "codex",
    },
    proxy: {
      enabled: false,
      url: "socks5h://127.0.0.1:10808",
      mode: "fixed",
      syncEnabled: false,
    },
    cpa: {
      baseUrl: "",
      apiKey: "",
      requestTimeout: 60,
      routeStrategy: "images_api",
    },
    externalResponses: {
      enabled: false,
      baseUrl: "",
      apiKey: "",
      model: "gpt-5",
      requestTimeout: 300,
      retryTimes: 3,
      providers: [],
    },
    newapi: {
      baseUrl: "",
      username: "",
      password: "",
      accessToken: "",
      userId: 0,
      sessionCookie: "",
      requestTimeout: 20,
    },
    sub2api: {
      baseUrl: "",
      email: "",
      password: "",
      apiKey: "",
      groupId: "",
      requestTimeout: 20,
    },
    log: {
      logAllRequests: false,
    },
    paths: {
      root: "",
      defaults: "",
      override: "",
    },
  };
}

function normalizeConfigPayload(
  payload: Partial<ConfigPayload> | null | undefined,
): ConfigPayload {
  const defaults = defaultConfigPayload();
  const next = payload ?? {};
  const chatgpt = {
    ...defaults.chatgpt,
    ...next.chatgpt,
  };
  chatgpt.sseTimeout = chatgpt.sseTimeout || defaults.chatgpt.sseTimeout;
  chatgpt.pollInterval = chatgpt.pollInterval || defaults.chatgpt.pollInterval;
  chatgpt.pollMaxWait = chatgpt.pollMaxWait || defaults.chatgpt.pollMaxWait;
  chatgpt.requestTimeout =
    chatgpt.requestTimeout || defaults.chatgpt.requestTimeout;
  chatgpt.imageAccountRetryTimes =
    chatgpt.imageAccountRetryTimes || defaults.chatgpt.imageAccountRetryTimes;

  const server = { ...defaults.server, ...next.server };
  server.port = server.port || defaults.server.port;
  server.maxImageConcurrency =
    server.maxImageConcurrency || defaults.server.maxImageConcurrency;
  server.imageQueueLimit =
    server.imageQueueLimit || defaults.server.imageQueueLimit;
  server.imageQueueTimeoutSeconds =
    server.imageQueueTimeoutSeconds || defaults.server.imageQueueTimeoutSeconds;
  server.imageTaskQueueTtlSeconds =
    server.imageTaskQueueTtlSeconds || defaults.server.imageTaskQueueTtlSeconds;
  server.imageDownloadRateKbps = Math.max(
    0,
    Number(server.imageDownloadRateKbps || 0),
  );

  const accounts = { ...defaults.accounts, ...next.accounts };
  accounts.defaultQuota =
    accounts.defaultQuota || defaults.accounts.defaultQuota;
  accounts.refreshWorkers =
    accounts.refreshWorkers || defaults.accounts.refreshWorkers;
  accounts.imageQuotaRefreshTTLSeconds =
    accounts.imageQuotaRefreshTTLSeconds ||
    defaults.accounts.imageQuotaRefreshTTLSeconds;
  accounts.dailyFreeImageLimit =
    accounts.dailyFreeImageLimit || defaults.accounts.dailyFreeImageLimit;
  accounts.dailyPaidImageLimit =
    accounts.dailyPaidImageLimit || defaults.accounts.dailyPaidImageLimit;

  const storage = {
    ...defaults.storage,
    ...next.storage,
  };
  if (chatgpt.imageMode !== "studio" && chatgpt.imageMode !== "cpa") {
    chatgpt.imageMode = "studio";
  }
  const legacyImageStorage =
    storage.imageStorage === "server" ? "server" : "browser";
  storage.imageConversationStorage =
    storage.imageConversationStorage === "server"
      ? "server"
      : legacyImageStorage;
  storage.imageDataStorage =
    storage.imageDataStorage === "server"
      ? "server"
      : storage.imageConversationStorage;
  storage.imageStorage = storage.imageConversationStorage;

  return {
    ...defaults,
    ...next,
    app: { ...defaults.app, ...next.app },
    server,
    chatgpt,
    accounts,
    storage,
    sync: { ...defaults.sync, ...next.sync },
    proxy: { ...defaults.proxy, ...next.proxy },
    cpa: { ...defaults.cpa, ...next.cpa },
    externalResponses: {
      ...defaults.externalResponses,
      ...next.externalResponses,
    },
    newapi: { ...defaults.newapi, ...next.newapi },
    sub2api: { ...defaults.sub2api, ...next.sub2api },
    log: { ...defaults.log, ...next.log },
    paths: { ...defaults.paths, ...next.paths },
  };
}

export default function SettingsPage() {
  const [config, setConfig] = useState<ConfigPayload>(defaultConfigPayload);
  const [defaultConfig, setDefaultConfig] =
    useState<ConfigPayload>(defaultConfigPayload);
  const [savedConfig, setSavedConfig] = useState<ConfigPayload | null>(null);
  const [invites, setInvites] = useState<InviteItem[]>([]);
  const [users, setUsers] = useState<AppUserItem[]>([]);
  const [invitePage, setInvitePage] = useState(1);
  const [userPage, setUserPage] = useState(1);
  const [inviteQuery, setInviteQuery] = useState("");
  const [userQuery, setUserQuery] = useState("");
  const [userSortMode, setUserSortMode] = useState<UserSortMode>("default");
  const [latestInviteCode, setLatestInviteCode] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isLoadingInvites, setIsLoadingInvites] = useState(false);
  const [isCreatingInvite, setIsCreatingInvite] = useState(false);
  const [isLoadingUsers, setIsLoadingUsers] = useState(false);
  const [mutatingUserID, setMutatingUserID] = useState("");
  const [bulkQuotaKind, setBulkQuotaKind] = useState<"free" | "paid">("free");
  const [bulkQuotaDelta, setBulkQuotaDelta] = useState("10");
  const [isAdjustingAllUsersQuota, setIsAdjustingAllUsersQuota] =
    useState(false);

  const isStudioMode = config.chatgpt.imageMode === "studio";
  const isCPAMode = config.chatgpt.imageMode === "cpa";

  const isDirty = useMemo(() => {
    if (!savedConfig) {
      return false;
    }
    return JSON.stringify(config) !== JSON.stringify(savedConfig);
  }, [config, savedConfig]);

  const resolvedStaticDir = useMemo(() => {
    const staticDir = String(config.server.staticDir || "").trim();
    if (!staticDir) {
      return "";
    }
    if (
      /^[A-Za-z]:[\\/]/.test(staticDir) ||
      staticDir.startsWith("/") ||
      staticDir.startsWith("\\\\")
    ) {
      return staticDir;
    }
    return joinDisplayPath(config.paths.root, staticDir);
  }, [config.paths.root, config.server.staticDir]);

  const startupErrorPath = useMemo(
    () => joinDisplayPath(config.paths.root, "data/last-startup-error.txt"),
    [config.paths.root],
  );
  const effectiveCPAImageBaseUrl = useMemo(
    () => firstNonEmptyValue(config.cpa.baseUrl, config.sync.baseUrl),
    [config.cpa.baseUrl, config.sync.baseUrl],
  );
  const syncManagementKeyStatus = useMemo(
    () =>
      String(config.sync.managementKey || "").trim() ? "已配置" : "未配置",
    [config.sync.managementKey],
  );
  const filteredInvites = useMemo(() => {
    const keyword = inviteQuery.trim().toLowerCase();
    if (!keyword) return invites;
    return invites.filter((item) =>
      [
        item.code,
        item.usedByUsername,
        item.usedByDisplayName,
        item.usedByUserId,
      ]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword)),
    );
  }, [inviteQuery, invites]);
  const filteredUsers = useMemo(() => {
    const keyword = userQuery.trim().toLowerCase();
    const matched = keyword
      ? users.filter((item) =>
          [item.username, item.name, item.id, item.role]
            .filter(Boolean)
            .some((value) => String(value).toLowerCase().includes(keyword)),
        )
      : [...users];
    if (userSortMode === "last_used_desc") {
      return matched.sort(
        (left, right) => getUserLastUsedTime(right) - getUserLastUsedTime(left),
      );
    }
    if (userSortMode === "last_used_asc") {
      return matched.sort(
        (left, right) => getUserLastUsedTime(left) - getUserLastUsedTime(right),
      );
    }
    if (userSortMode === "quota_desc") {
      return matched.sort(
        (left, right) =>
          getUserAvailableQuota(right) - getUserAvailableQuota(left),
      );
    }
    if (userSortMode === "quota_asc") {
      return matched.sort(
        (left, right) =>
          getUserAvailableQuota(left) - getUserAvailableQuota(right),
      );
    }
    return matched;
  }, [userQuery, userSortMode, users]);
  const invitePageCount = Math.max(
    1,
    Math.ceil(filteredInvites.length / MANAGEMENT_PAGE_SIZE),
  );
  const userPageCount = Math.max(
    1,
    Math.ceil(filteredUsers.length / MANAGEMENT_PAGE_SIZE),
  );
  const pagedInvites = useMemo(
    () =>
      filteredInvites.slice(
        (invitePage - 1) * MANAGEMENT_PAGE_SIZE,
        invitePage * MANAGEMENT_PAGE_SIZE,
      ),
    [invitePage, filteredInvites],
  );
  const pagedUsers = useMemo(
    () =>
      filteredUsers.slice(
        (userPage - 1) * MANAGEMENT_PAGE_SIZE,
        userPage * MANAGEMENT_PAGE_SIZE,
      ),
    [userPage, filteredUsers],
  );
  const storageMigrationNotice = useMemo(() => {
    if (!savedConfig) {
      return "";
    }
    const previousConversationStorage =
      savedConfig.storage.imageConversationStorage === "server"
        ? "服务器存储"
        : "浏览器存储";
    const nextConversationStorage =
      config.storage.imageConversationStorage === "server"
        ? "服务器存储"
        : "浏览器存储";
    const previousImageDataStorage =
      savedConfig.storage.imageDataStorage === "server"
        ? "服务器目录"
        : "浏览器 local";
    const nextImageDataStorage =
      config.storage.imageDataStorage === "server"
        ? "服务器目录"
        : "浏览器 local";
    const previousAccountStorage =
      savedConfig.storage.backend === "sqlite"
        ? "SQLite 数据库"
        : savedConfig.storage.backend === "redis"
          ? "Redis"
          : "本地文件";
    const nextAccountStorage =
      config.storage.backend === "sqlite"
        ? "SQLite 数据库"
        : config.storage.backend === "redis"
          ? "Redis"
          : "本地文件";
    const previousConfigStorage =
      savedConfig.storage.configBackend === "redis"
        ? "Redis"
        : "本地 config.toml";
    const nextConfigStorage =
      config.storage.configBackend === "redis" ? "Redis" : "本地 config.toml";

    const messages: string[] = [];
    if (previousAccountStorage !== nextAccountStorage) {
      messages.push(
        `账号池会从${previousAccountStorage}迁移到${nextAccountStorage}。`,
      );
    }
    if (previousConfigStorage !== nextConfigStorage) {
      messages.push(
        `配置文件会从${previousConfigStorage}迁移到${nextConfigStorage}。`,
      );
    }
    if (previousConversationStorage !== nextConversationStorage) {
      messages.push(
        `图片会话记录会从${previousConversationStorage}迁移到${nextConversationStorage}。`,
      );
    }
    if (previousImageDataStorage !== nextImageDataStorage) {
      messages.push(
        `图片数据会从${previousImageDataStorage}迁移到${nextImageDataStorage}。`,
      );
    }
    if (
      savedConfig.storage.imageConversationStorage === "server" &&
      config.storage.imageConversationStorage === "browser"
    ) {
      messages.push(
        "这次迁移需要把服务器图片重新下载回当前浏览器，历史图片较多时会更慢。",
      );
    }
    return messages.join(" ");
  }, [config, savedConfig]);

  const loadInvites = async () => {
    setIsLoadingInvites(true);
    try {
      const payload = await fetchInvites();
      setInvites(payload.items || []);
      setInvitePage(1);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取邀请码失败");
    } finally {
      setIsLoadingInvites(false);
    }
  };

  const handleCreateInvite = async () => {
    setIsCreatingInvite(true);
    try {
      const payload = await createInvite();
      if (payload.item?.code) {
        setLatestInviteCode(payload.item.code);
        setInvites((current) => [payload.item, ...current]);
        setInvitePage(1);
        toast.success(`邀请码已生成：${payload.item.code}`);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "生成邀请码失败");
    } finally {
      setIsCreatingInvite(false);
    }
  };

  const copyInviteCode = async (code: string) => {
    try {
      await navigator.clipboard.writeText(code);
      toast.success("邀请码已复制");
    } catch {
      toast.error("复制失败，请手动复制");
    }
  };

  const loadUsers = async () => {
    setIsLoadingUsers(true);
    try {
      const payload = await fetchUsers();
      setUsers(payload.items || []);
      setUserPage(1);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取用户失败");
    } finally {
      setIsLoadingUsers(false);
    }
  };

  const handleToggleUserDisabled = async (item: AppUserItem) => {
    setMutatingUserID(item.id);
    try {
      await updateUserDisabled(item.id, !item.disabled);
      setUsers((current) =>
        current.map((user) =>
          user.id === item.id ? { ...user, disabled: !item.disabled } : user,
        ),
      );
      toast.success(item.disabled ? "用户已启用" : "用户已禁用");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "更新用户状态失败");
    } finally {
      setMutatingUserID("");
    }
  };

  const handleDeleteUser = async (item: AppUserItem) => {
    setMutatingUserID(item.id);
    try {
      await deleteUser(item.id);
      setUsers((current) => current.filter((user) => user.id !== item.id));
      setUserPage((current) =>
        Math.min(
          current,
          Math.max(1, Math.ceil((users.length - 1) / MANAGEMENT_PAGE_SIZE)),
        ),
      );
      toast.success("用户已删除");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除用户失败");
    } finally {
      setMutatingUserID("");
    }
  };

  const handleAddQuota = async (item: AppUserItem) => {
    const input = window.prompt(
      "增加额度数量（正整数）：\n类型请在数字前加 p 表示 Paid，否则默认 Free\n例如：10 = Free+10，p5 = Paid+5",
      "10",
    );
    if (!input) return;
    const isPaid = input.trim().toLowerCase().startsWith("p");
    const num = parseInt(isPaid ? input.trim().slice(1) : input.trim(), 10);
    if (!num || num <= 0) {
      toast.error("请输入有效的正整数");
      return;
    }
    setMutatingUserID(item.id);
    try {
      await adjustUserQuota(item.id, isPaid ? "paid" : "free", num);
      toast.success(
        `已为 ${item.username} 增加 ${isPaid ? "Paid" : "Free"} 额度 ${num}`,
      );
      await loadUsers();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "调整额度失败");
    } finally {
      setMutatingUserID("");
    }
  };

  const handleAdjustAllUsersQuota = async () => {
    const delta = parseInt(bulkQuotaDelta.trim(), 10);
    if (!delta) {
      toast.error("请输入非 0 整数");
      return;
    }
    const action = delta > 0 ? "增加" : "减少";
    const label = bulkQuotaKind === "paid" ? "Paid" : "Free";
    if (
      !window.confirm(
        `确认给所有普通用户今日 ${label} 剩余额度统一${action} ${Math.abs(delta)}？\n此操作只影响今天，不改变每日重置额度。`,
      )
    ) {
      return;
    }
    setIsAdjustingAllUsersQuota(true);
    try {
      const result = await adjustAllUsersQuota(bulkQuotaKind, delta);
      toast.success(`已统一调整 ${result.affected} 个用户的今日 ${label} 额度`);
      await loadUsers();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "统一调整额度失败");
    } finally {
      setIsAdjustingAllUsersQuota(false);
    }
  };

  const loadConfig = async () => {
    setIsLoading(true);
    try {
      const [currentConfig, defaults] = await Promise.all([
        fetchConfig(),
        fetchDefaultConfig(),
      ]);
      const normalizedConfig = normalizeConfigPayload(currentConfig);
      const normalizedDefaults = normalizeConfigPayload(defaults);
      setCachedImageConversationStorageMode(
        normalizedConfig.storage.imageConversationStorage === "server"
          ? "server"
          : "browser",
      );
      setConfig(normalizedConfig);
      setSavedConfig(normalizedConfig);
      setDefaultConfig(normalizedDefaults);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "读取配置失败");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadConfig();
    void loadInvites();
    void loadUsers();
  }, []);

  const setSection = <K extends keyof ConfigPayload>(
    section: K,
    nextValue: ConfigPayload[K],
  ) => {
    setConfig((current) => ({
      ...current,
      [section]: nextValue,
    }));
  };

  const saveConfig = async () => {
    setIsSaving(true);
    try {
      const previousConfig = savedConfig
        ? normalizeConfigPayload(savedConfig)
        : normalizeConfigPayload(config);
      const previousConversationStorage =
        previousConfig.storage.imageConversationStorage === "server"
          ? "server"
          : "browser";
      const nextConversationStorage =
        config.storage.imageConversationStorage === "server"
          ? "server"
          : "browser";
      const needsServerHistoryMigration =
        (previousConversationStorage === "server" &&
          nextConversationStorage === "server" &&
          previousConfig.storage.backend !== config.storage.backend) ||
        previousConversationStorage !== nextConversationStorage;
      const sourceItems = needsServerHistoryMigration
        ? previousConversationStorage === "server"
          ? await exportServerImageConversationsSnapshot()
          : await exportLocalImageConversationsSnapshot()
        : null;
      if (needsServerHistoryMigration && sourceItems) {
        if (nextConversationStorage === "server") {
          await importImageConversationsToServerTarget(
            sourceItems,
            config.storage,
          );
        } else {
          await migrateImageConversationStorage({
            from: previousConversationStorage,
            to: nextConversationStorage,
            targetImageDataStorage:
              config.storage.imageDataStorage === "server"
                ? "server"
                : "browser",
            sourceItems,
          });
        }
      }
      const result = await updateConfig(config);
      const normalizedConfig = normalizeConfigPayload(result.config);
      const migratedCount = sourceItems?.length ?? 0;
      setCachedImageConversationStorageMode(
        normalizedConfig.storage.imageConversationStorage === "server"
          ? "server"
          : "browser",
      );
      clearCachedSyncStatus();
      setConfig(normalizedConfig);
      setSavedConfig(normalizedConfig);
      const migrationMessages: string[] = [];
      if (previousConfig.storage.backend !== normalizedConfig.storage.backend) {
        migrationMessages.push("账号池已迁移");
      }
      if (
        previousConfig.storage.configBackend !==
        normalizedConfig.storage.configBackend
      ) {
        migrationMessages.push("配置文件已迁移");
      }
      if (migratedCount > 0) {
        migrationMessages.push(`图片会话已迁移 ${migratedCount} 条`);
      }
      toast.success(
        migrationMessages.length > 0
          ? `配置已保存并立即生效：${migrationMessages.join("，")}`
          : "配置已保存并立即生效",
      );
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存配置失败");
    } finally {
      setIsSaving(false);
    }
  };

  const restoreDefaults = () => {
    setConfig(defaultConfig);
    toast.success("已恢复为默认配置草稿，点击“保存配置”后才会真正生效");
  };

  return (
    <section className="h-full">
      <div className="hide-scrollbar h-full min-h-0 overflow-y-auto rounded-[30px] border border-stone-200 bg-[#fcfcfb] px-4 pb-5 pt-0 shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] sm:px-5 sm:pb-6 sm:pt-0 lg:px-6 lg:pb-7 lg:pt-0">
        <div className="sticky top-0 z-20 -mx-4 bg-[#fcfcfb] px-4 pt-5 pb-4 transition-colors duration-200 dark:bg-[var(--studio-panel)] sm:-mx-5 sm:px-5 sm:pt-6 sm:pb-4 lg:-mx-6 lg:px-6 lg:pt-7 lg:pb-5">
          <section className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex items-center gap-4">
              <div className="inline-flex size-12 items-center justify-center rounded-[18px] bg-stone-950 text-white shadow-sm">
                <Settings2 className="size-5" />
              </div>
              <div className="space-y-1">
                <h1 className="text-2xl font-semibold tracking-tight text-stone-950">
                  配置管理
                </h1>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2 sm:justify-end">
              <Button
                type="button"
                variant="outline"
                className="h-10 w-full justify-center rounded-full border-stone-200 bg-white px-3 text-[13px] text-stone-700 shadow-none sm:w-auto"
                onClick={() => {
                  window.location.href = "/startup-check";
                }}
                disabled={isLoading || isSaving}
              >
                <Stethoscope className="size-4" />
                启动体检
              </Button>
              <Button
                type="button"
                variant="outline"
                className="h-10 w-full justify-center rounded-full border-stone-200 bg-white px-3 text-[13px] text-stone-700 shadow-none sm:w-auto"
                onClick={() => void loadConfig()}
                disabled={isLoading || isSaving}
              >
                {isLoading ? (
                  <LoaderCircle className="size-4 animate-spin" />
                ) : (
                  <RefreshCw className="size-4" />
                )}
                重新读取
              </Button>
              <Button
                type="button"
                variant="outline"
                className="h-10 w-full justify-center rounded-full border-stone-200 bg-white px-3 text-[13px] text-stone-700 shadow-none sm:w-auto"
                onClick={restoreDefaults}
                disabled={isLoading || isSaving}
              >
                <RefreshCcw className="size-4" />
                恢复默认
              </Button>
              <Button
                type="button"
                className="h-10 w-full justify-center rounded-full bg-stone-950 px-3 text-[13px] text-white hover:bg-stone-800 sm:w-auto"
                onClick={() => void saveConfig()}
                disabled={!isDirty || isLoading || isSaving}
              >
                {isSaving ? (
                  <LoaderCircle className="size-4 animate-spin" />
                ) : (
                  <Save className="size-4" />
                )}
                保存配置
              </Button>
            </div>
          </section>
        </div>

        {storageMigrationNotice ? (
          <div className="mt-1 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-900">
            {storageMigrationNotice}
          </div>
        ) : null}

        <div className="mt-5 space-y-5">
          <AnnouncementSection />

          <section className="rounded-[28px] border border-stone-200 bg-white/80 p-5 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-stone-950 dark:text-[var(--studio-text-strong)]">
                <Settings2 className="size-4" />
                <h2 className="text-lg font-semibold tracking-tight">
                  用户每日额度
                </h2>
              </div>
              <p className="text-sm leading-6 text-stone-500 dark:text-[var(--studio-text-muted)]">
                这里设置每天自然重置后的默认总额度；今日临时补偿或扣减请在用户管理里统一调整。
              </p>
            </div>
            <div className="mt-4 grid gap-3 md:grid-cols-2">
              <label className="space-y-2 text-sm">
                <span className="text-xs font-medium text-stone-500 dark:text-[var(--studio-text-muted)]">
                  每日 Free 重置额度
                </span>
                <Input
                  type="number"
                  min={1}
                  value={config.accounts.dailyFreeImageLimit}
                  onChange={(event) =>
                    setSection("accounts", {
                      ...config.accounts,
                      dailyFreeImageLimit: Number(event.target.value),
                    })
                  }
                  className="h-11 rounded-2xl border-stone-200 bg-white text-sm shadow-none"
                />
              </label>
              <label className="space-y-2 text-sm">
                <span className="text-xs font-medium text-stone-500 dark:text-[var(--studio-text-muted)]">
                  每日 Paid 重置额度
                </span>
                <Input
                  type="number"
                  min={1}
                  value={config.accounts.dailyPaidImageLimit}
                  onChange={(event) =>
                    setSection("accounts", {
                      ...config.accounts,
                      dailyPaidImageLimit: Number(event.target.value),
                    })
                  }
                  className="h-11 rounded-2xl border-stone-200 bg-white text-sm shadow-none"
                />
              </label>
            </div>
          </section>

          <section className="rounded-[28px] border border-stone-200 bg-white/80 p-5 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-stone-950 dark:text-[var(--studio-text-strong)]">
                  <KeyRound className="size-4" />
                  <h2 className="text-lg font-semibold tracking-tight">
                    邀请码管理
                  </h2>
                </div>
                <p className="text-sm leading-6 text-stone-500 dark:text-[var(--studio-text-muted)]">
                  管理员生成邀请码后，普通用户使用“用户名 + 密码 +
                  邀请码”完成注册。每个邀请码只能绑定一个用户。
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none"
                  onClick={() => void loadInvites()}
                  disabled={isLoadingInvites || isCreatingInvite}
                >
                  {isLoadingInvites ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <RefreshCw className="size-4" />
                  )}
                  刷新邀请码
                </Button>
                <Button
                  type="button"
                  className="h-10 rounded-full bg-stone-950 px-4 text-[13px] text-white hover:bg-stone-800"
                  onClick={() => void handleCreateInvite()}
                  disabled={isCreatingInvite}
                >
                  {isCreatingInvite ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <KeyRound className="size-4" />
                  )}
                  生成邀请码
                </Button>
              </div>
            </div>

            <div className="relative mt-4 max-w-md">
              <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-stone-400" />
              <Input
                value={inviteQuery}
                onChange={(event) => {
                  setInviteQuery(event.target.value);
                  setInvitePage(1);
                }}
                placeholder="搜索邀请码 / 绑定用户"
                className="h-10 rounded-2xl border-stone-200 bg-white pl-10 text-sm shadow-none"
              />
            </div>

            {latestInviteCode ? (
              <div className="mt-4 rounded-2xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900/40 dark:bg-emerald-950/20">
                <div className="text-xs text-emerald-700 dark:text-emerald-300">
                  最新生成的邀请码
                </div>
                <div className="mt-2 flex flex-col gap-2 sm:flex-row sm:items-center">
                  <Input
                    value={latestInviteCode}
                    readOnly
                    className="h-11 rounded-2xl border-emerald-200 bg-white font-mono text-sm shadow-none"
                  />
                  <Button
                    type="button"
                    variant="outline"
                    className="h-11 rounded-2xl border-emerald-200 bg-white text-emerald-700 shadow-none"
                    onClick={() => void copyInviteCode(latestInviteCode)}
                  >
                    <Copy className="size-4" />
                    复制
                  </Button>
                </div>
              </div>
            ) : null}

            <div className="mt-4 overflow-hidden rounded-2xl border border-stone-200 dark:border-[var(--studio-border)]">
              <div className="grid grid-cols-[1.2fr_1fr_1fr] gap-3 bg-stone-50 px-4 py-3 text-xs font-medium text-stone-500 dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]">
                <span>邀请码</span>
                <span>状态</span>
                <span>绑定用户</span>
              </div>
              <div className="divide-y divide-stone-200 dark:divide-[var(--studio-border)]">
                {filteredInvites.length === 0 ? (
                  <div className="px-4 py-6 text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">
                    {isLoadingInvites
                      ? "正在读取邀请码..."
                      : inviteQuery.trim()
                        ? "没有匹配的邀请码。"
                        : "还没有邀请码，先生成一个。"}
                  </div>
                ) : (
                  pagedInvites.map((item) => (
                    <div
                      key={item.code}
                      className="grid grid-cols-[1.2fr_1fr_1fr] gap-3 px-4 py-4 text-sm"
                    >
                      <button
                        type="button"
                        className="min-w-0 truncate text-left font-mono text-stone-950 dark:text-[var(--studio-text-strong)]"
                        onClick={() => void copyInviteCode(item.code)}
                        title="点击复制邀请码"
                      >
                        {item.code}
                      </button>
                      <span
                        className={
                          item.usedByUserId
                            ? "text-amber-700 dark:text-amber-300"
                            : "text-emerald-700 dark:text-emerald-300"
                        }
                      >
                        {item.usedByUserId ? "已使用" : "未使用"}
                      </span>
                      <span className="truncate text-stone-600 dark:text-[var(--studio-text)]">
                        {item.usedByUsername || item.usedByDisplayName || "—"}
                      </span>
                    </div>
                  ))
                )}
              </div>
            </div>
            <div className="mt-3 flex items-center justify-between text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
              <span>
                共 {filteredInvites.length} 条，第 {invitePage}/
                {invitePageCount} 页
              </span>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 rounded-full border-stone-200 bg-white px-3 text-xs shadow-none"
                  onClick={() => setInvitePage((page) => Math.max(1, page - 1))}
                  disabled={invitePage <= 1}
                >
                  上一页
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 rounded-full border-stone-200 bg-white px-3 text-xs shadow-none"
                  onClick={() =>
                    setInvitePage((page) => Math.min(invitePageCount, page + 1))
                  }
                  disabled={invitePage >= invitePageCount}
                >
                  下一页
                </Button>
              </div>
            </div>
          </section>

          <section className="rounded-[28px] border border-stone-200 bg-white/80 p-5 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-stone-950 dark:text-[var(--studio-text-strong)]">
                  <UserRound className="size-4" />
                  <h2 className="text-lg font-semibold tracking-tight">
                    用户管理
                  </h2>
                </div>
                <p className="text-sm leading-6 text-stone-500 dark:text-[var(--studio-text-muted)]">
                  查看普通用户、禁用登录与 API 调用，或删除用户账号。内置 admin
                  不在这里维护。
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none"
                onClick={() => void loadUsers()}
                disabled={isLoadingUsers || !!mutatingUserID}
              >
                {isLoadingUsers ? (
                  <LoaderCircle className="size-4 animate-spin" />
                ) : (
                  <RefreshCw className="size-4" />
                )}
                刷新用户
              </Button>
            </div>

            <div className="mt-4 rounded-2xl border border-stone-200 bg-stone-50/70 p-4 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
                <div className="space-y-1">
                  <div className="text-sm font-medium text-stone-900 dark:text-[var(--studio-text-strong)]">
                    统一调整今日额度
                  </div>
                  <p className="text-xs leading-5 text-stone-500 dark:text-[var(--studio-text-muted)]">
                    正数增加今日剩余额度，负数减少今日剩余额度；不影响每天重置后的默认额度。
                  </p>
                </div>
                <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                  <Select
                    value={bulkQuotaKind}
                    onValueChange={(value) =>
                      setBulkQuotaKind(value as "free" | "paid")
                    }
                  >
                    <SelectTrigger className="h-10 w-full rounded-2xl border-stone-200 bg-white text-sm shadow-none sm:w-28">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="free">Free</SelectItem>
                      <SelectItem value="paid">Paid</SelectItem>
                    </SelectContent>
                  </Select>
                  <Input
                    value={bulkQuotaDelta}
                    onChange={(event) => setBulkQuotaDelta(event.target.value)}
                    placeholder="例如 10 或 -5"
                    className="h-10 rounded-2xl border-stone-200 bg-white text-sm shadow-none sm:w-36"
                  />
                  <Button
                    type="button"
                    className="h-10 rounded-full bg-stone-950 px-4 text-[13px] text-white hover:bg-stone-800"
                    onClick={() => void handleAdjustAllUsersQuota()}
                    disabled={isAdjustingAllUsersQuota || isLoadingUsers}
                  >
                    {isAdjustingAllUsersQuota ? (
                      <LoaderCircle className="size-4 animate-spin" />
                    ) : (
                      <Plus className="size-4" />
                    )}
                    应用到所有用户
                  </Button>
                </div>
              </div>
            </div>

            <div className="mt-4 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div className="relative w-full max-w-md">
                <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-stone-400" />
                <Input
                  value={userQuery}
                  onChange={(event) => {
                    setUserQuery(event.target.value);
                    setUserPage(1);
                  }}
                  placeholder="搜索用户名 / 昵称 / ID / 角色"
                  className="h-10 rounded-2xl border-stone-200 bg-white pl-10 text-sm shadow-none"
                />
              </div>
              <Select
                value={userSortMode}
                onValueChange={(value) => {
                  setUserSortMode(value as UserSortMode);
                  setUserPage(1);
                }}
              >
                <SelectTrigger className="h-10 w-full rounded-2xl border-stone-200 bg-white text-sm shadow-none lg:w-56">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {userSortOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="mt-4 overflow-hidden rounded-2xl border border-stone-200 dark:border-[var(--studio-border)]">
              <div className="grid grid-cols-[1fr_0.6fr_0.6fr_0.9fr_1.2fr_1.4fr] gap-3 bg-stone-50 px-4 py-3 text-xs font-medium text-stone-500 dark:bg-[var(--studio-panel)] dark:text-[var(--studio-text-muted)]">
                <span>用户</span>
                <span>状态</span>
                <span>角色</span>
                <span>最近使用</span>
                <span>今日额度</span>
                <span>操作</span>
              </div>
              <div className="divide-y divide-stone-200 dark:divide-[var(--studio-border)]">
                {filteredUsers.length === 0 ? (
                  <div className="px-4 py-6 text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">
                    {isLoadingUsers
                      ? "正在读取用户..."
                      : userQuery.trim()
                        ? "没有匹配的用户。"
                        : "还没有普通用户。"}
                  </div>
                ) : (
                  pagedUsers.map((item) => (
                    <div
                      key={item.id}
                      className="grid grid-cols-[1fr_0.6fr_0.6fr_0.9fr_1.2fr_1.4fr] items-center gap-3 px-4 py-4 text-sm"
                    >
                      <div className="min-w-0">
                        <div className="truncate font-medium text-stone-950 dark:text-[var(--studio-text-strong)]">
                          {item.username}
                        </div>
                        <div className="mt-1 truncate text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
                          {item.name || item.id}
                        </div>
                      </div>
                      <span
                        className={
                          item.disabled
                            ? "text-red-600 dark:text-red-300"
                            : "text-emerald-700 dark:text-emerald-300"
                        }
                      >
                        {item.disabled ? "已禁用" : "正常"}
                      </span>
                      <span className="text-stone-600 dark:text-[var(--studio-text)]">
                        {item.role}
                      </span>
                      <span
                        className="text-xs text-stone-600 dark:text-[var(--studio-text)]"
                        title={item.lastUsedAt || ""}
                      >
                        {formatManagementTime(item.lastUsedAt)}
                      </span>
                      <div className="text-xs text-stone-600 dark:text-[var(--studio-text)]">
                        {item.quota ? (
                          <>
                            <span>
                              Free {item.quota.freeRemaining}/
                              {item.quota.freeLimit}
                            </span>
                            <span className="mx-1">·</span>
                            <span>
                              Paid {item.quota.paidRemaining}/
                              {item.quota.paidLimit}
                            </span>
                          </>
                        ) : (
                          <span className="text-stone-400">-</span>
                        )}
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <Button
                          type="button"
                          variant="outline"
                          className="h-9 rounded-xl border-stone-200 bg-white px-3 text-xs text-stone-700 shadow-none"
                          onClick={() => void handleAddQuota(item)}
                          disabled={mutatingUserID === item.id}
                        >
                          <Plus className="size-3" />
                          额度
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          className="h-9 rounded-xl border-stone-200 bg-white px-3 text-xs text-stone-700 shadow-none"
                          onClick={() => void handleToggleUserDisabled(item)}
                          disabled={mutatingUserID === item.id}
                        >
                          {mutatingUserID === item.id ? (
                            <LoaderCircle className="size-3 animate-spin" />
                          ) : null}
                          {item.disabled ? "启用" : "禁用"}
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          className="h-9 rounded-xl border-red-200 bg-white px-3 text-xs text-red-600 shadow-none hover:bg-red-50"
                          onClick={() => void handleDeleteUser(item)}
                          disabled={mutatingUserID === item.id}
                        >
                          <Trash2 className="size-3" />
                          删除
                        </Button>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
            <div className="mt-3 flex items-center justify-between text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
              <span>
                共 {filteredUsers.length} 个用户，第 {userPage}/{userPageCount}{" "}
                页
              </span>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 rounded-full border-stone-200 bg-white px-3 text-xs shadow-none"
                  onClick={() => setUserPage((page) => Math.max(1, page - 1))}
                  disabled={userPage <= 1}
                >
                  上一页
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 rounded-full border-stone-200 bg-white px-3 text-xs shadow-none"
                  onClick={() =>
                    setUserPage((page) => Math.min(userPageCount, page + 1))
                  }
                  disabled={userPage >= userPageCount}
                >
                  下一页
                </Button>
              </div>
            </div>
          </section>

          <ImageModeSection
            config={config}
            isStudioMode={isStudioMode}
            isCPAMode={isCPAMode}
            effectiveCPAImageBaseUrl={effectiveCPAImageBaseUrl}
            syncManagementKeyStatus={syncManagementKeyStatus}
            setSection={setSection}
          />

          <ImageSystemHintSection />

          <IntegrationSection config={config} setSection={setSection} />

          <RuntimeSection config={config} setSection={setSection} />

          <ServicePathsSection
            config={config}
            setConfig={setConfig}
            resolvedStaticDir={resolvedStaticDir}
            startupErrorPath={startupErrorPath}
          />
        </div>
      </div>
    </section>
  );
}

function AnnouncementSection() {
  const [content, setContent] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [isSaving, setIsSaving] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [hasExisting, setHasExisting] = useState(false);

  useEffect(() => {
    fetchAnnouncement()
      .then((data) => {
        if (data.active && data.content) {
          setContent(data.content);
          setExpiresAt(data.expiresAt ?? "");
          setHasExisting(true);
        }
      })
      .catch(() => {})
      .finally(() => setIsLoading(false));
  }, []);

  const handleSave = async () => {
    const trimmed = content.trim();
    if (!trimmed) {
      toast.error("请输入公告内容");
      return;
    }
    setIsSaving(true);
    try {
      await setAnnouncement(trimmed, expiresAt);
      setHasExisting(true);
      toast.success("公告已保存");
    } catch {
      toast.error("保存失败");
    } finally {
      setIsSaving(false);
    }
  };

  const handleDelete = async () => {
    setIsSaving(true);
    try {
      await deleteAnnouncement();
      setContent("");
      setExpiresAt("");
      setHasExisting(false);
      toast.success("公告已清除");
    } catch {
      toast.error("清除失败");
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <section className="rounded-[28px] border border-stone-200 bg-white/80 p-5 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)]">
      <div className="space-y-2">
        <div className="flex items-center gap-2 text-stone-950 dark:text-[var(--studio-text-strong)]">
          <Megaphone className="size-4" />
          <h2 className="text-lg font-semibold tracking-tight">公告管理</h2>
        </div>
        <p className="text-sm leading-6 text-stone-500 dark:text-[var(--studio-text-muted)]">
          设置登录时弹窗公告，支持设定过期时间。用户登录成功后会看到弹窗提示。
        </p>
      </div>

      {isLoading ? (
        <div className="mt-4 flex items-center gap-2 text-sm text-stone-400">
          <LoaderCircle className="size-4 animate-spin" />
          加载中…
        </div>
      ) : (
        <div className="mt-4 space-y-3">
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder="输入公告内容…"
            rows={3}
            className="w-full rounded-2xl border border-stone-200 bg-stone-50 px-4 py-3 text-sm leading-6 text-stone-900 shadow-none outline-none focus:ring-1 focus:ring-stone-300 dark:border-[var(--studio-border)] dark:bg-[var(--studio-bg)] dark:text-[var(--studio-text)]"
          />
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <div className="flex items-center gap-2">
              <label className="shrink-0 text-sm text-stone-500 dark:text-[var(--studio-text-muted)]">
                过期时间
              </label>
              <Input
                type="datetime-local"
                value={expiresAt ? expiresAt.slice(0, 16) : ""}
                onChange={(e) => {
                  const v = e.target.value;
                  setExpiresAt(v ? new Date(v).toISOString() : "");
                }}
                className="h-10 w-auto rounded-xl border-stone-200 bg-stone-50 px-3 text-sm shadow-none focus-visible:ring-1"
              />
            </div>
            <div className="flex gap-2 sm:ml-auto">
              {hasExisting ? (
                <Button
                  type="button"
                  variant="outline"
                  className="h-10 rounded-full border-stone-200 bg-white px-4 text-[13px] text-stone-700 shadow-none"
                  onClick={() => void handleDelete()}
                  disabled={isSaving}
                >
                  <Trash2 className="mr-1.5 size-3.5" />
                  清除公告
                </Button>
              ) : null}
              <Button
                type="button"
                className="h-10 rounded-full bg-stone-950 px-5 text-[13px] text-white shadow-none hover:bg-stone-800"
                onClick={() => void handleSave()}
                disabled={isSaving}
              >
                {isSaving ? (
                  <LoaderCircle className="mr-1.5 size-3.5 animate-spin" />
                ) : (
                  <Save className="mr-1.5 size-3.5" />
                )}
                保存公告
              </Button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function ImageSystemHintSection() {
  const [commonHint, setCommonHint] = useState("");
  const [privateHint, setPrivateHint] = useState("");
  const [savedCommonHint, setSavedCommonHint] = useState("");
  const [savedPrivateHint, setSavedPrivateHint] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    fetchImageSystemHint()
      .then((data) => {
        const nextCommonHint = data.imageCommonSystemHint || "";
        const nextPrivateHint =
          data.imagePrivateSystemHint || data.imageSystemHint || "";
        setCommonHint(nextCommonHint);
        setPrivateHint(nextPrivateHint);
        setSavedCommonHint(nextCommonHint);
        setSavedPrivateHint(nextPrivateHint);
      })
      .catch(() => {})
      .finally(() => setIsLoading(false));
  }, []);

  const handleSave = async () => {
    setIsSaving(true);
    try {
      const result = await updateImageSystemHint({
        imageCommonSystemHint: commonHint,
        imagePrivateSystemHint: privateHint,
      });
      const nextCommonHint = result.imageCommonSystemHint || "";
      const nextPrivateHint =
        result.imagePrivateSystemHint || result.imageSystemHint || "";
      setSavedCommonHint(nextCommonHint);
      setSavedPrivateHint(nextPrivateHint);
      setCommonHint(nextCommonHint);
      setPrivateHint(nextPrivateHint);
      toast.success("系统提示词已保存");
    } catch {
      toast.error("保存失败");
    } finally {
      setIsSaving(false);
    }
  };

  const isDirty =
    commonHint !== savedCommonHint || privateHint !== savedPrivateHint;

  return (
    <section className="rounded-xl border border-stone-200 bg-white p-5 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)]">
      <h3 className="mb-3 text-sm font-semibold text-stone-800 dark:text-[var(--studio-text-strong)]">
        生图系统提示词
      </h3>
      <p className="mb-3 text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
        系统提示词会注入到 Responses instructions；留空则不注入。
      </p>
      {isLoading ? (
        <div className="text-sm text-stone-400">加载中...</div>
      ) : (
        <>
          <div className="space-y-2">
            <label className="block text-xs font-medium text-stone-600 dark:text-[var(--studio-text-muted)]">
              通用系统提示词
            </label>
            <p className="text-xs text-stone-400 dark:text-[var(--studio-text-muted)]">
              所有生图、修图请求都会注入。
            </p>
            <textarea
              value={commonHint}
              onChange={(e) => setCommonHint(e.target.value)}
              rows={4}
              className="w-full rounded-lg border border-stone-200 bg-stone-50 px-3 py-2 text-sm text-stone-800 placeholder:text-stone-400 focus:border-stone-400 focus:outline-none focus:ring-1 focus:ring-stone-300 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)] dark:placeholder:text-[var(--studio-text-muted)]"
              placeholder="例如：你是专业图像生成助手，必须严格执行用户的图片生成/编辑请求，优先保证画面质量、构图稳定、细节完整。"
            />
          </div>
          <div className="mt-4 space-y-2">
            <label className="block text-xs font-medium text-stone-600 dark:text-[var(--studio-text-muted)]">
              隐藏模式系统提示词
            </label>
            <p className="text-xs text-stone-400 dark:text-[var(--studio-text-muted)]">
              仅在隐藏模式开启后额外注入，会追加在通用系统提示词后面。
            </p>
            <textarea
              value={privateHint}
              onChange={(e) => setPrivateHint(e.target.value)}
              rows={4}
              className="w-full rounded-lg border border-stone-200 bg-stone-50 px-3 py-2 text-sm text-stone-800 placeholder:text-stone-400 focus:border-stone-400 focus:outline-none focus:ring-1 focus:ring-stone-300 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)] dark:placeholder:text-[var(--studio-text-muted)]"
              placeholder="例如：隐藏模式下优先保留用户原始创作意图，同时提高成图质量、稳定性和通过率。"
            />
          </div>
          <div className="mt-3 flex items-center gap-3">
            <Button
              type="button"
              variant="default"
              className="h-8 rounded-full px-4 text-xs"
              onClick={() => void handleSave()}
              disabled={isSaving || !isDirty}
            >
              {isSaving ? "保存中..." : "保存提示词"}
            </Button>
            {isDirty ? (
              <span className="text-xs text-amber-600">有未保存的修改</span>
            ) : null}
          </div>
        </>
      )}
    </section>
  );
}
