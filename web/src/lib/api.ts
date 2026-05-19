import { httpRequest } from "@/lib/request";
import webConfig from "@/constants/common-env";
import { getStoredAuthKey, getStoredAuthUser, type AuthUser } from "@/store/auth";
import {
  buildImageAccountPolicyHeader,
  normalizeImageAccountPolicy,
  type StoredImageAccountPolicy,
} from "@/store/image-account-policy";

export type AccountType = "Free" | "Plus" | "Pro" | "Team";
export type AccountStatus = "正常" | "限流" | "异常" | "禁用";
export type SyncStatus =
  | "synced"
  | "pending_upload"
  | "remote_only"
  | "remote_deleted";
export type SyncSource = "cpa" | "newapi" | "sub2api";
export type AccountSourceKind = "auth_file" | "token";
export type ImageModel = "gpt-image-1" | "gpt-image-2";
export type ImageQuality = "low" | "medium" | "high";
export type ImageResolutionAccess = "free" | "paid";
export type ImageResponseItem = {
  url?: string;
  b64_json?: string;
  revised_prompt?: string;
  file_id?: string;
  gen_id?: string;
  conversation_id?: string;
  parent_message_id?: string;
  response_id?: string;
  source_account_id?: string;
  error?: string;
};

export type ImageTaskStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "cancel_requested"
  | "cancelled"
  | "expired";

export type ImageTaskWaitingReason =
  | ""
  | "global_concurrency"
  | "paid_account_busy"
  | "compatible_account_busy"
  | "source_account_busy"
  | "retry_backoff";

export type ImageTaskBlocker = {
  code: string;
  detail?: string;
};

export type ImageTaskView = {
  id: string;
  conversationId: string;
  turnId: string;
  mode: "generate" | "edit" | string;
  status: ImageTaskStatus;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  count: number;
  retryImageIndex?: number;
  queuePosition?: number;
  waitingReason?: ImageTaskWaitingReason;
  blockers?: ImageTaskBlocker[];
  images: ImageResponseItem[];
  error?: string;
  cancelRequested?: boolean;
};

export type DailyImageQuota = {
  dateKey: string;
  freeLimit: number;
  freeUsed: number;
  freeRemaining: number;
  paidLimit: number;
  paidUsed: number;
  paidRemaining: number;
};

export type ImageTaskSnapshot = {
  running: number;
  maxRunning: number;
  queued: number;
  total: number;
  activeSources: {
    workspace: number;
    compat: number;
  };
  finalStatuses: {
    succeeded: number;
    failed: number;
    cancelled: number;
    expired: number;
  };
  retentionSeconds: number;
};

export type ImageTaskStreamEvent = {
  type: string;
  taskId?: string;
  task?: ImageTaskView;
  snapshot?: ImageTaskSnapshot;
};

export type InpaintSourceReference = {
  original_file_id: string;
  original_gen_id: string;
  conversation_id?: string;
  parent_message_id?: string;
  response_id?: string;
  source_account_id: string;
};

export type ImageReferenceImage = {
  id: string;
  name: string;
  dataUrl?: string;
  url?: string;
};

export type ImageContextReference = {
  conversation_id?: string;
  parent_message_id?: string;
  response_id?: string;
  source_account_id: string;
};

export type Account = {
  id: string;
  fileName: string;
  access_token: string;
  sourceKind?: AccountSourceKind | null;
  type: AccountType;
  status: AccountStatus;
  quota: number;
  email?: string | null;
  user_id?: string | null;
  codex_quota_known?: boolean;
  codex_7d_used_percent?: number | null;
  codex_7d_reset_after_seconds?: number | null;
  codex_7d_window_minutes?: number | null;
  codex_5h_used_percent?: number | null;
  codex_5h_reset_after_seconds?: number | null;
  codex_5h_window_minutes?: number | null;
  limits_progress?: Array<{
    feature_name?: string;
    remaining?: number;
    reset_after?: string;
  }>;
  default_model_slug?: string | null;
  restoreAt?: string | null;
  success: number;
  fail: number;
  lastUsedAt: string | null;
  provider?: string;
  disabled?: boolean;
  note?: string | null;
  priority?: number;
  syncStatus?: SyncStatus | null;
  syncOrigin?: string | null;
  lastSyncedAt?: string | null;
  remoteDisabled?: boolean | null;
  importedAt?: string | null;
  imageRoutes?: Array<"legacy" | "responses" | string>;
};

export type SyncAccount = {
  name: string;
  status: SyncStatus;
  location: "local" | "remote" | "both";
  localDisabled?: boolean | null;
  remoteDisabled?: boolean | null;
};

