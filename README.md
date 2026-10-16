<p align="center">
  <img src="resources/icon.png" width="128" alt="Shelf icon">
</p>

<h1 align="center">Shelf</h1>

<p align="center">
  A native macOS shelf that lives in the notch — it peeks out when you hover or drag near it and unfolds into a black panel fused with the hardware notch. Stash files, text and screenshots, then drag them anywhere.<br>
  住在刘海里的 macOS 临时托盘——悬停或拖拽靠近时探头提示，展开成与硬件刘海融为一体的黑色面板；暂存文件、文字和截图，随用随拖走。
</p>

<p align="center">
  <a href="https://github.com/henryyu333/shelf/releases/latest/download/Shelf.dmg"><img src="https://img.shields.io/badge/Download_for_macOS-Shelf.dmg-007AFF?style=for-the-badge&logo=apple&logoColor=white" alt="Download Shelf.dmg for macOS"></a>
</p>

<p align="center">
  Apple Silicon · macOS 13+ · Free and open source · <a href="#install">Install guide</a> · <a href="https://github.com/henryyu333/shelf/releases/latest">All downloads</a><br>
  Apple Silicon 芯片 · macOS 13 或以上 · 免费开源 · <a href="#安装">安装说明</a> · <a href="https://github.com/henryyu333/shelf/releases/latest">全部下载</a>
</p>

<p align="center">
  <img src="docs/images/demo.gif" width="880" alt="Shelf slides down from the top of the screen; a copied text lands on top; a PDF is dragged from the shelf into a Finder folder and the shelf hides">
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/macOS-13%2B%20Apple%20Silicon-blue.svg" alt="macOS 13+ Apple Silicon">
  <img src="https://img.shields.io/badge/built%20with-Go-00ADD8.svg" alt="Built with Go">
</p>

<p align="center">
  <a href="#english">English</a> | <a href="#简体中文">简体中文</a>
</p>

---

<a id="english"></a>

## Install

Requires a Mac with Apple Silicon (M1 or later) running macOS 13 or later. Intel Macs are not supported.

