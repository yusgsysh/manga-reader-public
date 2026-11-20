import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Dialog, Input, Loader } from "@cloudflare/kumo";
import {
  ArrowsClockwise,
  CircleNotch,
  Copy,
  ShieldCheck,
  ShieldSlash,
  WarningCircle,
} from "@phosphor-icons/react";
import {
  beginTotpSetup,
  disableTotp,
  enableTotp,
  fetchTotpStatus,
  regenerateRecoveryCodes,
} from "../../api/totp";
import type { TotpSetup } from "../../api/auth";
import { formatDateTime } from "../../lib/time";
import { ErrorState } from "../common/ErrorState";
import { Section, useToast } from "../ui";

const TOTP_KEY = ["totp", "status"] as const;

function RecoveryCodesPanel({
  codes,
  onDismiss,
}: {
  codes: string[];
  onDismiss?: () => void;
}) {
  const toast = useToast();
  const copyAll = async () => {
    try {
      await navigator.clipboard?.writeText(codes.join("\n"));
      toast.info("已复制恢复码");
    } catch {
      toast.error("复制失败，请手动保存");
    }
  };

  return (
    <div className="rounded-xl border border-kumo-warning/30 bg-kumo-warning-tint p-4">
      <div className="flex items-start gap-2">
        <WarningCircle className="mt-0.5 size-4 shrink-0 text-kumo-warning" />
        <div className="min-w-0">
          <div className="text-sm font-medium text-kumo-default">
            恢复码（只显示这一次）
          </div>
          <p className="mt-1 text-xs text-kumo-subtle">
            每个恢复码只能使用一次。请离线保存；丢失验证器和恢复码后将无法登录。
          </p>
        </div>
      </div>
      <div className="mt-3 grid gap-1.5 sm:grid-cols-2">
        {codes.map((code) => (
          <code
            key={code}
            className="rounded-lg bg-kumo-base px-2.5 py-1.5 font-mono text-xs tracking-[0.08em]"
          >
            {code}
          </code>
        ))}
      </div>
      <div className="mt-3 flex flex-wrap justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={() => void copyAll()}>
          <Copy className="mr-1 size-3.5" />
          复制全部
        </Button>
        {onDismiss && (
          <Button size="sm" onClick={onDismiss}>
            我已保存
          </Button>
        )}
      </div>
    </div>
  );
}