export type SyncRunResult = {
  ok: boolean;
  running?: boolean;
  source: SyncSource;
  error?: string;
  direction?: string;
  imported: number;
  exported: number;
  skipped: number;
  failed: number;
  inaccessible: number;
  total?: number;
  processed?: number;
  phase?: string;
  current?: string;
  notes?: string[];
  started_at: string;
  finished_at: string;
  updated_at?: string;
};

export type SyncStatusResponse = {
  source: SyncSource;
  label: string;
  configured: boolean;
  pullSupported: boolean;
  pushSupported: boolean;
  local: number;
  remote: number;
  pendingPush: number;
  pendingPull: number;
  inaccessibleRemote: number;
  notes?: string[];
  lastRun?: SyncRunResult | null;
};

type AccountListResponse = {
  items: Account[];
};

type AccountMutationResponse = {
  items: Account[];
  added?: number;
  skipped?: number;
  removed?: number;
  refreshed?: number;
  errors?: Array<{ access_token: string; error: string }>;
};

export type AccountImportResponse = {
  items: Account[];
  imported?: number;
  imported_files?: number;
  refreshed?: number;
  errors?: Array<{ access_token: string; error: string }>;
  duplicates?: Array<{ name: string; reason: string }>;
  failed?: Array<{ name: string; error: string }>;
};

type AccountRefreshResponse = {
  items: Account[];
  refreshed: number;
  errors: Array<{ access_token: string; error: string }>;
};

export type AccountRefreshProgress = {
  ok: boolean;
  running: boolean;
  cancelRequested?: boolean;
  error?: string;
  scope?: "all" | "routing_groups" | string;
  total: number;
  processed: number;
  refreshed: number;
  failed: number;
  current?: string;
  started_at: string;
  finished_at: string;
  updated_at?: string;
};

type AccountRefreshAllResponse = {
  progress: AccountRefreshProgress | null;
  alreadyRunning?: boolean;
  stopped?: boolean;
};

type AccountUpdateResponse = {
  item: Account;
  items: Account[];
};

export type AccountQuotaResponse = {
  id: string;
  email?: string | null;
  status: AccountStatus;
  type: AccountType;
  quota: number;
  image_gen_remaining?: number | null;
  image_gen_reset_after?: string | null;
  codex_quota_known?: boolean;
  codex_7d_used_percent?: number | null;
  codex_7d_reset_after_seconds?: number | null;
  codex_7d_window_minutes?: number | null;
  codex_5h_used_percent?: number | null;
  codex_5h_reset_after_seconds?: number | null;
  codex_5h_window_minutes?: number | null;
  refresh_requested: boolean;
  refreshed: boolean;
  refresh_error?: string;
};

export type ImageMode = "studio" | "cpa";

type ImageResponse = {
  created: number;
  data: ImageResponseItem[];
};

type ImageTaskListResponse = {
  items: ImageTaskView[];
  snapshot: ImageTaskSnapshot;
};

type ImageTaskResponse = {
  task: ImageTaskView;
  snapshot: ImageTaskSnapshot;
};

export type ConfigPayload = {
  app: {
    name: string;
    version: string;
    apiKey: string;
    authKey: string;
    imageFormat: string;
    maxUploadSizeMB: number;
  };
  server: {
    host: string;
    port: number;
    staticDir: string;
    maxImageConcurrency: number;
    imageQueueLimit: number;
    imageQueueTimeoutSeconds: number;
    imageTaskQueueTtlSeconds: number;
  };
  chatgpt: {
    model: string;
    sseTimeout: number;
    pollInterval: number;
    pollMaxWait: number;
    requestTimeout: number;
    imageMode: ImageMode;
    freeImageRoute: string;
    freeImageModel: string;
    paidImageRoute: string;
    paidImageModel: string;
    studioAllowDisabledImageAccounts: boolean;
    imageAccountRetryTimes: number;
    imageCommonSystemHint: string;
    imagePrivateSystemHint: string;
    imageSystemHint: string;
  };
  accounts: {
    defaultQuota: number;
    preferRemoteRefresh: boolean;
    refreshWorkers: number;
    imageQuotaRefreshTTLSeconds: number;
  };
  storage: {
    backend: string;
    configBackend: "file" | "redis" | string;
    authDir: string;
    stateFile: string;
    syncStateDir: string;
    imageDir: string;
    imageStorage: "browser" | "server" | string;
    imageConversationStorage: "browser" | "server" | string;
    imageDataStorage: "browser" | "server" | string;
    sqlitePath: string;
    redisAddr: string;
    redisPassword: string;
    redisDb: number;
    redisPrefix: string;
  };
  sync: {
    enabled: boolean;
    baseUrl: string;
    managementKey: string;
    requestTimeout: number;
    concurrency: number;
    providerType: string;
  };
  proxy: {
    enabled: boolean;
    url: string;
    mode: string;
    syncEnabled: boolean;
  };
  cpa: {
    baseUrl: string;
    apiKey: string;
    requestTimeout: number;
    routeStrategy: "images_api" | "codex_responses" | "auto";
  };
  externalResponses: {
    enabled: boolean;
    baseUrl: string;
    apiKey: string;
    model: string;
    requestTimeout: number;
  };
  newapi: {
    baseUrl: string;
    username: string;
    password: string;
    accessToken: string;
    userId: number;
    sessionCookie: string;
    requestTimeout: number;
  };
  sub2api: {
    baseUrl: string;
    email: string;
    password: string;
    apiKey: string;
    groupId: string;
    requestTimeout: number;
  };
  log: {
    logAllRequests: boolean;
  };
  paths: {
    root: string;
    defaults: string;
    override: string;
  };
};

