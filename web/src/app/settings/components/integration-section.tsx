"use client";

import { useMemo, useState } from "react";
import { Link2, LoaderCircle, ListTree } from "lucide-react";
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
  fetchSub2APIGroups,
  testExternalResponsesReference,
  testIntegration,
  type ConfigPayload,
  type IntegrationTestResult,
  type Sub2APIGroupOption,
} from "@/lib/api";

import { ConfigSection, Field, ToggleField, TooltipDetails, type SetConfigSection } from "./shared";

type IntegrationSectionProps = {
  config: ConfigPayload;
  setSection: SetConfigSection;
};

function buildIntegrationToastMessage(result: IntegrationTestResult) {
  const segments = [result.message];
  if (result.status > 0) {
    segments.push(`HTTP ${result.status}`);
  }
  if (result.latency >= 0) {
    segments.push(`${result.latency} ms`);
  }
  return segments.filter(Boolean).join(" / ");
}

function normalizeSub2APIGroupStatus(status: string) {
  const normalized = String(status || "").trim().toLowerCase();
  if (normalized === "active") {
    return "启用";
  }
  if (normalized === "inactive") {
    return "停用";
  }
  return String(status || "").trim();
}

function buildSub2APIGroupLabel(group: Sub2APIGroupOption) {
  const parts = [group.name || `分组 ${group.id}`];
  if (group.platform) {
    parts.push(group.platform);
  }
  const statusLabel = normalizeSub2APIGroupStatus(group.status);
  if (statusLabel) {
    parts.push(statusLabel);
  }
  parts.push(`ID ${group.id}`);
  return parts.join(" · ");
}

