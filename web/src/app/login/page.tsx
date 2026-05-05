"use client";

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { CircleAlert, LoaderCircle, LockKeyhole, Sparkles } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { login, registerUser } from "@/lib/api";
import { setStoredAuthKey, setStoredAuthUser } from "@/store/auth";

export default function LoginPage() {
  const navigate = useNavigate();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [username, setUsername] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleAuthSuccess = async (payload: Awaited<ReturnType<typeof login>>) => {
    await setStoredAuthKey(payload.token);
    await setStoredAuthUser(payload.user);
    navigate(payload.user.role === "admin" ? "/accounts" : "/image", { replace: true });
  };

  const handleSubmit = async () => {
    setIsSubmitting(true);
    try {
      const normalizedUsername = username.trim().toLowerCase();
      if (!normalizedUsername || !password) {
        toast.error("请输入用户名和密码");
        return;
      }
      const payload =
        mode === "register"
          ? await registerUser({
              username: normalizedUsername,
              password,
              inviteCode: inviteCode.trim().toUpperCase(),
              name: name.trim() || undefined,
            })
          : await login(normalizedUsername, password);
      await handleAuthSuccess(payload);
    } catch (error) {
      const message = error instanceof Error ? error.message : mode === "register" ? "注册失败" : "登录失败";
      toast.error(message);
    } finally {
      setIsSubmitting(false);
    }
  };

  const isRegister = mode === "register";

  return (
    <div className="grid h-full min-h-0 w-full place-items-center overflow-y-auto">
      <div className="grid w-full max-w-[1120px] overflow-hidden rounded-[32px] border border-stone-200 bg-white shadow-[0_24px_80px_rgba(15,23,42,0.08)] lg:grid-cols-[1.05fr_0.95fr]">
        <div className="hidden bg-[radial-gradient(circle_at_top_left,_rgba(255,255,255,0.78),_rgba(255,255,255,0.18)_38%,_rgba(28,25,23,0.08)_100%),linear-gradient(155deg,#111827_0%,#1f2937_52%,#374151_100%)] p-8 text-white lg:flex lg:flex-col lg:justify-between">
          <div className="flex items-center gap-3">
            <span className="flex size-11 items-center justify-center rounded-2xl bg-white/12 backdrop-blur">
              <Sparkles className="size-4" />
            </span>
            <div>
              <div className="text-sm font-semibold tracking-tight">ChatGpt Image Studio</div>
              <div className="mt-1 text-xs text-white/65">多人隔离的图片工作区</div>
            </div>
          </div>

          <div className="space-y-6">
            <div className="space-y-3">
              <div className="text-sm font-medium uppercase tracking-[0.24em] text-white/55">Image Studio</div>
              <h1 className="max-w-[420px] text-[40px] font-semibold leading-[1.1] tracking-tight">
                每个用户都有自己的图片工作台。
              </h1>
              <p className="max-w-[430px] text-sm leading-7 text-white/72">
                普通用户通过用户名、密码和邀请码注册；管理员使用 admin 加管理员密码登录；历史记录、任务队列和图片文件按用户隔离。
              </p>
            </div>

            <div className="grid gap-3 sm:grid-cols-3">
              {[
                ["登录", "用户名 + 密码进入工作区"],
                ["注册", "邀请码创建普通用户"],
                ["后台", "admin 用户管理配置"],
              ].map(([title, desc]) => (
                <div key={title} className="rounded-2xl border border-white/12 bg-white/6 p-4 backdrop-blur-sm">
                  <div className="text-sm font-semibold">{title}</div>
                  <div className="mt-2 text-xs leading-6 text-white/65">{desc}</div>
                </div>
              ))}
            </div>
          </div>

          <div className="text-xs text-white/50">管理员用户名固定为 admin，密码使用后端管理员密码。</div>
        </div>

        <div className="flex items-center justify-center px-5 py-8 sm:px-8 lg:px-10">
          <div className="w-full max-w-[420px] space-y-7">
            <div className="space-y-4">
              <div className="inline-flex size-14 items-center justify-center rounded-[18px] bg-stone-950 text-white shadow-sm">
                <LockKeyhole className="size-5" />
              </div>
              <div className="space-y-2">
                <h1 className="text-3xl font-semibold tracking-tight text-stone-950">
                  {isRegister ? "注册工作台账号" : "登录工作台"}
                </h1>
                <p className="text-sm leading-7 text-stone-500">
                  {isRegister ? "使用邀请码创建普通用户账号。" : "普通用户用自己的用户名登录；管理员使用 admin 加管理员密码登录。"}
                </p>
              </div>
            </div>

            <div className="space-y-3">
              {isRegister ? (
                <Input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="昵称（可选）"
                  className="h-13 rounded-2xl border-stone-200 bg-stone-50 px-4 shadow-none focus-visible:ring-1"
                />
              ) : null}
              <Input
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                placeholder={isRegister ? "用户名，例如 xiaochige" : "用户名，管理员为 admin"}
                className="h-13 rounded-2xl border-stone-200 bg-stone-50 px-4 shadow-none focus-visible:ring-1"
              />
              {isRegister ? (
                <Input
                  value={inviteCode}
                  onChange={(event) => setInviteCode(event.target.value.toUpperCase())}
                  placeholder="邀请码"
                  className="h-13 rounded-2xl border-stone-200 bg-stone-50 px-4 shadow-none focus-visible:ring-1"
                />
              ) : null}
              <Input
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") void handleSubmit();
                }}
                placeholder={isRegister ? "密码，至少 6 位" : "密码"}
                className="h-13 rounded-2xl border-stone-200 bg-stone-50 px-4 shadow-none focus-visible:ring-1"
              />
            </div>

            <Button
              className="h-13 w-full rounded-2xl bg-stone-950 text-white hover:bg-stone-800"
              onClick={() => void handleSubmit()}
              disabled={isSubmitting}
            >
              {isSubmitting ? <LoaderCircle className="size-4 animate-spin" /> : null}
              {isRegister ? "注册并进入" : "登录"}
            </Button>

            <div className="text-center text-sm text-stone-500">
              {isRegister ? "已有账号？" : "还没有账号？"}
              <button
                type="button"
                className="ml-2 font-medium text-stone-950 underline underline-offset-4"
                onClick={() => setMode(isRegister ? "login" : "register")}
              >
                {isRegister ? "返回登录" : "注册账号"}
              </button>
            </div>

            <div className="rounded-2xl border border-stone-200 bg-stone-50 px-4 py-4 text-xs leading-6 text-stone-500">
              普通用户只能访问图片工作台；账号、配置、同步、诊断等入口仅管理员可见并由后端拦截。
            </div>

            <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-4 text-sm leading-6 text-amber-950">
              <div className="flex items-center gap-2 font-medium">
                <CircleAlert className="size-4" />
                使用与风险提示
              </div>
              <div className="mt-2">
                项目基于对 ChatGPT 官网相关能力的研究实现，存在账号被限制、临时封禁或永久封禁的风险。请勿使用常用、大号或高价值账号测试。
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