export type RequestLogFilterOption = {
  value: string;
  label: string;
};

export type RequestLogFilterOptions = {
  users: RequestLogFilterOption[];
  accounts: RequestLogFilterOption[];
};

export type RequestLogItem = {
  id: string;
  startedAt: string;
  finishedAt: string;
  operation: string;
  route: string;
  cpaSubroute?: string;
  size?: string;
  quality?: string;
  promptLength?: number;
  userId?: string;
  username?: string;
  userRole?: string;
  accountEmail?: string;
  accountType?: string;
  accountFile?: string;
  success: boolean;
  errorCode?: string;
  error?: string;
};

export type RequestLogDetail = {
  id: string;
  startedAt: string;
  finishedAt: string;
  endpoint: string;
  operation: string;
  imageMode: ImageMode | string;
  direction: "official" | "cpa" | string;
  route: string;
  cpaSubroute?: string;
  queueWaitMs?: number;
  inflightCountAtStart?: number;
  leaseAcquired?: boolean;
  errorCode?: string;
  routingPolicyApplied?: boolean;
  routingGroupIndex?: number;
  routingSortMode?: string;
  routingReservePercent?: number;
  accountType?: string;
  accountEmail?: string;
  accountFile?: string;
  requestedModel?: string;
  upstreamModel?: string;
  imageToolModel?: string;
  userId?: string;
  username?: string;
  userRole?: string;
  size?: string;
  quality?: string;
  prompt?: string;
  promptLength?: number;
  imageUrls?: string[];
  imageNames?: string[];
  preferred: boolean;
  success: boolean;
  error?: string;
};

export type VersionInfo = {
  version: string;
  commit?: string;
  buildTime?: string;
};

export type StartupCheckItem = {
  key: string;
  label: string;
  status: "pass" | "warn" | "fail" | string;
  detail: string;
  hint?: string;
  durationMs: number;
};

export type StartupCheckResponse = {
  startedAt: string;
  finishedAt: string;
  mode: "studio" | "cpa" | string;
  overall: "pass" | "warn" | "fail" | string;
  passCount: number;
  warnCount: number;
  failCount: number;
  checks: StartupCheckItem[];
  summaryText: string;
};

export type RuntimeStatusResponse = {
  timestamp: string;
  mode: "studio" | "cpa" | string;
  admission: {
    maxConcurrency: number;
    queueLimit: number;
    queueTimeoutMs: number;
    inflight: number;
    queued: number;
  };
  accounts: {
    total: number;
    available: number;
    availablePaid: number;
  };
  recent: {
    windowSeconds: number;
    failureCount: number;
    lastError?: string;
    lastErrorCode?: string;
    lastErrorAt?: string;
    lastErrorAccount?: string;
  };
  tasks: {
    total: number;
    running: number;
    queued: number;
    activeSources: {
      workspace: number;
      compat: number;
    };
    finalStatuses: {
      succeeded: number;
      failed: number;
      cancelled: number;
      expired: number;
    };
    retentionSeconds: number;
  };
};

type ImageAccountPolicyResponse = {
  policy: StoredImageAccountPolicy;
};

let cachedImageAccountPolicy: StoredImageAccountPolicy | null = null;
let cachedConfig: ConfigPayload | null = null;

export function setCachedImageAccountPolicy(
  policy: StoredImageAccountPolicy | null,
) {
  cachedImageAccountPolicy = policy ? normalizeImageAccountPolicy(policy) : null;
}

function setCachedConfig(config: ConfigPayload | null) {
  cachedConfig = config;
}

