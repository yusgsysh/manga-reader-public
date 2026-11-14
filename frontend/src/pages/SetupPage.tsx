import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "react-router";
import { Button, Input } from "@cloudflare/kumo";
import {
  CircleNotch,
  Eye,
  EyeSlash,
  LockSimple,
  User,
} from "@phosphor-icons/react";
import { ApiRequestError } from "../api/client";
import { ErrorBanner } from "../components/common/ErrorBanner";
import { LoadingScreen } from "../components/common/LoadingScreen";
import { Logo } from "../components/ui";
import { useAuth } from "../context/auth";
import { useDocumentTitle } from "../hooks/useDocumentTitle";

function errorMessage(error: unknown): string {
  if (error instanceof ApiRequestError) {
    if (error.status === 409) return "实例已完成初始化，请直接登录";
    if (error.status === 429) return "尝试次数过多，请稍后再试";
    if (error.status === 400) return error.message;
    if (error.status >= 500) return "服务器暂时无法处理请求，请稍后再试";
    return error.message;
  }
  return "无法连接服务器，请检查网络或后端服务是否已启动";
}

/**
 * First-run setup: creates the instance's first administrator. It is only
 * reachable while `/api/auth/status` reports `initialized: false`; once any
 * account exists the backend rejects further registrations and this page
 * redirects to the login page.
 */
export function SetupPage() {
  const { session, isAuthenticated, isLoading, isRegistering, register } =
    useAuth();
  const navigate = useNavigate();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useDocumentTitle("初始化");

  if (isLoading) {
    return <LoadingScreen />;
  }

  if (isAuthenticated || session?.initialized) return <Navigate to="/" replace />;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (isRegistering) return;
    if (!username.trim() || !password) {
      setError("请输入用户名和密码");
      return;
    }
    if (password.length < 8) {
      setError("密码至少需要 8 位");
      return;
    }
    if (password !== confirmPassword) {
      setError("两次输入的密码不一致");
      return;
    }

    setError(null);
    try {
      await register(username.trim(), password);
      navigate("/", { replace: true });
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  return (
    <div className="login-backdrop relative flex min-h-dvh flex-col bg-kumo-base text-kumo-default">
      <main className="page-enter relative flex flex-1 items-center justify-center px-4 py-16">
        <div className="w-full max-w-[24rem]">
          <form
            noValidate
            onSubmit={handleSubmit}
            className="login-card px-6 py-8 sm:px-8 sm:py-9"
          >
            <div className="flex flex-col items-center text-center">
              <div className="login-mark">
                <Logo className="[&_svg]:size-8" />
              </div>
              <h1 className="mt-4 text-[1.375rem] font-semibold tracking-tight">
                创建管理员账户
              </h1>
              <p className="mt-2 text-[0.8125rem] leading-5 text-kumo-subtle">
                首次启动需要创建一个管理员账户来完成初始化。
              </p>
            </div>

            <div className="mt-8 space-y-4">
              <div className="space-y-1.5">
                <label
                  htmlFor="username"
                  className="block text-[0.8125rem] font-medium text-kumo-default"
                >
                  用户名
                </label>
                <div className="relative">
                  <User
                    className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-kumo-subtle"
                    aria-hidden
                  />
                  <Input
                    id="username"
                    name="username"
                    autoComplete="username"
                    spellCheck={false}
                    size="lg"
                    aria-label="用户名"
                    className="h-11 w-full rounded-xl pl-10 pr-3.5 text-[0.9375rem] disabled:text-kumo-default"
                    value={username}
                    onChange={(event) => setUsername(event.target.value)}
                    disabled={isRegistering}
                  />
                </div>
              </div>

              <div className="space-y-1.5">
                <label
                  htmlFor="password"
                  className="block text-[0.8125rem] font-medium text-kumo-default"
                >
                  密码
                </label>
                <div className="relative">
                  <LockSimple
                    className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-kumo-subtle"
                    aria-hidden
                  />
                  <Input
                    id="password"
                    name="password"
                    type={showPassword ? "text" : "password"}
                    autoComplete="new-password"
                    size="lg"
                    aria-label="密码"
                    className="h-11 w-full rounded-xl pl-10 pr-11 text-[0.9375rem] disabled:text-kumo-default"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    disabled={isRegistering}
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword((visible) => !visible)}
                    aria-label={showPassword ? "隐藏密码" : "显示密码"}
                    title={showPassword ? "隐藏密码" : "显示密码"}
                    className="absolute right-2 top-1/2 flex size-8 -translate-y-1/2 items-center justify-center rounded-lg text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--app-accent)]"
                  >
                    {showPassword ? (
                      <EyeSlash className="size-4" />
                    ) : (
                      <Eye className="size-4" />
                    )}
                  </button>
                </div>
              </div>

              <div className="space-y-1.5">
                <label
                  htmlFor="confirm-password"
                  className="block text-[0.8125rem] font-medium text-kumo-default"
                >
                  确认密码
                </label>
                <div className="relative">
                  <LockSimple
                    className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-kumo-subtle"
                    aria-hidden
                  />
                  <Input
                    id="confirm-password"
                    name="confirm-password"
                    type={showPassword ? "text" : "password"}
                    autoComplete="new-password"
                    size="lg"
                    aria-label="确认密码"
                    className="h-11 w-full rounded-xl pl-10 pr-3.5 text-[0.9375rem] disabled:text-kumo-default"
                    value={confirmPassword}
                    onChange={(event) => setConfirmPassword(event.target.value)}
                    disabled={isRegistering}
                  />
                </div>
              </div>

              {error && <ErrorBanner message={error} />}
            </div>

            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="mt-6 h-11 w-full justify-center rounded-xl text-[0.9375rem] font-medium shadow-[0_10px_24px_-12px_var(--app-ring)] disabled:opacity-70"
              disabled={isRegistering}
              aria-busy={isRegistering}
            >
              {isRegistering ? (
                <>
                  <CircleNotch className="mr-2 size-4 animate-spin" />
                  正在创建…
                </>
              ) : (
                "创建管理员"
              )}
            </Button>
          </form>
        </div>
      </main>
    </div>
  );
}
