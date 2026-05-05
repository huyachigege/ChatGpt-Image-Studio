"use client";

import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Activity, ChevronLeft, ImageIcon, Images, LogOut, PanelLeftClose, PanelLeftOpen, Settings2, Shield } from "lucide-react";

import webConfig from "@/constants/common-env";
import { fetchVersionInfo } from "@/lib/api";
import { clearStoredAuthKey, getStoredAuthKey, getStoredAuthUser, type AuthUser } from "@/store/auth";
import { cn } from "@/lib/utils";
import { ThemeToggleButton } from "@/components/theme-toggle-button";

const repositoryUrl = "https://github.com/huyachigege/ChatGpt-Image-Studio";

function formatVersionLabel(value: string) {
  const normalized = String(value || "").trim();
  if (!normalized) {
    return "读取中";
  }

  const semanticMatch = normalized.match(/v?(\d+\.\d+\.\d+)/i);
  if (semanticMatch?.[1]) {
    return `v${semanticMatch[1]}`;
  }

  return normalized;
}

const navItems = [
  { href: "/image/history", matchPrefix: "/image/history", label: "图片工作台", description: "生成与编辑", icon: ImageIcon },
  { href: "/image/gallery", matchPrefix: "/image/gallery", label: "历史图库", description: "查看、下载和删除出图", icon: Images },
  { href: "/accounts", matchPrefix: "/accounts", label: "账号管理", description: "号池、额度与同步", icon: Shield },
  { href: "/settings", matchPrefix: "/settings", label: "配置管理", description: "模式、接口与后端配置", icon: Settings2 },
  { href: "/requests", matchPrefix: "/requests", label: "调用请求", description: "查看官方与 CPA 请求方向", icon: Activity },
] as const;

function BrandCopy({ subtitle }: { subtitle: string }) {
  return (
    <span className="min-w-0">
      <span className="block truncate text-sm font-semibold tracking-tight text-stone-900 dark:text-[var(--studio-text-strong)]">
        ChatGpt Image Studio
      </span>
      <span className="block truncate text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">
        {subtitle}
      </span>
    </span>
  );
}

function isNavItemActive(pathname: string, href: string, matchPrefix?: string) {
  if (href === "/image/history") {
    return pathname === "/image/history" || pathname === "/image/workspace";
  }
  if (matchPrefix) {
    return pathname === matchPrefix || pathname.startsWith(`${matchPrefix}/`);
  }
  return pathname === href;
}

type DesktopTopNavProps = {
  pathname: string;
  defaultCollapsed: boolean;
  versionLabel: string;
  user: AuthUser | null;
  onLogout: () => Promise<void>;
};