export async function fetchImageAccountPolicy() {
  const data = await httpRequest<ImageAccountPolicyResponse>(
    "/api/accounts/image-policy",
  );
  const normalized = normalizeImageAccountPolicy(data.policy);
  setCachedImageAccountPolicy(normalized);
  return normalized;
}

export async function updateImageAccountPolicy(
  policy: StoredImageAccountPolicy,
) {
  const data = await httpRequest<ImageAccountPolicyResponse>(
    "/api/accounts/image-policy",
    {
      method: "PUT",
      body: { policy: normalizeImageAccountPolicy(policy) },
    },
  );
  const normalized = normalizeImageAccountPolicy(data.policy);
  setCachedImageAccountPolicy(normalized);
  return normalized;
}

export async function getImageAccountPolicyForRequest() {
  if (cachedImageAccountPolicy) {
    return cachedImageAccountPolicy;
  }
  const user = await getStoredAuthUser();
  if (user && user.role !== "admin") {
    return normalizeImageAccountPolicy(null);
  }
  try {
    return await fetchImageAccountPolicy();
  } catch {
    return normalizeImageAccountPolicy(null);
  }
}

function resolveImageResponseFormat(config: ConfigPayload | null) {
  return config?.storage.imageDataStorage === "server" ? "url" : "b64_json";
}

async function getImageResponseFormatForRequest() {
  if (cachedConfig) {
    return resolveImageResponseFormat(cachedConfig);
  }
  const user = await getStoredAuthUser();
  if (user && user.role !== "admin") {
    return "url";
  }
  try {
    return resolveImageResponseFormat(await fetchConfig());
  } catch {
    return "b64_json";
  }
}

export type ProxyTestResult = {
  ok: boolean;
  status: number;
  latency: number;
  error?: string;
};

export type IntegrationTestResult = {
  ok: boolean;
  source: SyncSource | "cpa";
  message: string;
  status: number;
  latency: number;
  userId?: number;
  username?: string;
  email?: string;
  groupCount?: number;
};

export type NewAPITokenDiscoverResult = {
  ok: boolean;
  message: string;
  latency: number;
  accessToken?: string;
  userId?: number;
};

export type Sub2APIGroupOption = {
  id: string;
  name: string;
  description: string;
  platform: string;
  status: string;
};

export type Sub2APIGroupsResult = {
  ok: boolean;
  message: string;
  latency: number;
  groups: Sub2APIGroupOption[];
};

export type AuthResponse = {
  ok: boolean;
  version?: string;
  token: string;
  user: AuthUser;
};

export type ImageGalleryItem = {
  id: string;
  name: string;
  folder?: string;
  userId?: string;
  userLabel?: string;
  url: string;
  thumbUrl?: string;
  size: number;
  width?: number;
  height?: number;
  createdAt: string;
  prompt?: string;
  conversationId?: string;
  turnId?: string;
};

export type AppUserItem = {
  id: string;
  username: string;
  email?: string;
  name?: string;
  role: string;
  imageApiKey?: string;
  disabled?: boolean;
  createdAt?: string;
  lastUsedAt?: string;
  quota?: {
    freeUsed: number;
    freeLimit: number;
    freeRemaining: number;
    paidUsed: number;
    paidLimit: number;
    paidRemaining: number;
  };
};

export type InviteItem = {
  code: string;
  createdBy?: string;
  createdAt?: string;
  usedByUserId?: string;
  usedByUsername?: string;
  usedByDisplayName?: string;
  usedAt?: string;
};

export async function login(authKey: string): Promise<AuthResponse>;
export async function login(username: string, password: string): Promise<AuthResponse>;
export async function login(usernameOrKey: string, password?: string) {
  const first = String(usernameOrKey || "").trim();
  if (typeof password === "string") {
    return httpRequest<AuthResponse>("/auth/login", {
      method: "POST",
      body: { username: first, password },
      redirectOnUnauthorized: false,
    });
  }
  return httpRequest<AuthResponse>("/auth/login", {
    method: "POST",
    body: {},
    headers: {
      Authorization: `Bearer ${first}`,
    },
    redirectOnUnauthorized: false,
  });
}

export async function registerUser(payload: { username: string; password: string; inviteCode: string; name?: string }) {
  return httpRequest<AuthResponse>("/auth/register", {
    method: "POST",
    body: payload,
    redirectOnUnauthorized: false,
  });
}

export async function fetchInvites() {
  return httpRequest<{ items: InviteItem[] }>("/api/invites");
}

export async function createInvite() {
  return httpRequest<{ item: InviteItem }>("/api/invites", {
    method: "POST",
  });
}

export async function fetchImageQuota() {
  return httpRequest<{ item: DailyImageQuota }>("/api/image/quota");
}