export function TotpSection() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: TOTP_KEY,
    queryFn: fetchTotpStatus,
    retry: false,
    staleTime: 0,
  });
  const status = query.data;
  const enabled = status?.enabled ?? false;

  const [setup, setSetup] = useState<TotpSetup | null>(null);
  const [enableCode, setEnableCode] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const [disableOpen, setDisableOpen] = useState(false);
  const [disablePassword, setDisablePassword] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [regenOpen, setRegenOpen] = useState(false);
  const [regenPassword, setRegenPassword] = useState("");
  const [regenCode, setRegenCode] = useState("");

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: TOTP_KEY });
  };

  const begin = useMutation({
    mutationFn: beginTotpSetup,
    onSuccess: (data) => {
      setSetup(data);
      setEnableCode("");
      setRecoveryCodes(null);
    },
    onError: (error) => toast.error("开始设置两步验证失败", error),
  });

  const enable = useMutation({
    mutationFn: () => enableTotp(enableCode.trim()),
    onSuccess: (result) => {
      setSetup(null);
      setEnableCode("");
      setRecoveryCodes(result.recovery_codes);
      refresh();
      toast.success("两步验证已启用", "请立即保存恢复码");
    },
    onError: (error) => toast.error("验证码无效，启用失败", error),
  });

  const disable = useMutation({
    mutationFn: () => disableTotp(disablePassword, disableCode.trim()),
    onSuccess: () => {
      setDisableOpen(false);
      setDisablePassword("");
      setDisableCode("");
      setRecoveryCodes(null);
      setSetup(null);
      refresh();
      toast.success("两步验证已关闭");
    },
    onError: (error) => toast.error("关闭两步验证失败", error),
  });

  const regenerate = useMutation({
    mutationFn: () => regenerateRecoveryCodes(regenPassword, regenCode.trim()),
    onSuccess: (result) => {
      setRegenOpen(false);
      setRegenPassword("");
      setRegenCode("");
      setRecoveryCodes(result.recovery_codes);
      refresh();
      toast.success("恢复码已重新生成", "旧恢复码已全部失效");
    },
    onError: (error) => toast.error("重新生成恢复码失败", error),
  });

  const closeDialogs = () => {
    if (disable.isPending || regenerate.isPending) return;
    setDisableOpen(false);
    setRegenOpen(false);
    setDisablePassword("");
    setDisableCode("");
    setRegenPassword("");
    setRegenCode("");
  };

  return (
    <Section
      title="两步验证"
      description="密码之外再要求一个身份验证器验证码（TOTP）。启用后仍可使用通行密钥登录。"
    >
      {query.isLoading ? (
        <div className="card-surface flex items-center gap-2 p-4 text-sm text-kumo-subtle">
          <Loader size={16} />
          正在读取…
        </div>
      ) : query.isError || !status ? (
        <ErrorState
          message="加载两步验证状态失败"
          onRetry={() => void query.refetch()}
        />
      ) : (
        <div className="space-y-4">
          {recoveryCodes && (
            <RecoveryCodesPanel
              codes={recoveryCodes}
              onDismiss={() => setRecoveryCodes(null)}
            />
          )}

          {enabled ? (
            <div className="card-surface space-y-3 p-4">
              <div className="flex flex-wrap items-center gap-2">
                <ShieldCheck className="size-5 text-kumo-success" weight="bold" />
                <span className="text-sm font-medium">已启用</span>
                {status.confirmed_at && (
                  <span className="text-xs text-kumo-subtle">
                    自 {formatDateTime(status.confirmed_at)}
                  </span>
                )}
                <span className="flex-1" />
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => setRegenOpen(true)}
                >
                  <ArrowsClockwise className="mr-1 size-3.5" />
                  重新生成恢复码
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => setDisableOpen(true)}
                >
                  <ShieldSlash className="mr-1 size-3.5" />
                  关闭
                </Button>
              </div>
              <p className="text-xs text-kumo-subtle">
                剩余恢复码：{status.recovery_codes_remaining} 个
                {status.recovery_codes_remaining <= 2 && (
                  <span className="ml-2 text-kumo-danger">
                    数量不足，请尽快重新生成。
                  </span>
                )}
              </p>
              <p className="text-xs text-kumo-subtle">
                登录时输入身份验证器中的 6 位验证码；验证器丢失时使用一个未使用的恢复码。
              </p>
            </div>
          ) : setup ? (
            <div className="card-surface space-y-4 p-4">
              <div className="flex items-center gap-2">
                <ShieldCheck className="size-5 text-[var(--app-accent)]" weight="bold" />
                <h3 className="text-sm font-medium">扫描二维码</h3>
              </div>
              <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
                {setup.qr_data_url ? (
                  <img
                    src={setup.qr_data_url}
                    alt="两步验证二维码"
                    width={200}
                    height={200}
                    className="size-[200px] shrink-0 rounded-xl bg-white p-2"
                  />
                ) : null}
                <div className="min-w-0 space-y-2 text-xs text-kumo-subtle">
                  <p>
                    使用 Google Authenticator、1Password、Bitwarden 等应用扫描二维码。
                  </p>
                  <p>无法扫描时手动输入：</p>
                  <code className="block break-all rounded-lg bg-kumo-recessed p-2 font-mono text-sm text-kumo-default">
                    {setup.secret}
                  </code>
                  <p className="break-all">
                    账户：{setup.account_name} · 发行方：{setup.issuer}
                  </p>
                </div>
              </div>

              <div className="space-y-2 border-t border-kumo-hairline pt-3">
                <div className="text-xs font-medium text-kumo-subtle">
                  输入验证器当前显示的 6 位验证码
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Input
                    aria-label="验证码"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    maxLength={6}
                    placeholder="000000"
                    className="h-10 w-32 text-center tracking-[0.2em]"
                    value={enableCode}
                    onChange={(event) => setEnableCode(event.target.value)}
                    disabled={enable.isPending}
                  />
                  <Button
                    size="sm"
                    onClick={() => enable.mutate()}
                    disabled={enable.isPending || enableCode.trim().length !== 6}
                  >
                    {enable.isPending ? (
                      <CircleNotch className="mr-1 size-3.5 animate-spin" />
                    ) : (
                      <ShieldCheck className="mr-1 size-3.5" />
                    )}
                    确认启用
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      setSetup(null);
                      setEnableCode("");
                    }}
                    disabled={enable.isPending}
                  >
                    取消
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <div className="card-surface space-y-3 p-4">
              <div className="flex items-center gap-2">
                <ShieldSlash className="size-5 text-kumo-subtle" weight="bold" />
                <span className="text-sm font-medium">未启用</span>
              </div>
              <p className="text-xs text-kumo-subtle">
                启用后，密码登录会要求额外输入验证器验证码；通行密钥登录不受影响。
              </p>
              <div className="flex justify-end">
                <Button
                  size="sm"
                  onClick={() => begin.mutate()}
                  disabled={begin.isPending}
                >
                  {begin.isPending ? (
                    <CircleNotch className="mr-1 size-3.5 animate-spin" />
                  ) : (
                    <ShieldCheck className="mr-1 size-3.5" />
                  )}
                  启用两步验证
                </Button>
              </div>
            </div>
          )}
        </div>
      )}

      <Dialog.Root
        open={disableOpen}
        onOpenChange={(open) => {
          if (!open) closeDialogs();
        }}
      >
        <Dialog className="w-[min(92vw,26rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            关闭两步验证？
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            关闭后仅凭密码即可登录。请输入当前密码以及一个验证码或恢复码确认。
          </Dialog.Description>
          <Input
            aria-label="当前密码"
            type="password"
            autoComplete="current-password"
            className="mt-3 h-10 w-full"
            placeholder="当前密码"
            value={disablePassword}
            onChange={(event) => setDisablePassword(event.target.value)}
            disabled={disable.isPending}
          />
          <Input
            aria-label="验证码或恢复码"
            autoComplete="one-time-code"
            className="mt-2 h-10 w-full text-center tracking-[0.1em]"
            placeholder="验证码或恢复码"
            value={disableCode}
            onChange={(event) => setDisableCode(event.target.value)}
            disabled={disable.isPending}
          />
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" onClick={closeDialogs} disabled={disable.isPending}>
              取消
            </Button>
            <Button
              onClick={() => disable.mutate()}
              disabled={
                disable.isPending ||
                disablePassword === "" ||
                disableCode.trim() === ""
              }
            >
              {disable.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              关闭两步验证
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={regenOpen}
        onOpenChange={(open) => {
          if (!open) closeDialogs();
        }}
      >
        <Dialog className="w-[min(92vw,28rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            重新生成恢复码？
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            旧恢复码会立即全部失效。需要密码和当前验证码确认。
          </Dialog.Description>
          <div className="mt-3 space-y-2">
            <Input
              aria-label="当前密码"
              type="password"
              autoComplete="current-password"
              className="h-10 w-full"
              placeholder="当前密码"
              value={regenPassword}
              onChange={(event) => setRegenPassword(event.target.value)}
              disabled={regenerate.isPending}
            />
            <Input
              aria-label="验证码"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={6}
              className="h-10 w-full text-center tracking-[0.2em]"
              placeholder="身份验证器验证码"
              value={regenCode}
              onChange={(event) => setRegenCode(event.target.value)}
              disabled={regenerate.isPending}
            />
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" onClick={closeDialogs} disabled={regenerate.isPending}>
              取消
            </Button>
            <Button
              onClick={() => regenerate.mutate()}
              disabled={
                regenerate.isPending ||
                regenPassword === "" ||
                regenCode.trim().length !== 6
              }
            >
              {regenerate.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              生成新的恢复码
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </Section>
  );
}