function DesktopTopNav({ pathname, defaultCollapsed, versionLabel, user, onLogout }: DesktopTopNavProps) {
  const [collapsed, setCollapsed] = useState(defaultCollapsed);
  const visibleNavItems = user?.role === "admin" ? navItems : navItems.filter((item) => item.href.startsWith("/image"));
  const apiBaseUrl = webConfig.apiUrl || (typeof window !== "undefined" ? window.location.origin : "http://localhost:3000");
  const exampleToken = user?.imageApiKey ?? "";
  const requestExample = exampleToken
    ? `curl ${apiBaseUrl.replace(/\/$/, "")}/v1/images/generations \\\n  -H "Authorization: Bearer ${exampleToken}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"prompt":"一只坐在窗边的橘猫","size":"1024x1024"}'`
    : "";

  return (
    <aside className={cn("hidden shrink-0 transition-[width] duration-200 lg:flex", collapsed ? "w-[92px]" : "w-[228px]")}>
      <div className="flex h-full w-full flex-col rounded-[28px] border border-stone-200 bg-[#f0f0ed] p-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.65)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.03)]">
        <div className={cn("gap-2", collapsed ? "flex flex-col items-center" : "flex items-center justify-between")}>
          <div className={cn(collapsed ? "flex flex-col items-center gap-2" : "flex min-w-0 flex-1 items-center gap-3")}>
            <ThemeToggleButton
              className={cn(collapsed ? "size-11" : "size-10")}
              iconClassName={cn(collapsed ? "size-5" : "size-4")}
            />
            {!collapsed ? (
              <Link
                to="/image"
                className="min-w-0 flex-1 rounded-2xl px-2 py-2 transition hover:bg-white/70 dark:hover:bg-[var(--studio-panel-soft)]"
              >
                <BrandCopy subtitle="浅色 / 浅灰 / 深黑主题切换" />
              </Link>
            ) : null}
          </div>

          <button
            type="button"
            className={cn(
              "inline-flex items-center justify-center rounded-2xl border border-stone-200 bg-white text-stone-600 transition hover:bg-stone-50 hover:text-stone-900 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)] dark:hover:bg-[var(--studio-panel-muted)] dark:hover:text-[var(--studio-text-strong)]",
              collapsed ? "size-11" : "size-10",
            )}
            onClick={() => setCollapsed((current) => !current)}
            aria-label={collapsed ? "展开导航" : "收起导航"}
          >
            {collapsed ? <PanelLeftOpen className="size-5" /> : <PanelLeftClose className="size-4" />}
          </button>
        </div>

        <nav className="mt-4 space-y-1">
          {visibleNavItems.map((item) => {
            const active = isNavItemActive(pathname, item.href, item.matchPrefix);
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                to={item.href}
                className={cn(
                  "flex rounded-2xl transition",
                  collapsed ? "justify-center px-0 py-3.5" : "items-center gap-3 px-3 py-3",
                  active
                    ? "bg-white text-stone-950 shadow-sm dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-strong)]"
                    : "text-stone-600 hover:bg-white/65 hover:text-stone-900 dark:text-[var(--studio-text-muted)] dark:hover:bg-[var(--studio-panel-soft)] dark:hover:text-[var(--studio-text-strong)]",
                )}
                title={collapsed ? item.label : undefined}
              >
                <span
                  className={cn(
                    "flex items-center justify-center rounded-2xl",
                    collapsed ? "size-11" : "size-9",
                    active
                      ? "bg-stone-950 text-white dark:bg-[var(--studio-accent-strong)] dark:text-[var(--studio-accent-foreground)]"
                      : "bg-white/80 text-stone-600 dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]",
                  )}
                >
                  <Icon className={cn(collapsed ? "size-5" : "size-4")} />
                </span>
                {!collapsed ? (
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-medium">{item.label}</span>
                    <span className="block truncate text-xs text-stone-500 dark:text-[var(--studio-text-muted)]">{item.description}</span>
                  </span>
                ) : null}
              </Link>
            );
          })}
        </nav>

        <div className="mt-auto space-y-3">
          {requestExample && !collapsed ? (
            <div className="rounded-2xl bg-white/70 px-4 py-3 text-[11px] leading-5 text-stone-600 shadow-sm dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)]">
              <div className="font-medium text-stone-700 dark:text-[var(--studio-text)]">图片 API 请求示例</div>
              <pre className="mt-2 overflow-x-auto whitespace-pre-wrap break-all font-mono text-[10px] leading-5">{requestExample}</pre>
            </div>
          ) : null}
          <a
            href={repositoryUrl}
            target="_blank"
            rel="noreferrer"
            className={cn(
              "block rounded-2xl bg-white/70 text-xs text-stone-500 shadow-sm transition hover:bg-white hover:text-stone-700 dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-muted)] dark:hover:bg-[var(--studio-panel-muted)] dark:hover:text-[var(--studio-text)]",
              collapsed ? "px-2 py-3 text-center" : "px-4 py-3",
            )}
            title="打开 GitHub 仓库"
          >
            {!collapsed ? <div className="font-medium text-stone-700 dark:text-[var(--studio-text)]">版本</div> : null}
            <div className={cn(!collapsed ? "mt-1" : "font-medium")}>{versionLabel}</div>
          </a>
          <button
            type="button"
            className={cn(
              "flex w-full items-center rounded-2xl border border-stone-200 bg-white text-sm font-medium text-stone-700 transition hover:bg-stone-50 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)] dark:hover:bg-[var(--studio-panel-muted)]",
              collapsed ? "justify-center px-0 py-3" : "justify-center gap-2 px-4 py-3",
            )}
            onClick={() => void onLogout()}
            title={collapsed ? "退出登录" : undefined}
          >
            <LogOut className="size-4" />
            {!collapsed ? "退出登录" : null}
          </button>
        </div>
      </div>
    </aside>
  );
}