export async function listImageGallery(params: { page?: number; pageSize?: number; q?: string; folder?: string; group?: "user" | "month" | "day" } = {}) {
  const searchParams = new URLSearchParams();
  if (params.page) searchParams.set("page", String(params.page));
  if (params.pageSize) searchParams.set("pageSize", String(params.pageSize));
  if (params.q?.trim()) searchParams.set("q", params.q.trim());
  if (params.folder?.trim()) searchParams.set("folder", params.folder.trim());
  if (params.group && params.group !== "user") searchParams.set("group", params.group);
  const query = searchParams.toString();
  return httpRequest<{ items: ImageGalleryItem[]; total: number; page: number; pageSize: number; folders?: { value: string; label: string }[] }>(`/api/image/gallery${query ? `?${query}` : ""}`);
}

export async function deleteImageGalleryItem(name: string) {
  return httpRequest<{ ok: boolean; name: string }>(`/api/image/gallery/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export async function deleteImageGalleryItems(names: string[]) {
  return httpRequest<{ ok: boolean; deleted: string[] }>("/api/image/gallery/delete", {
    method: "POST",
    body: { names },
  });
}

export async function fetchUsers() {
  return httpRequest<{ items: AppUserItem[] }>("/api/users");
}

export async function updateUserDisabled(id: string, disabled: boolean) {
  return httpRequest<{ ok: boolean }>(`/api/users/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: { disabled },
  });
}

