import { useEffect, useState } from "react";
import { Button, Dialog, Loader } from "@cloudflare/kumo";
import { DownloadSimple, Magnet } from "@phosphor-icons/react";
import {
  fetchGalleryTorrentInfo,
  fetchGalleryTorrents,
  galleryTorrentDownloadUrl,
} from "../../api/gallery";
import type { GalleryTorrent, GalleryTorrentInfo } from "../../types/gallery";

interface GalleryTorrentsDialogProps {
  id: number;
  token: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const DOWNLOAD_LINK_CLASS =
  "inline-flex items-center gap-1.5 rounded-lg border border-kumo-hairline px-2.5 py-1 text-xs font-medium text-kumo-default transition-colors hover:bg-kumo-tint";

function TorrentRow({
  id,
  token,
  torrent,
}: {
  id: number;
  token: string;
  torrent: GalleryTorrent;
}) {
  const [openInfo, setOpenInfo] = useState(false);
  const [info, setInfo] = useState<GalleryTorrentInfo | null>(null);
  const [infoError, setInfoError] = useState<string | null>(null);
  const [infoLoading, setInfoLoading] = useState(false);

  const toggleInfo = () => {
    const next = !openInfo;
    setOpenInfo(next);
    if (next && !info && !infoLoading) {
      setInfoLoading(true);
      setInfoError(null);
      fetchGalleryTorrentInfo(id, token, torrent.gtid)
        .then(setInfo)
        .catch((e) =>
          setInfoError(e instanceof Error ? e.message : String(e)),
        )
        .finally(() => setInfoLoading(false));
    }
  };

  return (
    <div className="rounded-xl border border-kumo-hairline p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium" title={torrent.name}>
            {torrent.name}
          </p>
          <p className="mt-1 text-xs text-kumo-subtle">
            {torrent.size} · {torrent.posted} · {torrent.uploader}
          </p>
          <p className="mt-1 text-xs text-kumo-subtle">
            做种 <span className="tnum">{torrent.seeds}</span> · 下载中{" "}
            <span className="tnum">{torrent.peers}</span> · 完成{" "}
            <span className="tnum">{torrent.downloads}</span>
          </p>
        </div>

        <div className="flex shrink-0 flex-col items-end gap-2">
          <a
            href={galleryTorrentDownloadUrl(id, token, torrent.gtid)}
            className={DOWNLOAD_LINK_CLASS}
          >
            <DownloadSimple className="size-3.5" weight="bold" />
            下载
          </a>
          <button
            type="button"
            onClick={toggleInfo}
            className="text-xs text-kumo-subtle transition-colors hover:text-kumo-default"
          >
            {openInfo ? "收起详情" : "Information"}
          </button>
        </div>
      </div>

      {openInfo && (
        <div className="mt-3 border-t border-kumo-hairline pt-3 text-xs text-kumo-subtle">
          {infoLoading ? (
            <div className="flex justify-center py-3">
              <Loader size={18} />
            </div>
          ) : infoError ? (
            <p className="text-kumo-danger">{infoError}</p>
          ) : info ? (
            <div className="space-y-2">
              <div className="grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-3">
                <span>发布：{info.posted}</span>
                <span>大小：{info.size}</span>
                <span>上传者：{info.uploader}</span>
                <span>做种：{info.seeds}</span>
                <span>下载中：{info.dlers}</span>
                <span>完成：{info.completes}</span>
              </div>
              <p className="text-kumo-default/80">{info.comments}</p>
              {info.personalized && (
                <div className="flex justify-center pt-1">
                  <a
                    href={galleryTorrentDownloadUrl(
                      id,
                      token,
                      torrent.gtid,
                      "personalized",
                    )}
                    className={DOWNLOAD_LINK_CLASS}
                  >
                    <DownloadSimple className="size-3.5" weight="bold" />
                    个性化种子
                  </a>
                </div>
              )}
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}

export function GalleryTorrentsDialog({
  id,
  token,
  open,
  onOpenChange,
}: GalleryTorrentsDialogProps) {
  const [torrents, setTorrents] = useState<GalleryTorrent[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleOpenChange = (next: boolean) => {
    if (next) {
      setLoading(true);
      setError(null);
      setTorrents(null);
    }
    onOpenChange(next);
  };

  // Live fetch on each open; intentionally no query cache.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    fetchGalleryTorrents(id, token)
      .then((resp) => {
        if (!cancelled) setTorrents(resp.torrents);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, id, token]);

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog className="w-[min(94vw,40rem)] p-5">
        <Dialog.Title className="flex items-center gap-2 text-base font-semibold">
          <Magnet className="size-4" weight="bold" />
          种子下载
          {torrents && torrents.length > 0 && (
            <span className="text-sm font-normal text-kumo-subtle">
              ({torrents.length})
            </span>
          )}
        </Dialog.Title>

        <div className="mt-4 max-h-[60vh] space-y-3 overflow-y-auto">
          {loading && (
            <div className="flex justify-center py-8">
              <Loader size={24} />
            </div>
          )}
          {error && <p className="py-4 text-sm text-kumo-danger">{error}</p>}
          {!loading && !error && torrents && torrents.length === 0 && (
            <p className="py-6 text-center text-sm text-kumo-subtle">
              暂无种子
            </p>
          )}
          {torrents?.map((torrent) => (
            <TorrentRow
              key={torrent.gtid}
              id={id}
              token={token}
              torrent={torrent}
            />
          ))}
        </div>

        <div className="mt-4 flex justify-end">
          <Dialog.Close render={<Button variant="secondary">关闭</Button>} />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
