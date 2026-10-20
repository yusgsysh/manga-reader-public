# 安装（手动）

三平台归档内是一个可执行文件；macOS 与 Windows 解压即用。Linux 下想让启动器和任务栏显示图标，按下面把文件放到标准位置（也可以直接运行 `./manga-reader`，不影响功能）：

```sh
# 1. 二进制
sudo install -m755 manga-reader /usr/local/bin/

# 2. 图标（512×512 PNG）
sudo install -m644 linux/manga-reader.png /usr/share/icons/hicolor/512x512/apps/

# 3. 桌面入口（应用列表 / 启动器）
sudo desktop-file-install linux/manga-reader.desktop
# 或手动：
#   sudo install -m644 linux/manga-reader.desktop /usr/share/applications/
#   sudo update-desktop-database /usr/share/applications/
```

用户级安装（免 sudo）：把图标放到 `~/.local/share/icons/hicolor/512x512/apps/`、桌面入口放到 `~/.local/share/applications/`，二进制放在 `$HOME/bin` 并把 `Exec=` 改成绝对路径。

Windows：`manga-reader.exe` 可以直接固定到任务栏，图标由 exe 内嵌的窗口图标提供。macOS：把二进制拖进终端运行，或自行包一层 `.app`（`Contents/MacOS/manga-reader`）。

数据位置：配置与 Cookie 在 `~/.config/manga-reader/`（图片缓存默认在 `~/.cache/manga-reader/images/`，崩溃日志 `crash.log` 也在配置目录）。