export async function deleteUser(id: string) {
  return httpRequest<{ ok: boolean }>(`/api/users/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function adjustUserQuota(id: string, kind: "free" | "paid", delta: number) {
  return httpRequest<{ ok: boolean }>(`/api/users/${encodeURIComponent(id)}/quota`, {
    method: "POST",
    body: { kind, delta },
  });
}

export async function fetchAccounts() {
  return httpRequest<AccountListResponse>("/api/accounts");
}

export async function createAccounts(tokens: string[]) {
  return httpRequest<AccountMutationResponse>("/api/accounts", {
    method: "POST",
    body: { tokens },
  });
}

export async function importAccountFiles(files: File[]) {
  const formData = new FormData();
  files.forEach((file) => formData.append("file", file));
  return httpRequest<AccountImportResponse>("/api/accounts/import", {
    method: "POST",
    body: formData,
  });
}

export async function deleteAccounts(tokens: string[]) {
  return httpRequest<AccountMutationResponse>("/api/accounts", {
    method: "DELETE",
    body: { tokens },
  });
}

export async function refreshAccounts(accessTokens: string[]) {
  return httpRequest<AccountRefreshResponse>("/api/accounts/refresh", {
    method: "POST",
    body: { access_tokens: accessTokens },
  });
}

export async function refreshAllAccounts(options: { scope?: "all" | "routing_groups" } = {}) {
  const scope = options.scope ?? "all";
  const policy = scope === "routing_groups" ? await getImageAccountPolicyForRequest() : null;
  const policyHeader = policy ? buildImageAccountPolicyHeader(policy) : "";
  return httpRequest<AccountRefreshAllResponse>("/api/accounts/refresh-all", {
    method: "POST",
    body: { scope },
    headers: policyHeader ? { "X-Studio-Account-Policy": policyHeader } : undefined,
  });
}

export async function stopAccountRefresh() {
  return httpRequest<AccountRefreshAllResponse>("/api/accounts/refresh-stop", {
    method: "POST",
    body: {},
  });
}

export async function fetchAccountRefreshProgress() {
  return httpRequest<AccountRefreshAllResponse>(
    "/api/accounts/refresh-progress",
  );
}

export async function updateAccount(
  accessToken: string,
  updates: {
    type?: AccountType;
    status?: AccountStatus;
    quota?: number;
    note?: string;
  },
) {
  return httpRequest<AccountUpdateResponse>("/api/accounts/update", {
    method: "POST",
    body: {
      access_token: accessToken,
      ...updates,
    },
  });
}

export async function fetchAccountQuota(
  accountId: string,
  options: { refresh?: boolean } = {},
) {
  const refresh = options.refresh ?? true;
  const suffix = refresh ? "" : "?refresh=false";
  return httpRequest<AccountQuotaResponse>(
    `/api/accounts/${encodeURIComponent(accountId)}/quota${suffix}`,
  );
}

export type AccountImageTestResult = {
  ok: boolean;
  accountId: string;
  accountEmail: string;
  route: string;
  model: string;
  prompt: string;
  requestBody?: string;
  responseStatus?: number;
  responseBody?: string;
  imageUrl?: string;
  error?: string;
  latencyMs: number;
};

export type ImageSystemHintConfig = {
  imageCommonSystemHint: string;
  imagePrivateSystemHint: string;
  imageSystemHint: string;
};

export async function fetchImageSystemHint() {
  return httpRequest<ImageSystemHintConfig>("/api/config/image-system-hint");
}

export async function updateImageSystemHint(
  hint:
    | string
    | {
        imageCommonSystemHint?: string;
        imagePrivateSystemHint?: string;
      },
) {
  const body =
    typeof hint === "string"
      ? { imageSystemHint: hint }
      : {
          imageCommonSystemHint: hint.imageCommonSystemHint || "",
          imagePrivateSystemHint: hint.imagePrivateSystemHint || "",
        };
  return httpRequest<ImageSystemHintConfig>("/api/config/image-system-hint", {
    method: "PUT",
    body,
  });
}

export async function testAccountImage(accountId: string, route?: string) {
  return httpRequest<AccountImageTestResult>(
    `/api/accounts/${encodeURIComponent(accountId)}/image-test`,
    { method: "POST", body: { route: route || "" } },
  );
}

export async function fetchSyncStatus(
  source: SyncSource = "cpa",
  options: { progressOnly?: boolean } = {},
) {
  const params = new URLSearchParams({ source });
  if (options.progressOnly) {
    params.set("progress_only", "1");
  }
  return httpRequest<SyncStatusResponse>(
    `/api/sync/status?${params.toString()}`,
  );
}

export async function fetchConfig() {
  const config = await httpRequest<ConfigPayload>("/api/config");
  setCachedConfig(config);
  return config;
}

export async function testProxy(url?: string) {
  return httpRequest<ProxyTestResult>("/api/proxy/test", {
    method: "POST",
    body: { url: url ?? "" },
  });
}

export async function testIntegration(
  source: "cpa" | "newapi" | "sub2api",
  payload: {
    cpa?: ConfigPayload["cpa"];
    newapi?: ConfigPayload["newapi"];
    sub2api?: ConfigPayload["sub2api"];
  },
) {
  return httpRequest<IntegrationTestResult>("/api/integration/test", {
    method: "POST",
    body: {
      source,
      cpa: payload.cpa,
      newapi: payload.newapi,
      sub2api: payload.sub2api,
    },
  });
}

export async function discoverNewAPIToken(newapi: ConfigPayload["newapi"]) {
  return httpRequest<NewAPITokenDiscoverResult>(
    "/api/integration/newapi/token",
    {
      method: "POST",
      body: { newapi },
    },
  );
}

export async function fetchSub2APIGroups(sub2api: ConfigPayload["sub2api"]) {
  return httpRequest<Sub2APIGroupsResult>("/api/integration/sub2api/groups", {
    method: "POST",
    body: { sub2api },
  });
}

export async function fetchDefaultConfig() {
  return httpRequest<ConfigPayload>("/api/config/defaults");
}

export async function updateConfig(config: ConfigPayload) {
  const result = await httpRequest<{ status: string; config: ConfigPayload }>("/api/config", {
    method: "PUT",
    body: config,
  });
  setCachedConfig(result.config);
  return result;
}

export async function fetchRequestLogs(params: { page?: number; pageSize?: number; user?: string; account?: string; prompt?: string } = {}) {
  const searchParams = new URLSearchParams();
  if (params.page) searchParams.set("page", String(params.page));
  if (params.pageSize) searchParams.set("pageSize", String(params.pageSize));
  if (params.user?.trim()) searchParams.set("user", params.user.trim());
  if (params.account?.trim()) searchParams.set("account", params.account.trim());
  if (params.prompt?.trim()) searchParams.set("prompt", params.prompt.trim());
  const query = searchParams.toString();
  return httpRequest<{ items: RequestLogItem[]; total: number; page: number; pageSize: number }>(`/api/requests${query ? `?${query}` : ""}`);
}

export async function fetchRequestLogFilters() {
  return httpRequest<RequestLogFilterOptions>("/api/requests/filters");
}

export async function fetchRequestLogDetail(id: string) {
  return httpRequest<RequestLogDetail>(`/api/requests/${encodeURIComponent(id)}`);
}

export async function deleteRequestLogs(ids: string[]) {
  return httpRequest<{ ok: boolean; deleted: string[] }>("/api/requests/delete", {
    method: "POST",
    body: { ids },
  });
}

export async function deleteFailedRequestLogs() {
  return httpRequest<{ ok: boolean; deletedCount: number }>("/api/requests/delete-failed", {
    method: "POST",
  });
}

export async function fetchVersionInfo() {
  return httpRequest<VersionInfo>("/version", {
    redirectOnUnauthorized: false,
  });
}

export async function fetchStartupCheck() {
  return httpRequest<StartupCheckResponse>("/api/startup/check");
}

export async function fetchRuntimeStatus() {
  return httpRequest<RuntimeStatusResponse>("/api/runtime/status");
}

export async function downloadDiagnosticsExport() {
  const authKey = await getStoredAuthKey();
  const response = await fetch(
    `${webConfig.apiUrl.replace(/\/$/, "")}/api/diagnostics/export`,
    {
      method: "GET",
      headers: authKey ? { Authorization: `Bearer ${authKey}` } : {},
    },
  );
  if (!response.ok) {
    let message = `download failed (${response.status})`;
    try {
      const payload = (await response.json()) as {
        error?: string;
        message?: string;
        detail?: { message?: string };
      };
      message =
        payload?.detail?.message || payload?.message || payload?.error || message;
    } catch {
      // ignore json parse errors
    }
    throw new Error(message);
  }
  const blob = await response.blob();
  const disposition = response.headers.get("content-disposition") || "";
  const match = disposition.match(/filename="([^"]+)"/i);
  const fileName =
    match?.[1] || `chatgpt-image-studio-diagnostics-${Date.now()}.json`;
  return { blob, fileName };
}

export async function runSync(
  direction: "pull" | "push",
  source: SyncSource = "cpa",
) {
  return httpRequest<{ result: SyncRunResult; status?: SyncStatusResponse }>(
    "/api/sync/run",
    {
      method: "POST",
      body: { direction, source },
    },
  );
}

export async function generateImage(
  prompt: string,
  model: ImageModel = "gpt-image-2",
  count = 1,
) {
  return generateImageWithOptions(prompt, { model, count });
}

export async function generateImageWithOptions(
  prompt: string,
  options: {
    model?: ImageModel;
    count?: number;
    size?: string;
    quality?: ImageQuality;
  } = {},
) {
  const { model = "gpt-image-2", count = 1, size, quality = "high" } = options;
  const [policy, responseFormat] = await Promise.all([
    getImageAccountPolicyForRequest(),
    getImageResponseFormatForRequest(),
  ]);
  const policyHeader = buildImageAccountPolicyHeader(policy);
  const normalizedCount = Math.max(1, count);
  return httpRequest<ImageResponse>("/v1/images/generations", {
    method: "POST",
    headers: policyHeader
      ? { "X-Studio-Account-Policy": policyHeader }
      : undefined,
    body: {
      prompt,
      model,
      n: normalizedCount,
      size: size?.trim() || undefined,
      quality,
      response_format: responseFormat,
    },
  });
}

export async function createImageTask(payload: {
  taskId?: string;
  conversationId: string;
  turnId: string;
  mode: "generate" | "edit";
  prompt: string;
  model?: ImageModel;
  count?: number;
  retryImageIndex?: number;
  size?: string;
  resolutionAccess?: ImageResolutionAccess;
  quality?: ImageQuality;
  sourceImages?: Array<{
    id: string;
    role: "image" | "mask";
    name: string;
    dataUrl?: string;
    url?: string;
  }>;
  referenceImages?: ImageReferenceImage[];
  sourceReference?: InpaintSourceReference;
  contextReference?: ImageContextReference;
  conversationContext?: string;
  policy?: StoredImageAccountPolicy;
  privatePhotoMode?: boolean;
  systemHint?: string;
}) {
  const policy = payload.policy ?? (await getImageAccountPolicyForRequest());
  return httpRequest<ImageTaskResponse>("/api/image/tasks", {
    method: "POST",
    body: {
      taskId: payload.taskId?.trim() || undefined,
      conversationId: payload.conversationId,
      turnId: payload.turnId,
      mode: payload.mode,
      prompt: payload.prompt,
      model: payload.model ?? "gpt-image-2",
      count: Math.max(1, payload.count ?? 1),
      retryImageIndex:
        typeof payload.retryImageIndex === "number"
          ? payload.retryImageIndex
          : undefined,
      size: payload.size?.trim() || undefined,
      resolutionAccess: payload.resolutionAccess,
      quality: payload.quality,
      sourceImages: payload.sourceImages ?? [],
      referenceImages: payload.referenceImages ?? [],
      sourceReference: payload.sourceReference,
      contextReference: payload.contextReference,
      conversationContext: payload.conversationContext?.trim() || undefined,
      policy: normalizeImageAccountPolicy(policy),
      privatePhotoMode: payload.privatePhotoMode || undefined,
      systemHint: payload.systemHint || undefined,
    },
  });
}

export async function listImageTasks() {
  return httpRequest<ImageTaskListResponse>("/api/image/tasks");
}

export async function cancelImageTask(taskId: string) {
  return httpRequest<ImageTaskResponse>(
    `/api/image/tasks/${encodeURIComponent(taskId)}`,
    {
      method: "DELETE",
    },
  );
}

export async function consumeImageTaskStream(
  handlers: {
    onInit: (payload: { items: ImageTaskView[]; snapshot: ImageTaskSnapshot }) => void;
    onEvent: (event: ImageTaskStreamEvent) => void;
  },
  signal: AbortSignal,
) {
  const authKey = await getStoredAuthKey();
  const response = await fetch(
    `${webConfig.apiUrl.replace(/\/$/, "")}/api/image/tasks/stream`,
    {
      method: "GET",
      headers: authKey ? { Authorization: `Bearer ${authKey}` } : {},
      signal,
    },
  );
  if (!response.ok || !response.body) {
    throw new Error(`task stream failed (${response.status})`);
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let eventType = "message";
  let dataLines: string[] = [];

  const flushEvent = () => {
    if (dataLines.length === 0) {
      eventType = "message";
      return;
    }
    const raw = dataLines.join("\n");
    dataLines = [];
    try {
      if (eventType === "init") {
        handlers.onInit(JSON.parse(raw) as { items: ImageTaskView[]; snapshot: ImageTaskSnapshot });
      } else {
        handlers.onEvent(JSON.parse(raw) as ImageTaskStreamEvent);
      }
    } finally {
      eventType = "message";
    }
  };

  const STREAM_READ_TIMEOUT_MS = 30_000;

  while (true) {
    const readPromise = reader.read();
    const timeoutPromise = new Promise<{ value: undefined; done: true }>((_, reject) => {
      const id = setTimeout(() => reject(new Error("stream read timeout")), STREAM_READ_TIMEOUT_MS);
      readPromise.then(() => clearTimeout(id), () => clearTimeout(id));
    });
    const { value, done } = await Promise.race([readPromise, timeoutPromise]);
    if (done) {
      flushEvent();
      return;
    }
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split(/\r?\n/);
    buffer = lines.pop() ?? "";

    for (const line of lines) {
      if (!line) {
        flushEvent();
        continue;
      }
      if (line.startsWith("event:")) {
        eventType = line.slice(6).trim() || "message";
        continue;
      }
      if (line.startsWith("data:")) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
  }
}

export async function editImage({
  prompt,
  images,
  mask,
  sourceReference,
  size,
  quality,
  model = "gpt-image-2",
}: {
  prompt: string;
  images: File[];
  mask?: File | null;
  sourceReference?: InpaintSourceReference;
  size?: string;
  quality?: ImageQuality;
  model?: ImageModel;
}) {
  const formData = new FormData();
  const [policy, responseFormat] = await Promise.all([
    getImageAccountPolicyForRequest(),
    getImageResponseFormatForRequest(),
  ]);
  const policyHeader = buildImageAccountPolicyHeader(policy);
  formData.append("prompt", prompt);
  formData.append("model", model);
  formData.append("response_format", responseFormat);
  if (size?.trim()) {
    formData.append("size", size.trim());
  }
  if (quality) {
    formData.append("quality", quality);
  }
  images.forEach((file) => formData.append("image", file));
  if (mask) {
    formData.append("mask", mask);
  }
  if (sourceReference) {
    formData.append("original_file_id", sourceReference.original_file_id);
    formData.append("original_gen_id", sourceReference.original_gen_id);
    formData.append("source_account_id", sourceReference.source_account_id);
    if (sourceReference.conversation_id) {
      formData.append("conversation_id", sourceReference.conversation_id);
    }
    if (sourceReference.parent_message_id) {
      formData.append("parent_message_id", sourceReference.parent_message_id);
    }
    if (sourceReference.response_id) {
      formData.append("response_id", sourceReference.response_id);
    }
  }
  return httpRequest<ImageResponse>("/v1/images/edits", {
    method: "POST",
    headers: policyHeader
      ? { "X-Studio-Account-Policy": policyHeader }
      : undefined,
    body: formData,
  });
}

// Announcement

export type AnnouncementData = {
  active: boolean;
  content?: string;
  expiresAt?: string;
  createdAt?: string;
};

export async function fetchAnnouncement() {
  return httpRequest<AnnouncementData>("/api/announcement", { redirectOnUnauthorized: false });
}

export async function setAnnouncement(content: string, expiresAt: string) {
  return httpRequest<{ ok: boolean }>("/api/announcement", {
    method: "PUT",
    body: { content, expiresAt },
  });
}

export async function deleteAnnouncement() {
  return httpRequest<{ ok: boolean }>("/api/announcement", { method: "DELETE" });
}
