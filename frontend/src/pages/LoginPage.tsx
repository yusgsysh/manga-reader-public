import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { Button, Input, Loader } from "@cloudflare/kumo";
import {
  CircleNotch,
  Eye,
  EyeSlash,
  LockSimple,
  User,
  WarningCircle,
} from "@phosphor-icons/react";
import { ApiRequestError } from "../api/client";
import { Logo } from "../components/ui";
import { ThemeSwitcher } from "../components/layout/ThemeSwitcher";
import { useAuth } from "../context/auth";
import { useDocumentTitle } from "../hooks/useDocumentTitle";

function errorMessage(error: unknown): string {
  if (error instanceof ApiRequestError) {
    if (error.status === 401) return "用户名或密码错误";
    if (error.status === 429) return "尝试次数过多，请稍后再试";
    if (error.status === 400) return error.message;
    if (error.status >= 500) return "服务器暂时无法处理请求，请稍后再试";
    return error.message;
  }
  return "无法连接服务器，请检查网络或后端服务是否已启动";
}

/** Only allow same-origin paths as the post-login destination. */
function safeReturnPath(value: unknown): string {
  if (
    typeof value === "string" &&
    value.startsWith("/") &&
    !value.startsWith("//") &&
    !value.startsWith("/login")
  ) {
    return value;
  }
  return "/";
}

export function LoginPage() {
  const { enabled, isAuthenticated, isLoading, login, isLoggingIn } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const returnTo = safeReturnPath(
    (location.state as { from?: unknown } | null)?.from,
  );

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useDocumentTitle("登录");

  if (isLoading) {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-kumo-base">
        <Loader size={32} />
      </div>
    );
  }

  // Legacy anonymous deployments have no login wall.
  if (!enabled) return <Navigate to={returnTo} replace />;
  if (isAuthenticated) return <Navigate to={returnTo} replace />;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (isLoggingIn) return;
    if (!username.trim() || !password) {
      setError("请输入用户名和密码");
      return;
    }

    setError(null);
    try {
      await login(username.trim(), password);
      navigate(returnTo, { replace: true });
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  return (
    <div className="login-backdrop relative flex min-h-dvh flex-col bg-kumo-base text-kumo-default">
      <div className="absolute right-4 top-4 z-10">
        <ThemeSwitcher className="w-[7.5rem] shadow-sm" />
      </div>

      <main className="page-enter relative flex flex-1 items-center justify-center px-4 py-16">
        <div className="w-full max-w-sm">
          <div className="mb-8 flex flex-col items-center text-center">
            <div className="login-mark">
              <Logo className="[&_svg]:size-8" />
            </div>
            <h1 className="mt-5 text-2xl font-semibold tracking-tight">
              Manga Reader
            </h1>
            <p className="mt-1.5 text-sm text-kumo-subtle">
              登录以访问你的书架与阅读进度
            </p>
          </div>

          <form
            noValidate
            onSubmit={handleSubmit}
            className="card-surface login-card space-y-5 p-6 sm:p-7"
          >
            <div className="space-y-1.5">
              <label htmlFor="username" className="text-sm font-medium">
                用户名
              </label>
              <div className="relative">
                <User
                  className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-kumo-subtle"
                  aria-hidden
                />
                <Input
                  id="username"
                  name="username"
                  autoComplete="username"
                  spellCheck={false}
                  size="lg"
                  aria-label="用户名"
                  className="pl-9"
                  placeholder="请输入用户名"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  disabled={isLoggingIn}
                />
              </div>
            </div>

            <div className="space-y-1.5">
              <label htmlFor="password" className="text-sm font-medium">
                密码
              </label>
              <div className="relative">
                <LockSimple
                  className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-kumo-subtle"
                  aria-hidden
                />
                <Input
                  id="password"
                  name="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="current-password"
                  size="lg"
                  aria-label="密码"
                  className="px-9"
                  placeholder="请输入密码"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  disabled={isLoggingIn}
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((visible) => !visible)}
                  aria-label={showPassword ? "隐藏密码" : "显示密码"}
                  title={showPassword ? "隐藏密码" : "显示密码"}
                  className="absolute right-1.5 top-1/2 flex size-8 -translate-y-1/2 items-center justify-center rounded-lg text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--app-accent)]"
                >
                  {showPassword ? (
                    <EyeSlash className="size-4" />
                  ) : (
                    <Eye className="size-4" />
                  )}
                </button>
              </div>
            </div>

            {error && (
              <p
                role="alert"
                className="flex items-start gap-2 rounded-lg border border-kumo-danger/30 bg-kumo-danger/10 px-3 py-2 text-sm text-kumo-danger"
              >
                <WarningCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                <span>{error}</span>
              </p>
            )}

            <Button
              type="submit"
              variant="primary"
              size="lg"
              className="w-full"
              disabled={isLoggingIn}
              aria-busy={isLoggingIn}
            >
              {isLoggingIn ? (
                <>
                  <CircleNotch className="mr-2 size-4 animate-spin" />
                  正在登录…
                </>
              ) : (
                "登录"
              )}
            </Button>
          </form>

          <p className="mt-6 text-center text-xs text-kumo-subtle/90">
            JWT 身份认证 · Token 保存在 HttpOnly Cookie 中
          </p>
        </div>
      </main>
    </div>
  );
}