export function IntegrationSection({ config, setSection }: IntegrationSectionProps) {
  const [testingSource, setTestingSource] = useState<"newapi" | "sub2api" | "externalResponsesReference" | null>(null);
  const [isLoadingSub2APIGroups, setIsLoadingSub2APIGroups] = useState(false);
  const [sub2apiGroups, setSub2apiGroups] = useState<Sub2APIGroupOption[]>([]);
  const usesSQLiteStorage = config.storage.backend === "sqlite";
  const usesRedisAccountStorage = config.storage.backend === "redis";
  const usesRedisConfigStorage = config.storage.configBackend === "redis";
  const shouldShowRedisFields = usesRedisAccountStorage || usesRedisConfigStorage;
  const imageConversationStorage =
    config.storage.imageConversationStorage === "server" ? "server" : "browser";
  const imageDataStorage =
    config.storage.imageDataStorage === "server" ? "server" : "browser";
  const externalResponsesProviders = config.externalResponses.providers?.length
    ? config.externalResponses.providers
    : [
        {
          id: "default",
          name: "Default",
          enabled: config.externalResponses.enabled,
          baseUrl: config.externalResponses.baseUrl,
          apiKey: config.externalResponses.apiKey,
          model: config.externalResponses.model,
          requestTimeout: config.externalResponses.requestTimeout,
        },
      ];
  const externalResponsesDuplicateIDs = new Set(
    externalResponsesProviders
      .map((provider) => provider.id.trim())
      .filter((id, index, ids) => id && ids.indexOf(id) !== index),
  );
  const serverConversationStorageLabel =
    config.storage.backend === "sqlite"
      ? "SQLite 数据库"
      : config.storage.backend === "redis"
        ? "Redis"
        : "程序目录";
  const serverConversationStorageHint =
    config.storage.backend === "sqlite"
      ? "服务器侧会话记录会写入当前 SQLite 数据库文件。"
      : config.storage.backend === "redis"
        ? "服务器侧会话记录会写入当前 Redis。"
        : "服务器侧会话记录会写入当前程序目录下的数据目录。";

  const sub2apiGroupHint = useMemo(() => {
    if (sub2apiGroups.length === 0) {
      return "先点击“拉取分组”，再从下拉框里选择目标分组。";
    }
    return `已拉取 ${sub2apiGroups.length} 个分组；可直接从下拉框选择，也可选“不限制分组”。`;
  }, [sub2apiGroups.length]);

  const sub2apiGroupOptions = useMemo(() => {
    const items = [...sub2apiGroups];
    const currentGroupId = String(config.sub2api.groupId || "").trim();
    if (currentGroupId && !items.some((group) => group.id === currentGroupId)) {
      items.unshift({
        id: currentGroupId,
        name: "当前已保存分组",
        description: "",
        platform: "",
        status: "",
      });
    }
    return items;
  }, [config.sub2api.groupId, sub2apiGroups]);

  const handleTestNewAPI = async () => {
    setTestingSource("newapi");
    try {
      const result = await testIntegration("newapi", { newapi: config.newapi });
      if (result.ok) {
        if (result.userId && result.userId > 0 && result.userId !== config.newapi.userId) {
          setSection("newapi", {
            ...config.newapi,
            userId: result.userId,
          });
        }
        toast.success(buildIntegrationToastMessage(result));
      } else {
        toast.error(buildIntegrationToastMessage(result));
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "NewAPI 测试失败");
    } finally {
      setTestingSource(null);
    }
  };

  const handleTestSub2API = async () => {
    setTestingSource("sub2api");
    try {
      const result = await testIntegration("sub2api", { sub2api: config.sub2api });
      if (result.ok) {
        toast.success(buildIntegrationToastMessage(result));
      } else {
        toast.error(buildIntegrationToastMessage(result));
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Sub2API 测试失败");
    } finally {
      setTestingSource(null);
    }
  };

  const handleTestExternalResponsesReference = async () => {
    setTestingSource("externalResponsesReference");
    try {
      const result = await testExternalResponsesReference(config.externalResponses);
      const message = [result.message, result.latency >= 0 ? `${result.latency} ms` : ""]
        .filter(Boolean)
        .join(" / ");
      if (result.ok) {
        toast.success(message);
      } else {
        toast.error(message || "图片引用测试失败");
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "图片引用测试失败");
    } finally {
      setTestingSource(null);
    }
  };

  const handleLoadSub2APIGroups = async () => {
    setIsLoadingSub2APIGroups(true);
    try {
      const result = await fetchSub2APIGroups(config.sub2api);
      setSub2apiGroups(result.groups || []);
      toast.success(`${result.message} / ${result.latency} ms`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Sub2API 分组拉取失败");
    } finally {
      setIsLoadingSub2APIGroups(false);
    }
  };

  return (
    <>
      <ConfigSection
        title="NewAPI 接入"
        description="账号管理页里的 NewAPI 来源和推送会走这里。推荐顺序是先填写“地址 + 用户名 + 密码”完成测试连接，再按需补“系统访问令牌 + 用户 ID”；Session Cookie 只保留给高级兼容场景。"
        actions={
          <Button
            type="button"
            variant="outline"
            className="h-10 rounded-full border-stone-200 bg-white px-4 text-stone-700 shadow-none"
            onClick={() => void handleTestNewAPI()}
            disabled={testingSource === "newapi"}
          >
            {testingSource === "newapi" ? <LoaderCircle className="size-4 animate-spin" /> : <Link2 className="size-4" />}
            测试连接
          </Button>
        }
      >
        <Field
          label="NewAPI 地址"
          hint="填写你的 NewAPI 站点根地址，例如 http://127.0.0.1:3000。"
        >
          <Input
            value={config.newapi.baseUrl}
            onChange={(event) => setSection("newapi", { ...config.newapi, baseUrl: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field label="NewAPI 超时（秒）" hint="配置页测试连接和账号同步时使用。">
          <Input
            type="number"
            value={String(config.newapi.requestTimeout)}
            onChange={(event) =>
              setSection("newapi", {
                ...config.newapi,
                requestTimeout: Number(event.target.value || 0),
              })
            }
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="用户名"
          hint="对应 NewAPI 登录页里的“用户名”。页面入口：站点登录页。"
        >
          <Input
            value={config.newapi.username}
            onChange={(event) => setSection("newapi", { ...config.newapi, username: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="密码"
          hint="对应 NewAPI 登录页里的“密码”。页面入口：站点登录页。"
        >
          <Input
            type="password"
            value={config.newapi.password}
            onChange={(event) => setSection("newapi", { ...config.newapi, password: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="系统访问令牌"
          hint="对应 NewAPI 个人设置里的“系统访问令牌”。页面路径：右上角头像 -> 个人设置 -> 安全设置 -> 系统访问令牌。"
          tooltip={
            <TooltipDetails
              items={[
                {
                  title: "在哪里看",
                  body: <>在 NewAPI 页面右上角点头像，进入“个人设置”，再切到“安全设置”，就能看到“系统访问令牌”卡片。</>,
                },
                {
                  title: "注意",
                  body: <>这里建议填写你已经确认好的现成值；如果你在 NewAPI 页面里点了“重新生成”，旧令牌会失效。</>,
                },
              ]}
            />
          }
        >
          <Input
            type="password"
            value={config.newapi.accessToken}
            onChange={(event) => setSection("newapi", { ...config.newapi, accessToken: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="用户 ID"
          hint="测试连接成功后会自动回填；如果你已经知道自己的用户 ID，也可以直接填写。"
        >
          <Input
            type="number"
            value={String(config.newapi.userId)}
            onChange={(event) =>
              setSection("newapi", {
                ...config.newapi,
                userId: Number(event.target.value || 0),
              })
            }
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="Session Cookie（高级）"
          hint="高级备用项，不对应 NewAPI 页面里的常规表单。通常优先使用“用户名/密码”或“系统访问令牌/用户 ID”。"
          fullWidth
        >
          <Input
            value={config.newapi.sessionCookie}
            onChange={(event) => setSection("newapi", { ...config.newapi, sessionCookie: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>
      </ConfigSection>

      <ConfigSection
        title="外部 Responses"
        description="图片 responses 链路统一请求外部 /v1/responses provider；多个 provider 会在后台随机起点轮换重试，不再回退账号内置 responses。"
        actions={
          <Button
            type="button"
            variant="outline"
            className="h-10 rounded-full border-stone-200 bg-white px-4 text-stone-700 shadow-none"
            onClick={() => void handleTestExternalResponsesReference()}
            disabled={testingSource === "externalResponsesReference"}
          >
            {testingSource === "externalResponsesReference" ? <LoaderCircle className="size-4 animate-spin" /> : <Link2 className="size-4" />}
            检测图片引用
          </Button>
        }
      >
        <ToggleField
          label="启用外部 Responses"
          hint="paid/auto 图片链路依赖这里；未配置可用 provider 时会直接报错。"
          checked={config.externalResponses.enabled}
          onCheckedChange={(checked) =>
            setSection("externalResponses", {
              ...config.externalResponses,
              enabled: checked,
            })
          }
        />

        <Field label="重试轮数" hint="每轮会按 provider 列表顺序尝试一遍；任务首次 provider 起点会随机选择。">
          <Input
            type="number"
            value={String(config.externalResponses.retryTimes || 3)}
            onChange={(event) =>
              setSection("externalResponses", {
                ...config.externalResponses,
                retryTimes: Number(event.target.value || 0),
              })
            }
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field label="参考图引用方式" hint="url 适合上游可访问本服务图片地址的场景；base64 会直接传 data:image/jpeg;base64,...。">
          <Select
            value={config.externalResponses.referenceImageMode || "url"}
            onValueChange={(value) =>
              setSection("externalResponses", {
                ...config.externalResponses,
                referenceImageMode: value,
              })
            }
          >
            <SelectTrigger className="h-11 rounded-2xl border-stone-200 bg-white shadow-none">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="url">URL 引用</SelectItem>
              <SelectItem value="base64">Base64 data URL</SelectItem>
            </SelectContent>
          </Select>
        </Field>

        <Field
          label="参考图 URL 根地址"
          hint="仅 url 方式使用。最终会拼成：这里的地址 + /v1/files/image/...；代理可填 https://proxy.example/http://origin.example。"
          fullWidth
        >
          <Input
            value={config.externalResponses.referenceImageBaseUrl || ""}
            onChange={(event) =>
              setSection("externalResponses", {
                ...config.externalResponses,
                referenceImageBaseUrl: event.target.value,
              })
            }
            placeholder="https://proxy.example/http://origin.example"
            disabled={(config.externalResponses.referenceImageMode || "url") === "base64"}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <div className="col-span-full space-y-3">
          {externalResponsesProviders.map((provider, index) => (
            <div key={`${provider.id || "provider"}-${index}`} className="rounded-3xl border border-stone-200 bg-stone-50/60 p-4">
              <div className="mb-3 flex items-center justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-stone-900">Provider {index + 1}</div>
                  <div className="text-xs text-stone-500">ID 用于隔离 response 上下文，保存后不要随意改名。</div>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  className="h-9 rounded-full border-stone-200 bg-white px-3 text-xs text-stone-600 shadow-none"
                  onClick={() => {
                    const providers = [...externalResponsesProviders];
                    providers.splice(index, 1);
                    setSection("externalResponses", { ...config.externalResponses, providers });
                  }}
                >
                  删除
                </Button>
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                <ToggleField
                  label="启用"
                  hint="关闭后不会参与轮换。"
                  checked={provider.enabled}
                  onCheckedChange={(checked) => {
                    const providers = [...externalResponsesProviders];
                    providers[index] = { ...provider, enabled: checked };
                    setSection("externalResponses", { ...config.externalResponses, providers });
                  }}
                />
                <Field
                  label="Provider ID"
                  hint={externalResponsesDuplicateIDs.has(provider.id.trim()) ? "Provider ID 重复，请改成唯一值。" : "稳定唯一 ID，例如 primary、backup-us。"}
                >
                  <Input
                    value={provider.id}
                    onChange={(event) => {
                      const providers = [...externalResponsesProviders];
                      providers[index] = { ...provider, id: event.target.value };
                      setSection("externalResponses", { ...config.externalResponses, providers });
                    }}
                    className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
                  />
                </Field>
                <Field label="显示名称" hint="日志和界面展示用，可选。">
                  <Input
                    value={provider.name}
                    onChange={(event) => {
                      const providers = [...externalResponsesProviders];
                      providers[index] = { ...provider, name: event.target.value };
                      setSection("externalResponses", { ...config.externalResponses, providers });
                    }}
                    className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
                  />
                </Field>
                <Field label="API 地址" hint="支持 OpenAI-compatible /v1/responses 的服务根地址。">
                  <Input
                    value={provider.baseUrl}
                    onChange={(event) => {
                      const providers = [...externalResponsesProviders];
                      providers[index] = { ...provider, baseUrl: event.target.value };
                      setSection("externalResponses", { ...config.externalResponses, providers });
                    }}
                    className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
                  />
                </Field>
                <Field label="API Key" hint="请求该 provider 时作为 Authorization: Bearer 使用。">
                  <Input
                    type="password"
                    value={provider.apiKey}
                    onChange={(event) => {
                      const providers = [...externalResponsesProviders];
                      providers[index] = { ...provider, apiKey: event.target.value };
                      setSection("externalResponses", { ...config.externalResponses, providers });
                    }}
                    className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
                  />
                </Field>
                <Field label="Responses 模型" hint="该 provider /v1/responses payload 中的 model。">
                  <Input
                    value={provider.model}
                    onChange={(event) => {
                      const providers = [...externalResponsesProviders];
                      providers[index] = { ...provider, model: event.target.value };
                      setSection("externalResponses", { ...config.externalResponses, providers });
                    }}
                    className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
                  />
                </Field>
                <Field label="请求超时（秒）" hint="该 provider 的 /v1/responses 请求超时。">
                  <Input
                    type="number"
                    value={String(provider.requestTimeout || 300)}
                    onChange={(event) => {
                      const providers = [...externalResponsesProviders];
                      providers[index] = { ...provider, requestTimeout: Number(event.target.value || 0) };
                      setSection("externalResponses", { ...config.externalResponses, providers });
                    }}
                    className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
                  />
                </Field>
              </div>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            className="h-10 rounded-full border-stone-200 bg-white px-4 text-stone-700 shadow-none"
            onClick={() =>
              setSection("externalResponses", {
                ...config.externalResponses,
                providers: [
                  ...externalResponsesProviders,
                  {
                    id: `provider-${externalResponsesProviders.length + 1}`,
                    name: "",
                    enabled: true,
                    baseUrl: "",
                    apiKey: "",
                    model: "",
                    requestTimeout: config.externalResponses.requestTimeout || 300,
                  },
                ],
              })
            }
          >
            添加 Provider
          </Button>
        </div>
      </ConfigSection>

      <ConfigSection
        title="Sub2API 接入"
        description="用于 Sub2API 来源同步和推送。字段名称尽量和 Sub2API 页面里的叫法一致，同时在提示里给出对应页面路径。"
        actions={
          <>
            <Button
              type="button"
              variant="outline"
              className="h-10 rounded-full border-stone-200 bg-white px-4 text-stone-700 shadow-none"
              onClick={() => void handleTestSub2API()}
              disabled={testingSource === "sub2api"}
            >
              {testingSource === "sub2api" ? <LoaderCircle className="size-4 animate-spin" /> : <Link2 className="size-4" />}
              测试连接
            </Button>
            <Button
              type="button"
              variant="outline"
              className="h-10 rounded-full border-stone-200 bg-white px-4 text-stone-700 shadow-none"
              onClick={() => void handleLoadSub2APIGroups()}
              disabled={isLoadingSub2APIGroups}
            >
              {isLoadingSub2APIGroups ? <LoaderCircle className="size-4 animate-spin" /> : <ListTree className="size-4" />}
              拉取分组
            </Button>
          </>
        }
      >
        <Field
          label="Sub2API 地址"
          hint="填写你的 Sub2API 站点根地址，例如 http://127.0.0.1:8080。"
        >
          <Input
            value={config.sub2api.baseUrl}
            onChange={(event) => setSection("sub2api", { ...config.sub2api, baseUrl: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="邮箱"
          hint="对应 Sub2API 登录页里的“邮箱”。页面入口：站点登录页。"
        >
          <Input
            value={config.sub2api.email}
            onChange={(event) => setSection("sub2api", { ...config.sub2api, email: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="密码"
          hint="对应 Sub2API 登录页里的“密码”。页面入口：站点登录页。"
        >
          <Input
            type="password"
            value={config.sub2api.password}
            onChange={(event) => setSection("sub2api", { ...config.sub2api, password: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="API 密钥"
          hint="对应 Sub2API 用户侧的“API 密钥”页面。页面路径：左侧菜单 -> API 密钥。如果你使用邮箱密码登录，这里可以留空。"
        >
          <Input
            type="password"
            value={config.sub2api.apiKey}
            onChange={(event) => setSection("sub2api", { ...config.sub2api, apiKey: event.target.value })}
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>

        <Field
          label="分组"
          hint={sub2apiGroupHint}
          tooltip={
            <TooltipDetails
              items={[
                {
                  title: "不填写",
                  body: <>表示不限制拉取分组，推送时也不强制绑定指定分组。</>,
                },
                {
                  title: "页面路径",
                  body: <>对应 Sub2API 管理后台左侧菜单里的“分组”页面。</>,
                },
                {
                  title: "选择方式",
                  body: <>点击“拉取分组”后，会按名称、平台、状态和 ID 组合展示成下拉选项，避免只看数字难分辨。</>,
                },
              ]}
            />
          }
        >
          <Select
            value={config.sub2api.groupId ? config.sub2api.groupId : "__none__"}
            onValueChange={(value) =>
              setSection("sub2api", {
                ...config.sub2api,
                groupId: value === "__none__" ? "" : value,
              })
            }
            disabled={isLoadingSub2APIGroups || sub2apiGroupOptions.length === 0}
          >
            <SelectTrigger className="h-11 rounded-2xl border-stone-200 bg-white shadow-none">
              <SelectValue placeholder="先点击“拉取分组”获取可选项" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">不限制分组</SelectItem>
              {sub2apiGroupOptions.map((group) => (
                <SelectItem key={group.id} value={group.id}>
                  {buildSub2APIGroupLabel(group)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <Field label="Sub2API 超时（秒）" hint="配置页测试连接、拉取分组和账号同步时使用。">
          <Input
            type="number"
            value={String(config.sub2api.requestTimeout)}
            onChange={(event) =>
              setSection("sub2api", {
                ...config.sub2api,
                requestTimeout: Number(event.target.value || 0),
              })
            }
            className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
          />
        </Field>
      </ConfigSection>

      <ConfigSection
        title="数据存储"
        description="这里分别控制账号池、配置文件、图片会话记录和图片数据的实际落点。本地文件模式兼容旧版目录结构；服务器会话记录会自动配套使用服务器图片目录。"
      >
        <Field
          label="账号池存储后端"
          hint="控制账号池本体、认证文件、额度状态和同步状态写到本地文件、SQLite 数据库还是 Redis。"
          tooltip={
            <TooltipDetails
              items={[
                {
                  title: "本地文件",
                  body: <>沿用当前版本的 `auths/*.json`、`accounts_state.json`、`sync_state/*.json` 目录结构。</>,
                },
                {
                  title: "SQLite 数据库",
                  body: <>账号池相关数据统一写入 SQLite，适合单机但希望更集中管理的场景。</>,
                },
                {
                  title: "Redis",
                  body: <>账号池相关数据写入 Redis，适合无持久磁盘云容器或多实例共享状态。</>,
                },
              ]}
            />
          }
        >
          <Select
            value={config.storage.backend}
            onValueChange={(value) => setSection("storage", { ...config.storage, backend: value })}
          >
            <SelectTrigger className="h-11 rounded-2xl border-stone-200 bg-white shadow-none">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="current">本地文件</SelectItem>
              <SelectItem value="sqlite">SQLite 数据库</SelectItem>
              <SelectItem value="redis">Redis</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field label="配置文件存储" hint="决定配置管理页点击保存后，配置写到本地 config.toml 还是 Redis。">
          <Select
            value={config.storage.configBackend === "redis" ? "redis" : "file"}
            onValueChange={(value) => setSection("storage", { ...config.storage, configBackend: value })}
          >
            <SelectTrigger className="h-11 rounded-2xl border-stone-200 bg-white shadow-none">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="file">file</SelectItem>
              <SelectItem value="redis">redis</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field
          label="会话记录存储"
          hint={`切换后保存配置时会自动迁移现有图片会话记录；从${serverConversationStorageLabel}切回浏览器时，需要把历史图片下载回当前浏览器。${serverConversationStorageHint}`}
        >
          <Select
            value={imageConversationStorage}
            onValueChange={(value) =>
              setSection("storage", {
                ...config.storage,
                imageConversationStorage: value,
                imageDataStorage: value,
                imageStorage: value,
              })
            }
          >
            <SelectTrigger className="h-11 rounded-2xl border-stone-200 bg-white shadow-none">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="browser">浏览器存储</SelectItem>
              <SelectItem value="server">{serverConversationStorageLabel}</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field
          label="图片数据存储"
          hint={
            imageConversationStorage === "server"
              ? "当前会话记录已使用服务器侧存储，图片数据也必须写入本地/服务器目录；切换后保存配置时会自动迁移。"
              : "当前会话记录使用浏览器存储，图片数据会随会话一起保存在浏览器 local；切换后保存配置时会自动迁移。"
          }
        >
          <Select
            value={imageDataStorage}
            onValueChange={(value) =>
              setSection("storage", {
                ...config.storage,
                imageConversationStorage: value,
                imageDataStorage: value,
                imageStorage: value,
              })
            }
          >
            <SelectTrigger className="h-11 rounded-2xl border-stone-200 bg-white shadow-none">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="browser" disabled={imageConversationStorage === "server"}>浏览器 local</SelectItem>
              <SelectItem value="server" disabled={imageConversationStorage !== "server"}>本地/服务器目录</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        {usesSQLiteStorage ? (
          <Field label="SQLite 路径" hint="仅在账号池存储选择 SQLite 数据库时使用。">
            <Input
              value={config.storage.sqlitePath}
              onChange={(event) => setSection("storage", { ...config.storage, sqlitePath: event.target.value })}
              className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
            />
          </Field>
        ) : null}
        {shouldShowRedisFields ? (
          <>
            <Field
              label="Redis 地址"
              hint={
                usesRedisAccountStorage && usesRedisConfigStorage
                  ? "账号池和配置都会使用这组 Redis 连接。"
                  : usesRedisAccountStorage
                    ? "仅账号池和运行状态使用这组 Redis 连接。"
                    : "仅配置文件存储使用这组 Redis 连接。"
              }
            >
              <Input
                value={config.storage.redisAddr}
                onChange={(event) => setSection("storage", { ...config.storage, redisAddr: event.target.value })}
                className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
              />
            </Field>
            <Field label="Redis 密码" hint="Redis 无密码可留空。">
              <Input
                type="password"
                value={config.storage.redisPassword}
                onChange={(event) => setSection("storage", { ...config.storage, redisPassword: event.target.value })}
                className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
              />
            </Field>
            <Field label="Redis DB" hint="默认 0。">
              <Input
                type="number"
                value={String(config.storage.redisDb)}
                onChange={(event) =>
                  setSection("storage", {
                    ...config.storage,
                    redisDb: Number(event.target.value || 0),
                  })
                }
                className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
              />
            </Field>
            <Field label="Redis Key 前缀" hint="避免和其他业务共享 Redis 时键名冲突。">
              <Input
                value={config.storage.redisPrefix}
                onChange={(event) => setSection("storage", { ...config.storage, redisPrefix: event.target.value })}
                className="h-11 rounded-2xl border-stone-200 bg-white shadow-none"
              />
            </Field>
          </>
        ) : null}
      </ConfigSection>
    </>
  );
}
