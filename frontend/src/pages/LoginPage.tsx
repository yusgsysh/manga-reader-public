import { useState } from "react";
import { Navigate, useLocation } from "react-router";
import { CircleNotch } from "@phosphor-icons/react";
import { ErrorBanner } from "../components/common/ErrorBanner";
import { LoadingScreen } from "../components/common/LoadingScreen";
import { Logo } from "../components/ui";
import {
  readReturnPath,
  safeReturnPath,
  useAuth,
} from "../context/auth";
import { useDocumentTitle } from "../hooks/useDocumentTitle";

function sdkErrorMessage(search: string): string | null {
  const params = new URLSearchParams(search);
  const error = params.get("error");
  if (!error) return null;
  const description = params.get("error_description");
  return description
    ? `登录失败：${description}`
    : `登录失败（${error}），请重试。`;
}

export function LoginPage() {
  const {
    isAuthenticated,
    isLoading,
    error,
    login,
    register,
    isLoggingIn,
    isRegistering,
    registrationEnabled,
  } = useAuth();
  const location = useLocation();
  const returnTo =
    readReturnPath() ??
    safeReturnPath((location.state as { from?: unknown } | null)?.from);

  const [actionError, setActionError] = useState<string | null>(null);
  const [pending, setPending] = useState<"login" | "register" | null>(null);

  useDocumentTitle("登录");

  if (isLoading) {
    return <LoadingScreen />;
  }

  if (isAuthenticated) {
    return <Navigate to={returnTo} replace />;
  }

  const message =
    actionError ??
    (error ? error.message : null) ??
    sdkErrorMessage(location.search);

  const start = async (kind: "login" | "register") => {
    if (pending) return;
    setPending(kind);
    setActionError(null);
    try {
      if (kind === "login") {
        await login(returnTo);
      } else {
        await register(returnTo);
      }
    } catch (cause) {
      setActionError(
        cause instanceof Error
          ? `无法启动认证：${cause.message}`
          : "无法启动认证，请稍后再试。",
      );
    } finally {
      setPending(null);
    }
  };

  const busy = pending !== null || isLoggingIn || isRegistering;

  return (
    <div className="login-backdrop relative flex min-h-dvh flex-col text-kumo-default">
      <main className="page-enter relative flex flex-1 items-center justify-center px-4 py-16">
        <div className="w-full max-w-[24rem]">
          <div className="auth-panel flex flex-col p-6 sm:p-8">
            <div className="flex flex-col items-center text-center">
              <Logo className="[&_svg]:size-9" />
              <h1 className="mt-5 text-[1.75rem] leading-[1.15] font-semibold tracking-[-0.02em] sm:text-[2rem]">
                登录 Manga Reader
              </h1>
              <p className="mt-3 text-[0.875rem] leading-5 text-kumo-subtle">
                {registrationEnabled
                  ? "使用 Kinde 账号登录。首次使用请先注册，注册后会自动返回此应用。"
                  : "使用 Kinde 账号登录。"}
              </p>
            </div>

            {message && <ErrorBanner message={message} className="mt-6" />}

            <div className="mt-7 space-y-3">
              <button
                type="button"
                onClick={() => void start("login")}
                className="auth-submit flex h-12 w-full items-center justify-center rounded-xl text-[0.9375rem] font-medium disabled:cursor-not-allowed disabled:opacity-60"
                disabled={busy}
                aria-busy={busy && pending === "login"}
              >
                {pending === "login" ? (
                  <>
                    <CircleNotch className="mr-2 size-4 animate-spin" />
                    正在跳转…
                  </>
                ) : (
                  "登录"
                )}
              </button>
              {registrationEnabled && (
                <button
                  type="button"
                  onClick={() => void start("register")}
                  className="flex h-12 w-full items-center justify-center rounded-xl border border-kumo-hairline text-[0.9375rem] font-medium text-kumo-default transition-colors hover:bg-kumo-tint disabled:cursor-not-allowed disabled:opacity-60"
                  disabled={busy}
                  aria-busy={busy && pending === "register"}
                >
                  {pending === "register" ? (
                    <>
                      <CircleNotch className="mr-2 size-4 animate-spin" />
                      正在跳转…
                    </>
                  ) : (
                    "注册新账号"
                  )}
                </button>
              )}
            </div>

            <p className="mt-6 text-center text-xs leading-5 text-kumo-subtle">
              登录与注册由 Kinde 托管；本应用不会接触或保存你的密码。
            </p>
          </div>
        </div>
      </main>
    </div>
  );
}
