import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { Input } from "@cloudflare/kumo";
import { CircleNotch, Eye, EyeSlash, Fingerprint } from "@phosphor-icons/react";
import { ApiRequestError } from "../api/client";
import { ErrorBanner } from "../components/common/ErrorBanner";
import { LoadingScreen } from "../components/common/LoadingScreen";
import { Logo } from "../components/ui";
import { useAuth } from "../context/auth";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import { isPasskeySupported } from "../lib/webauthn";

function errorMessage(error: unknown): string {
  if (error instanceof ApiRequestError) {
    if (error.status === 401) return "用户名或密码错误";
    if (error.status === 429) return "尝试次数过多，请稍后再试";
    if (error.status === 400) return error.message;
    if (error.status >= 500) return "服务器暂时无法处理请求，请稍后再试";
    return error.message;
  }
  if (error instanceof DOMException) {
    if (error.name === "NotAllowedError") return "已取消通行密钥登录";
    if (error.name === "SecurityError")
      return "当前页面不是安全上下文，通行密钥需要 HTTPS 或 localhost";
  }
  if (error instanceof Error && error.message) return error.message;
  return "无法连接服务器，请检查网络或后端服务是否已启动";
}

function passkeyErrorMessage(error: unknown): string {
  if (error instanceof ApiRequestError && error.status === 401) {
    return "通行密钥验证失败，请重试或改用密码登录";
  }
  return errorMessage(error);
}