export function TopNav() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const isImageRoute = pathname === "/image" || pathname?.startsWith("/image/");
  const isMobileWorkspaceRoute = pathname === "/image/workspace";
  const [versionLabel, setVersionLabel] = useState("读取中");
  const [authUser, setAuthUser] = useState<AuthUser | null>(null);
  const [authKey, setAuthKey] = useState("");
  const [mobileNavExpanded, setMobileNavExpanded] = useState(false);
  const [mobileHeaderHeight, setMobileHeaderHeight] = useState(0);
  const [mobileWorkspaceHeaderHeight, setMobileWorkspaceHeaderHeight] = useState(0);
  const [mobileWorkspaceTitle, setMobileWorkspaceTitle] = useState<string | null>(null);
  const mobileHeaderRef = useRef<HTMLElement | null>(null);
  const mobileWorkspaceHeaderRef = useRef<HTMLDivElement | null>(null);
  const setMobileHeaderRef = (node: HTMLElement | null) => {
    mobileHeaderRef.current = node;
  };

  useEffect(() => {
    let cancelled = false;

    const loadAuthState = async () => {
      const [user, token] = await Promise.all([getStoredAuthUser(), getStoredAuthKey()]);
      if (!cancelled) {
        setAuthUser(user);
        setAuthKey(token);
      }
    };

    void loadAuthState();

    const loadVersion = async () => {
      try {
        const payload = await fetchVersionInfo();
        if (!cancelled) {
          setVersionLabel(formatVersionLabel(payload.version));
        }
      } catch {
        if (!cancelled) {
          setVersionLabel("未知版本");
        }
      }
    };

    void loadVersion();
    return () => {
      cancelled = true;
    };
  }, [pathname]);

  useEffect(() => {
    setMobileNavExpanded(false);
  }, [pathname]);

  useEffect(() => {
    const element = mobileHeaderRef.current;
    if (!element) {
      return;
    }

    const updateHeight = () => {
      setMobileHeaderHeight(element.offsetHeight);
    };

    updateHeight();
    const observer = new ResizeObserver(() => updateHeight());
    observer.observe(element);
    window.addEventListener("resize", updateHeight);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", updateHeight);
    };
  }, [mobileNavExpanded]);

  useEffect(() => {
    if (!isMobileWorkspaceRoute) {
      setMobileWorkspaceHeaderHeight(0);
      return;
    }

    const element = mobileWorkspaceHeaderRef.current;
    if (!element) {
      return;
    }

    const updateHeight = () => {
      setMobileWorkspaceHeaderHeight(element.offsetHeight);
    };

    updateHeight();
    const observer = new ResizeObserver(() => updateHeight());
    observer.observe(element);
    window.addEventListener("resize", updateHeight);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", updateHeight);
    };
  }, [isMobileWorkspaceRoute, mobileWorkspaceTitle]);

  useEffect(() => {
    if (!isImageRoute) {
      setMobileWorkspaceTitle(null);
      return;
    }

    const handleWorkspaceTitle = (event: Event) => {
      const detail = (event as CustomEvent<{ title?: string | null }>).detail;
      setMobileWorkspaceTitle(detail?.title ? String(detail.title) : null);
    };

    window.addEventListener("chatgpt-image-studio:mobile-workspace-title", handleWorkspaceTitle as EventListener);
    return () => {
      window.removeEventListener("chatgpt-image-studio:mobile-workspace-title", handleWorkspaceTitle as EventListener);
    };
  }, [isImageRoute]);

  const handleLogout = async () => {
    await clearStoredAuthKey();
    navigate("/login", { replace: true });
  };

  if (pathname === "/login" || pathname === "/login.html" || pathname.startsWith("/login/")) {
    return null;
  }

  return (
    <>
      <div
        className="lg:hidden"
        style={{
          height: isMobileWorkspaceRoute
            ? mobileWorkspaceHeaderHeight + (mobileNavExpanded ? mobileHeaderHeight : 0)
            : mobileHeaderHeight,
        }}
      />
      {!isMobileWorkspaceRoute ? (
        <header ref={setMobileHeaderRef} className="fixed inset-x-0 top-0 z-40 px-3 lg:hidden">
        <div className="rounded-[26px] border border-stone-200 bg-[#f0f0ed] p-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.65)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.03)]">
          <div className="flex items-center justify-between gap-3">
            <div className="flex min-w-0 flex-1 items-center gap-3">
              <ThemeToggleButton className="size-10 shrink-0" />
              <button
                type="button"
                className="flex min-w-0 flex-1 items-center rounded-2xl px-1 py-1 text-left transition hover:bg-white/70 dark:hover:bg-[var(--studio-panel-soft)]"
                onClick={() => setMobileNavExpanded((current) => !current)}
                aria-label={mobileNavExpanded ? "收起导航" : "展开导航"}
              >
                <BrandCopy subtitle={mobileNavExpanded ? "点击收起导航" : "点击展开导航"} />
              </button>
            </div>
            <div className="flex items-center gap-2">
              <Link
                to="/image/history"
                className="hidden rounded-2xl border border-stone-200 bg-white px-3 py-2 text-xs font-medium text-stone-600 shadow-sm dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)] sm:inline-flex"
              >
                {(authUser?.role === "admin" ? navItems : navItems.filter((item) => item.href.startsWith("/image"))).find((item) => isNavItemActive(pathname, item.href, item.matchPrefix))?.label ?? "导航"}
              </Link>
              <button
                type="button"
                className="inline-flex h-10 shrink-0 items-center justify-center rounded-2xl border border-stone-200 bg-white px-3 text-sm font-medium text-stone-700 transition hover:bg-stone-50 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)] dark:hover:bg-[var(--studio-panel-muted)]"
                onClick={() => void handleLogout()}
              >
                <LogOut className="size-4" />
              </button>
            </div>
          </div>

          {mobileNavExpanded ? (
            <nav className="hide-scrollbar mt-3 -mx-1 overflow-x-auto px-1">
              <div className="inline-flex min-w-full gap-2 rounded-[20px] bg-white/55 p-1">
                {(authUser?.role === "admin" ? navItems : navItems.filter((item) => item.href.startsWith("/image"))).map((item) => {
                  const active = isNavItemActive(pathname, item.href, item.matchPrefix);
                  const Icon = item.icon;
                  return (
                    <Link
                      key={item.href}
                      to={item.href}
                      className={cn(
                        "flex min-w-[104px] shrink-0 items-center justify-center gap-2 rounded-2xl px-3 py-2.5 text-sm font-medium transition",
                        active
                          ? "bg-white text-stone-950 shadow-sm dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-strong)]"
                          : "text-stone-600 hover:bg-white/75 hover:text-stone-900 dark:text-[var(--studio-text-muted)] dark:hover:bg-[var(--studio-panel-soft)] dark:hover:text-[var(--studio-text-strong)]",
                      )}
                    >
                      <Icon className="size-4 shrink-0" />
                      <span className="truncate">{item.label}</span>
                    </Link>
                  );
                })}
              </div>
            </nav>
          ) : null}
        </div>
        </header>
      ) : null}
      {isMobileWorkspaceRoute ? (
        <div
          ref={mobileWorkspaceHeaderRef}
          className="fixed inset-x-0 top-0 z-40 px-3 lg:hidden"
          style={{ top: mobileNavExpanded ? mobileHeaderHeight : 0 }}
        >
          <div className="min-w-0 rounded-[26px] border border-stone-200 bg-[#f0f0ed] p-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.65)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:shadow-[inset_0_1px_0_rgba(255,255,255,0.03)]">
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={() => navigate("/image/history")}
                className="inline-flex h-10 items-center gap-2 rounded-full border border-stone-200 bg-white px-4 text-stone-700 shadow-none dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)]"
              >
                <ChevronLeft className="size-4" />
                会话历史
              </button>
              <h1 className="text-xl font-semibold tracking-tight text-stone-950 dark:text-[var(--studio-text-strong)]">图片工作台</h1>
              <ThemeToggleButton className="ml-auto size-9 shrink-0 rounded-full" />
              <button
                type="button"
                onClick={() => setMobileNavExpanded((current) => !current)}
                className="inline-flex size-9 items-center justify-center rounded-full border border-stone-200 bg-white text-stone-600 shadow-none dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)]"
                aria-label={mobileNavExpanded ? "收起导航" : "显示导航"}
                title={mobileNavExpanded ? "收起导航" : "显示导航"}
              >
                {mobileNavExpanded ? <PanelLeftClose className="size-4" /> : <PanelLeftOpen className="size-4" />}
              </button>
            </div>
            {mobileWorkspaceTitle ? (
              <div className="mt-3">
                <span className="inline-flex max-w-full truncate rounded-full bg-white/80 px-3 py-1 text-xs font-medium text-stone-600 dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)]">
                  {mobileWorkspaceTitle}
                </span>
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
      {isMobileWorkspaceRoute && mobileNavExpanded ? (
        <div
          ref={setMobileHeaderRef}
          className="fixed inset-x-0 top-0 z-40 px-3 lg:hidden"
        >
          <div className="rounded-[24px] border border-stone-200 bg-[#f0f0ed] p-3 shadow-[0_14px_40px_rgba(15,23,42,0.05)] transition-colors duration-200 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel)] dark:shadow-[0_14px_40px_rgba(0,0,0,0.5)]">
            <div className="flex items-center justify-between gap-3">
              <div className="flex min-w-0 flex-1 items-center gap-3">
                <ThemeToggleButton className="size-10 shrink-0" />
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center rounded-2xl px-1 py-1 text-left transition hover:bg-white/70 dark:hover:bg-[var(--studio-panel-soft)]"
                  onClick={() => setMobileNavExpanded(false)}
                  aria-label="收起导航"
                >
                  <BrandCopy subtitle="点击收起导航" />
                </button>
              </div>
              <button
                type="button"
                className="inline-flex h-10 shrink-0 items-center justify-center rounded-2xl border border-stone-200 bg-white px-3 text-sm font-medium text-stone-700 transition hover:bg-stone-50 dark:border-[var(--studio-border)] dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text)] dark:hover:bg-[var(--studio-panel-muted)]"
                onClick={() => void handleLogout()}
              >
                <LogOut className="size-4" />
              </button>
            </div>
            <nav className="hide-scrollbar mt-3 -mx-1 overflow-x-auto px-1">
              <div className="inline-flex min-w-full gap-2 rounded-[20px] bg-white/55 p-1">
                {(authUser?.role === "admin" ? navItems : navItems.filter((item) => item.href.startsWith("/image"))).map((item) => {
                  const active = isNavItemActive(pathname, item.href, item.matchPrefix);
                  const Icon = item.icon;
                  return (
                    <Link
                      key={item.href}
                      to={item.href}
                      className={cn(
                        "flex min-w-[104px] shrink-0 items-center justify-center gap-2 rounded-2xl px-3 py-2.5 text-sm font-medium transition",
                        active
                          ? "bg-white text-stone-950 shadow-sm dark:bg-[var(--studio-panel-soft)] dark:text-[var(--studio-text-strong)]"
                          : "text-stone-600 hover:bg-white/75 hover:text-stone-900 dark:text-[var(--studio-text-muted)] dark:hover:bg-[var(--studio-panel-soft)] dark:hover:text-[var(--studio-text-strong)]",
                      )}
                    >
                      <Icon className="size-4 shrink-0" />
                      <span className="truncate">{item.label}</span>
                    </Link>
                  );
                })}
              </div>
            </nav>
          </div>
        </div>
      ) : null}
      <DesktopTopNav
        key={isImageRoute ? "image-route" : "non-image-route"}
        pathname={pathname}
        defaultCollapsed={isImageRoute}
        versionLabel={versionLabel}
        user={authUser ? { ...authUser, imageApiKey: authUser.imageApiKey || (authUser.role === "admin" ? authKey : authUser.imageApiKey) } : null}
        onLogout={handleLogout}
      />
    </>
  );
}
