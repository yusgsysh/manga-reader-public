import { Link, useNavigate } from "react-router";
import { useState } from "react";
import { List, MagnifyingGlass } from "@phosphor-icons/react";
import { IconButton, Logo } from "../ui";
import { MobileDrawer } from "./MobileDrawer";

export function Header() {
  const navigate = useNavigate();
  const [drawerOpen, setDrawerOpen] = useState(false);

  return (
    <>
      {/* No backdrop-blur: it is recomputed every scroll frame and is one of
          the worst frame-drop sources on low-end GPUs. Near-opaque gives the
          same look without the cost. */}
      <header className="sticky top-0 z-50 flex h-14 items-center gap-2 border-b border-kumo-hairline bg-kumo-base/95 px-4 pt-[env(safe-area-inset-top)] lg:hidden">
        <IconButton label="打开菜单" onClick={() => setDrawerOpen(true)}>
          <List className="size-5" weight="bold" />
        </IconButton>

        <Link
          to="/"
          className="flex flex-1 items-center justify-center"
          aria-label="Manga Reader 首页"
        >
          <Logo />
        </Link>

        <IconButton label="搜索" onClick={() => navigate("/search")}>
          <MagnifyingGlass className="size-5" weight="bold" />
        </IconButton>
      </header>

      <MobileDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </>
  );
}