1. Download **[Shelf.dmg](https://github.com/henryyu333/shelf/releases/latest/download/Shelf.dmg)**.
2. Open the downloaded file and drag **Shelf** onto the **Applications** folder.
3. Open Shelf from Applications. The first time, macOS will stop it — follow [First launch](#first-launch) below. You only do this once.
4. Shelf lives in the menu bar, not the Dock. Press `⌃⌥Space` to show the shelf.

Prefer a zip? Every release on the [Releases page](https://github.com/henryyu333/shelf/releases/latest) also has a `.zip` of the same version.

<a id="first-launch"></a>

### First launch: "Shelf Not Opened"

Shelf is free and not notarized by Apple, so macOS asks you to confirm it once. Only do this for a copy you downloaded from this repository.

**macOS 15 or later**

1. Double-click Shelf in Applications. When macOS says Shelf was not opened, click **Done** (not Move to Trash).
2. Open **System Settings → Privacy & Security** and scroll down to **Security**.
3. Next to "Shelf was blocked to protect your Mac", click **Open Anyway**.
4. Enter your password or use Touch ID, then click **Open Anyway** once more.

**macOS 13 or 14**

Control-click (or right-click) Shelf in Applications, choose **Open**, then click **Open** in the dialog.

From then on Shelf opens normally. If macOS ever says the app **is damaged**, don't bypass it — delete it and download it again from the Releases page. Apple's guide: [Safely open apps on your Mac](https://support.apple.com/102445).

### Optional

- **Start at login**: System Settings → General → Login Items → click **+** and choose Shelf.
- **Uninstall**: right-click the menu-bar icon → Quit, then move Shelf from Applications to the Trash. To also delete your saved items, remove `~/Library/Application Support/Shelf/`.

## Features

- A transparent notch surface stays anchored at the top; hovering reveals a small black pill without moving the window
- Hover the notch and the shelf peeks; click the peeked notch and it unfolds into a pure-black panel fused with the hardware notch
- Drag files near the notch and the shelf unfolds; drop onto the peeked pill to stash instantly
- While dragging, shake the pointer left and right to summon a floating basket beside the cursor — drop into it to stash
- Global hotkey ⌃⌥Space to show or hide the shelf; clicking outside collapses it back into the notch
- Drag files, folders, text and images in — and drag them out to any app
- Clipboard text history: keeps the last 30 items, duplicates move to the top instead of creating copies
- Long text is shown as a short preview; copying or dragging keeps the complete text
- Screenshots and clipboard images land in the shelf automatically
- Skips passwords copied from password managers that mark them as private
- Contents persist across restarts
- Stores file references only — your files are never copied
- Select several items with ⌘ or ⇧ and drag them out together
- Right-click an item to open it, show it in Finder, Quick Look it, AirDrop it or copy its text or path
- Optional automatic cleanup of items older than 1, 7 or 30 days
- Fully local: zero network requests, no telemetry

## Usage
| Want to | Do this |
| --- | --- |
| Show or hide the shelf | Press `⌃⌥Space`, click the menu-bar icon, or hover the notch and click the peeked pill |
| Peek at the shelf | Move the pointer onto the notch (no drag needed) — it bulges out; click it to unfold |
| Stash what you're dragging | While dragging anything, move near the notch — the shelf expands instantly; you can also drop right onto the peeked notch |
| Summon the drop basket | While dragging, shake the pointer left and right — a basket card appears beside the cursor; drop onto it |
| Send files or text to the shelf | Right-click selected items in Finder → Services/Quick Actions → "添加到 Shelf" (Add to Shelf); or select text in any app → app menu → Services → "添加到 Shelf" |
| Drag an item out | Drag it from the shelf into the target app |
| Copy a text item back | Click it once, then paste (`⌘V`) anywhere |
| Remove an item | Click the × on its right; file references never delete the original file |
| Select several items | ⌘-click to add or remove one, ⇧-click to select a range |
| Drag several items out | Select them, then drag any one of them |
| More actions | Right-click an item: open, show in Finder, Quick Look, AirDrop, copy, remove |
| Quick Look | Select files or images and press Space; Space or `Esc` closes it |
| Clean up old items | Right-click the menu-bar icon, choose the automatic cleanup period |
| Clear all items | Click the clear button in the header; file references never delete the original file |
| Collapse the shelf | Press `⌃⌥Space`, `Esc`, the collapse button, or click anywhere outside it |
| Turn on notch drop protection (optional) | Right-click the menu-bar icon, choose the notch drop protection item, then allow Shelf in System Settings; turn it on again after each update |
| Quit Shelf | Right-click the menu-bar icon → Quit |

## Privacy

Everything stays on your Mac: no network requests, no telemetry, no cloud sync. Text history is stored in plaintext at `~/Library/Application Support/Shelf/`, so avoid copying secrets you don't want recorded there.

## Build from source

Requires Go 1.27 or later and Xcode Command Line Tools:

```sh
go run github.com/egoist/mygo/cmd/mygo build
```

The packaged `Shelf.app` and a `.dmg` land in a `darwin-arm64` subfolder under the output directory configured in `mygo.json`.

## Docs

- [docs/MANUAL.zh-CN.md](docs/MANUAL.zh-CN.md) — full user manual (in Chinese)

## License

MIT — see [LICENSE](LICENSE).

---

<a id="简体中文"></a>

## 安装

需要 Apple Silicon 芯片（M1 或更新）的 Mac，系统 macOS 13 或以上。不支持 Intel 芯片的 Mac。

1. 下载 **[Shelf.dmg](https://github.com/henryyu333/shelf/releases/latest/download/Shelf.dmg)**。
2. 打开下载好的文件，把 **Shelf** 拖到 **Applications（应用程序）** 文件夹上。
3. 从“应用程序”里打开 Shelf。第一次打开会被系统拦下，按下面的[第一次打开](#第一次打开)操作即可，只需要做一次。
4. Shelf 住在屏幕顶部的菜单栏里，不在 Dock 上。按 `⌃⌥Space` 打开托盘。

想要 zip 压缩包？[Releases 页面](https://github.com/henryyu333/shelf/releases/latest)的每个版本里也有同一版本的 `.zip`。

<a id="第一次打开"></a>

### 第一次打开：提示“未打开 Shelf”

Shelf 是免费软件，没有经过 Apple 公证，所以 macOS 会让你确认一次。请只对从本仓库下载的版本这样做。

**macOS 15 或更新**

1. 在“应用程序”里双击 Shelf。系统提示未打开时，点 **完成**（不要点“移到废纸篓”）。
2. 打开 **系统设置 → 隐私与安全性**，往下滚动到 **安全性**。
3. 在“已阻止 Shelf 以保护 Mac”旁边，点 **仍要打开**。
4. 输入开机密码或用触控 ID 确认，再点一次 **仍要打开**。

**macOS 13 或 14**

在“应用程序”里按住 Control 点按（或右键点按）Shelf，选 **打开**，再在弹窗里点 **打开**。

之后就能正常打开了。如果系统提示 App **已损坏**，不要绕过，删掉后从 Releases 页面重新下载。Apple 官方说明：[在 Mac 上安全地打开 App](https://support.apple.com/zh-cn/102445)。

### 可选

- **开机自动启动**：系统设置 → 通用 → 登录项 → 点 **+**，选择 Shelf。
- **卸载**：右键菜单栏图标 → 退出 Shelf，再把“应用程序”里的 Shelf 移到废纸篓。如果连保存的内容也要删除，删掉 `~/Library/Application Support/Shelf/` 文件夹。

## 功能

- 刘海附近常驻透明接收区，悬停只改变胶囊画面，不再反复移动或缩放窗口
- 悬停刘海即探头，单击探头展开成与硬件刘海融为一体的纯黑面板
- 拖着内容靠近刘海时托盘会探头并展开；也可以直接拖到探头胶囊上松开
- 拖拽中左右快速摇晃指针，光标旁会唤出一个悬浮篮子——拖到上面松开即暂存
- 全局快捷键 ⌃⌥Space 随时显示或收起；点击托盘外任意处自动缩回刘海
- 文件、文件夹、文字、图片都可以拖进来，再拖到任意应用
- 剪贴板文字历史：最多保留 30 条，重复内容移到顶部而不新增条目
- 长文字只缩短列表摘要，复制和拖出仍保留完整正文
- 截图和剪贴板图片自动进入托盘
- 密码管理器标记为隐私的密码不会被记录
- 重启后内容保留
- 只记录文件引用，绝不复制你的文件
- 按住 ⌘ 或 ⇧ 多选，一次拖出多个
- 右键项目可以打开、在 Finder 中显示、快速查看、隔空投送、复制文字或路径
- 可选自动清理超过 1 天、7 天或 30 天的内容
- 完全本地运行：零网络请求，无遥测

## 使用

| 想做什么 | 操作 |
| --- | --- |
| 显示或收起托盘 | 按 `⌃⌥Space`、单击菜单栏图标，或悬停刘海后单击探头胶囊 |
| 探头预览 | 空手把指针移到刘海上——它会向下鼓出；单击探头胶囊即展开 |
| 暂存正在拖的内容 | 拖着任何东西靠近刘海触发区，托盘瞬间展开；也可以直接拖到探头刘海上松开 |
| 唤出暂存篮 | 拖拽中左右快速摇晃指针，光标旁出现篮子卡片；拖到上面松开即可 |
| 从 Finder 或服务菜单收进托盘 | 在 Finder 右键选中的文件或文件夹 → 服务/快速操作 → “添加到 Shelf”；或在任意应用选中文字 → 应用菜单 → 服务 → “添加到 Shelf” |
| 拖出项目 | 从托盘中拖住该项，到目标应用中松开 |
| 把文字复制回来 | 单击该文字项，然后在任意应用中粘贴（`⌘V`） |
| 移除项目 | 点击该项右侧的 ×；文件引用不会删除原文件 |
| 选中多个项目 | 按住 `⌘` 单击逐个选，按住 `⇧` 单击选一段 |
| 一次拖出多个 | 选中后拖住其中任意一个 |
| 更多操作 | 右键项目：打开、在 Finder 中显示、快速查看、隔空投送、复制、移除 |
| 快速查看 | 选中文件或图片后按空格；再按空格或 `Esc` 关闭 |
| 自动清理旧内容 | 右键菜单栏图标 → 自动清理，选择期限 |
| 清空全部项目 | 点击托盘顶部的“清空”；文件引用不会删除原文件 |
| 收起托盘 | 按 `⌃⌥Space`、`Esc`、“收起”，或点击托盘外任意处 |
| 开启刘海拖放保护（可选） | 右键菜单栏图标 → 开启刘海拖放保护…，再在系统设置里打开 Shelf；每次更新后重新开启一次 |
| 退出 Shelf | 右键菜单栏图标 → 退出 Shelf |

## 隐私

一切都在本机：不联网、无遥测、无云同步。文字历史以明文保存在 `~/Library/Application Support/Shelf/`，不希望被记录的密码、验证码等内容请先退出 Shelf 再复制。

## 从源码构建

需要 Go 1.27 或以上，以及 Xcode Command Line Tools：

```sh
go run github.com/egoist/mygo/cmd/mygo build
```

打包好的 `Shelf.app` 和 `.dmg` 在 `mygo.json` 配置的输出目录下的 `darwin-arm64` 子文件夹中。

## 文档

- [docs/MANUAL.zh-CN.md](docs/MANUAL.zh-CN.md) — 完整中文使用手册（含日常使用、数据位置、开机启动、修改快捷键）

## 许可

MIT — 见 [LICENSE](LICENSE)。
