import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Dialog, Input, Loader } from "@cloudflare/kumo";
import {
  CircleNotch,
  Fingerprint,
  PencilSimple,
  Plus,
  Trash,
} from "@phosphor-icons/react";
import {
  deletePasskey,
  fetchPasskeys,
  registerPasskey,
  renamePasskey,
  type PasskeySummary,
} from "../../api/passkey";
import { isPasskeySupported } from "../../lib/webauthn";
import { formatDateTime } from "../../lib/time";
import { ErrorState } from "../common/ErrorState";
import { Section, useToast } from "../ui";

const PASSKEYS_KEY = ["passkeys"] as const;

const TRANSPORT_LABELS: Record<string, string> = {
  usb: "USB",
  nfc: "NFC",
  ble: "蓝牙",
  hybrid: "跨设备",
  internal: "本机",
  "smart-card": "智能卡",
};

function transportLabel(value: string): string {
  return TRANSPORT_LABELS[value] ?? value;
}

function passkeyMeta(passkey: PasskeySummary): string[] {
  const parts: string[] = [];
  if (passkey.backup_eligible) {
    parts.push(passkey.backup_state ? "已同步（可备份）" : "可备份");
  } else {
    parts.push("仅本设备");
  }
  if (passkey.user_verified) parts.push("需要验证");
  if (passkey.transports.length > 0) {
    parts.push(passkey.transports.map(transportLabel).join(" / "));
  }
  return parts;
}

export function PasskeySection() {
  const toast = useToast();
  const queryClient = useQueryClient();
  const supported = isPasskeySupported();
  const query = useQuery({
    queryKey: PASSKEYS_KEY,
    queryFn: fetchPasskeys,
    retry: false,
    staleTime: 0,
  });

  const [name, setName] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState("");
  const [deleting, setDeleting] = useState<PasskeySummary | null>(null);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: PASSKEYS_KEY });
  };

  const create = useMutation({
    mutationFn: () => registerPasskey(name.trim() || undefined),
    onSuccess: () => {
      setName("");
      refresh();
      toast.success("通行密钥已添加");
    },
    onError: (error) => {
      if (error instanceof DOMException && error.name === "NotAllowedError") {
        toast.info("已取消添加通行密钥");
        return;
      }
      toast.error("添加通行密钥失败", error);
    },
  });

  const rename = useMutation({
    mutationFn: ({ id, value }: { id: string; value: string }) =>
      renamePasskey(id, value),
    onSuccess: () => {
      setEditingId(null);
      setEditingName("");
      refresh();
      toast.success("通行密钥已重命名");
    },
    onError: (error) => toast.error("重命名失败", error),
  });

  const remove = useMutation({
    mutationFn: (id: string) => deletePasskey(id),
    onSuccess: () => {
      setDeleting(null);
      refresh();
      toast.success("通行密钥已删除");
    },
    onError: (error) => toast.error("删除通行密钥失败", error),
  });

  const passkeys = query.data?.passkeys ?? [];

  return (
    <Section
      title="通行密钥"
      description="使用设备解锁（指纹、面容、PIN 或安全密钥）登录，无需输入密码。通行密钥只保存在你的设备上。"
    >
      {!supported ? (
        <p className="card-surface p-4 text-sm text-kumo-subtle">
          当前浏览器不支持通行密钥，或页面不是安全上下文（需要 HTTPS 或
          localhost）。仍可使用密码登录。
        </p>
      ) : (
        <div className="space-y-4">
          <div className="card-surface space-y-3 p-4">
            <h3 className="text-xs font-medium text-kumo-subtle">添加通行密钥</h3>
            <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
              <div className="space-y-2">
                <div className="text-xs font-medium text-kumo-subtle">
                  名称（可选）
                </div>
                <Input
                  aria-label="通行密钥名称"
                  placeholder="例如 MacBook 或 YubiKey"
                  maxLength={64}
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  disabled={create.isPending}
                />
              </div>
              <Button
                size="sm"
                onClick={() => create.mutate()}
                disabled={create.isPending}
              >
                {create.isPending ? (
                  <CircleNotch className="mr-1 size-3.5 animate-spin" />
                ) : (
                  <Plus className="mr-1 size-3.5" />
                )}
                添加通行密钥
              </Button>
            </div>
            <p className="text-xs text-kumo-subtle">
              浏览器会要求验证身份。建议至少添加两个通行密钥，避免设备丢失后无法登录。
            </p>
          </div>

          <div className="space-y-2">
            <h3 className="text-xs font-medium text-kumo-subtle">已添加的通行密钥</h3>
            {query.isLoading ? (
              <div className="flex justify-center py-4">
                <Loader size={20} />
              </div>
            ) : query.isError ? (
              <ErrorState
                message="加载通行密钥失败"
                onRetry={() => void query.refetch()}
              />
            ) : passkeys.length === 0 ? (
              <p className="rounded-lg bg-kumo-recessed p-3 text-xs text-kumo-subtle">
                暂无通行密钥。添加后即可在登录页无需密码登录。
              </p>
            ) : (
              passkeys.map((passkey) => {
                const isEditing = editingId === passkey.id;
                return (
                  <div
                    key={passkey.id}
                    className="rounded-lg bg-kumo-recessed p-2.5 text-xs"
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <Fingerprint className="size-4 text-kumo-subtle" weight="bold" />
                      {isEditing ? (
                        <>
                          <Input
                            aria-label="通行密钥名称"
                            className="h-8 w-48"
                            maxLength={64}
                            value={editingName}
                            onChange={(event) =>
                              setEditingName(event.target.value)
                            }
                            disabled={rename.isPending}
                          />
                          <Button
                            size="sm"
                            onClick={() =>
                              rename.mutate({
                                id: passkey.id,
                                value: editingName.trim(),
                              })
                            }
                            disabled={
                              rename.isPending || editingName.trim() === ""
                            }
                          >
                            保存
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => {
                              setEditingId(null);
                              setEditingName("");
                            }}
                            disabled={rename.isPending}
                          >
                            取消
                          </Button>
                        </>
                      ) : (
                        <>
                          <span className="font-medium">
                            {passkey.name || "未命名"}
                          </span>
                          {passkey.clone_warning && (
                            <span className="rounded-full bg-kumo-warning-tint px-2 py-0.5 text-kumo-warning">
                              签名计数异常
                            </span>
                          )}
                          <span className="flex-1" />
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => {
                              setEditingId(passkey.id);
                              setEditingName(passkey.name);
                            }}
                          >
                            <PencilSimple className="mr-1 size-3.5" />
                            重命名
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => setDeleting(passkey)}
                          >
                            <Trash className="mr-1 size-3.5" />
                            删除
                          </Button>
                        </>
                      )}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-kumo-subtle">
                      <span>添加于 {formatDateTime(passkey.created_at)}</span>
                      {passkey.last_used_at && (
                        <span>最后使用 {formatDateTime(passkey.last_used_at)}</span>
                      )}
                      {passkeyMeta(passkey).map((item) => (
                        <span key={item}>{item}</span>
                      ))}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      <Dialog.Root
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
      >
        <Dialog className="w-[min(92vw,26rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            删除通行密钥？
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            删除「{deleting?.name || "未命名"}」后将无法再用它登录。仍可使用密码或其他通行密钥登录。
          </Dialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              onClick={() => {
                if (deleting) remove.mutate(deleting.id);
              }}
              disabled={remove.isPending}
            >
              {remove.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              删除
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </Section>
  );
}