function totpErrorMessage(error: unknown): string {
  if (error instanceof ApiRequestError) {
    if (error.status === 401) return "验证码无效或已过期，请重试";
    if (error.status === 429) return "尝试次数过多，请稍后再试";
    return errorMessage(error);
  }
  return errorMessage(error);
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
  const {
    session,
    isAuthenticated,
    isLoading,
    login,
    loginWithPasskey,
    verifyTOTP,
    cancelMfa,
    isLoggingIn,
    isLoggingInWithPasskey,
    isMfaPending,
    isVerifyingTOTP,
  } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const returnTo = safeReturnPath(
    (location.state as { from?: unknown } | null)?.from,
  );

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [totpCode, setTotpCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const passkeysAvailable = isPasskeySupported();
  const busy = isLoggingIn || isLoggingInWithPasskey || isVerifyingTOTP;

  useDocumentTitle(isMfaPending ? "两步验证" : "登录");

  if (isLoading) {
    return <LoadingScreen />;
  }

  if (session && !session.initialized) return <Navigate to="/setup" replace />;
  if (isAuthenticated) return <Navigate to={returnTo} replace />;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    if (isMfaPending) {
      if (!totpCode.trim()) {
        setError("请输入验证码或恢复码");
        return;
      }
      setError(null);
      try {
        await verifyTOTP(totpCode.trim());
        navigate(returnTo, { replace: true });
      } catch (cause) {
        setError(totpErrorMessage(cause));
      }
      return;
    }
    if (!username.trim() || !password) {
      setError("请输入用户名和密码");
      return;
    }

    setError(null);
    try {
      const result = await login(username.trim(), password);
      if (!result.mfaRequired) {
        navigate(returnTo, { replace: true });
      }
    } catch (cause) {
      setError(errorMessage(cause));
    }
  };

  const handleCancelMfa = () => {
    cancelMfa();
    setTotpCode("");
    setError(null);
  };

  const handlePasskeyLogin = async () => {
    if (busy) return;
    setError(null);
    try {
      await loginWithPasskey();
      navigate(returnTo, { replace: true });
    } catch (cause) {
      setError(passkeyErrorMessage(cause));
    }
  };

  return (
    <div className="login-backdrop relative flex min-h-dvh flex-col text-kumo-default">
      <main className="page-enter relative flex flex-1 items-center justify-center px-4 py-16">
        <div className="w-full max-w-[24rem]">
          <form
            noValidate
            onSubmit={handleSubmit}
            className="auth-panel flex flex-col p-6 sm:p-8"
          >
            <div className="flex flex-col items-center text-center">
              <Logo className="[&_svg]:size-9" />
              <h1 className="mt-5 text-[1.75rem] leading-[1.15] font-semibold tracking-[-0.02em] sm:text-[2rem]">
                {isMfaPending ? "两步验证" : "登录 Manga Reader"}
              </h1>
              {isMfaPending && (
                <p className="mt-3 text-[0.875rem] leading-5 text-kumo-subtle">
                  请输入身份验证器中的 6 位验证码，或使用一个恢复码。
                </p>
              )}
            </div>

            {isMfaPending ? (
              <>
                <div className="mt-8 space-y-3">
                  <div className="relative">
                    <label htmlFor="totp-code" className="sr-only">
                      验证码或恢复码
                    </label>
                    <Input
                      id="totp-code"
                      name="totp-code"
                      type="text"
                      autoComplete="one-time-code"
                      autoFocus
                      spellCheck={false}
                      size="lg"
                      placeholder="6 位验证码或恢复码"
                      aria-label="验证码或恢复码"
                      className="h-12 w-full rounded-xl px-4 text-center text-[1.05rem] tracking-[0.12em] focus:!ring-[var(--app-accent)] disabled:text-kumo-default"
                      value={totpCode}
                      onChange={(event) => setTotpCode(event.target.value)}
                      disabled={busy}
                    />
                  </div>

                  {error && <ErrorBanner message={error} className="mt-1" />}
                </div>

                <button
                  type="submit"
                  className="auth-submit mt-4 flex h-12 w-full items-center justify-center rounded-xl text-[0.9375rem] font-medium disabled:cursor-not-allowed disabled:opacity-60"
                  disabled={busy}
                  aria-busy={isVerifyingTOTP}
                >
                  {isVerifyingTOTP ? (
                    <>
                      <CircleNotch className="mr-2 size-4 animate-spin" />
                      正在验证…
                    </>
                  ) : (
                    "验证并登录"
                  )}
                </button>

                <button
                  type="button"
                  onClick={handleCancelMfa}
                  disabled={busy}
                  className="mt-3 text-sm font-medium text-kumo-subtle transition-colors hover:text-kumo-default disabled:cursor-not-allowed disabled:opacity-60"
                >
                  返回密码登录
                </button>
              </>
            ) : (
              <>
                <div className="mt-8 space-y-3">
                  <div className="relative">
                    <label htmlFor="username" className="sr-only">
                      用户名
                    </label>
                    <Input
                      id="username"
                      name="username"
                      autoComplete="username"
                      spellCheck={false}
                      size="lg"
                      placeholder="用户名"
                      aria-label="用户名"
                      className="h-12 w-full rounded-xl px-4 text-[0.9375rem] focus:!ring-[var(--app-accent)] disabled:text-kumo-default"
                      value={username}
                      onChange={(event) => setUsername(event.target.value)}
                      disabled={busy}
                    />
                  </div>

                  <div className="relative">
                    <label htmlFor="password" className="sr-only">
                      密码
                    </label>
                    <Input
                      id="password"
                      name="password"
                      type={showPassword ? "text" : "password"}
                      autoComplete="current-password"
                      size="lg"
                      placeholder="密码"
                      aria-label="密码"
                      className="h-12 w-full rounded-xl pr-12 pl-4 text-[0.9375rem] focus:!ring-[var(--app-accent)] disabled:text-kumo-default"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      disabled={busy}
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword((visible) => !visible)}
                      aria-label={showPassword ? "隐藏密码" : "显示密码"}
                      title={showPassword ? "隐藏密码" : "显示密码"}
                      className="absolute right-2 top-1/2 flex size-8 -translate-y-1/2 items-center justify-center rounded-lg text-kumo-subtle transition-colors hover:text-kumo-default focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--app-accent)]"
                    >
                      {showPassword ? (
                        <EyeSlash className="size-4" />
                      ) : (
                        <Eye className="size-4" />
                      )}
                    </button>
                  </div>

                  {error && <ErrorBanner message={error} className="mt-1" />}
                </div>

                <button
                  type="submit"
                  className="auth-submit mt-4 flex h-12 w-full items-center justify-center rounded-xl text-[0.9375rem] font-medium disabled:cursor-not-allowed disabled:opacity-60"
                  disabled={busy}
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
                </button>

                {passkeysAvailable && (
                  <>
                    <div className="mt-5 flex items-center gap-3 text-xs text-kumo-subtle">
                      <span className="h-px flex-1 bg-kumo-tint" />
                      或
                      <span className="h-px flex-1 bg-kumo-tint" />
                    </div>
                    <button
                      type="button"
                      onClick={() => void handlePasskeyLogin()}
                      disabled={busy}
                      aria-busy={isLoggingInWithPasskey}
                      className="mt-3 flex h-12 w-full items-center justify-center rounded-xl border border-kumo-tint bg-kumo-base text-[0.9375rem] font-medium text-kumo-default transition-colors hover:bg-kumo-tint disabled:cursor-not-allowed disabled:opacity-60 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--app-accent)]"
                    >
                      {isLoggingInWithPasskey ? (
                        <>
                          <CircleNotch className="mr-2 size-4 animate-spin" />
                          等待通行密钥…
                        </>
                      ) : (
                        <>
                          <Fingerprint className="mr-2 size-4" weight="bold" />
                          使用通行密钥登录
                        </>
                      )}
                    </button>
                  </>
                )}
              </>
            )}
          </form>
        </div>
      </main>
    </div>
  );
}
