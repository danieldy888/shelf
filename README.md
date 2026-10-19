<p align="center">
  <img src="resources/icon.png" width="128" alt="Shelf icon">
</p>

<h1 align="center">Shelf</h1>

<p align="center">
  The MacBook notch, turned into a shelf. Drop files, text and screenshots into it, drag them out wherever you need them, and keep your clipboard and music one hover away.<br>
  把 MacBook 的刘海变成一个随手放东西的托盘：文件、文字、截图往里一放，要用时再拖出来；剪贴板和正在播放的音乐，鼠标移过去就在。
</p>

<p align="center">
  <a href="https://github.com/henryyu333/shelf/releases/latest/download/Shelf.dmg"><img src="https://img.shields.io/badge/Download_for_macOS-Shelf.dmg-007AFF?style=for-the-badge&logo=apple&logoColor=white" alt="Download Shelf.dmg for macOS"></a>
</p>

<p align="center">
  Apple Silicon · macOS 13+ · Free and open source · <a href="#install">Install guide</a> · <a href="https://github.com/henryyu333/shelf/releases/latest">All downloads</a><br>
  Apple Silicon 芯片 · macOS 13 或以上 · 免费开源 · <a href="#安装">安装说明</a> · <a href="https://github.com/henryyu333/shelf/releases/latest">全部下载</a>
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

## What is Shelf

When you move files between apps, you usually need somewhere to put them for a moment. Shelf puts that place in the notch. It stays out of sight until you need it: hover the notch or drag something toward it, and a black panel grows out of the notch, so it looks like part of the hardware. Put things in, take them out, and it goes back into the notch.

It lives in the menu bar, has no Dock icon, keeps everything on your Mac, and never copies your files — it only remembers where they are.

## Highlights

**A shelf in the notch**
- Drag files, folders, images or text toward the notch — it shows "release to drop" (松开放入), and the shelf opens as you let go.
- Files show as thumbnails. Drag one or several (⌘/⇧ to select) straight into any app.
- Press Space for Quick Look; right-click to open, reveal in Finder, AirDrop, copy, pin or remove — pictures get an extra "复制图片" (Copy Image) entry that copies the image itself.
- Other ways in: shake the pointer while dragging to summon a drop basket beside the cursor, or use Services → "添加到 Shelf" (Add to Shelf) in Finder and other apps.

**Clipboard history**
- Text you copy is kept automatically, along with the app it came from. Screenshots and copied images land on the shelf too.
- Press `⌃⌥V` to search: type to filter, ↑/↓ to choose, ⏎ copies it and takes you back to the app you were in.
- Pin what matters. Exclude apps you never want recorded. Passwords that password managers mark as private are skipped.

**Now playing**
- Hover the notch while music plays and it becomes a small player: cover, progress, previous / play-pause / next.
- The open shelf shows the same controls at the top.

**AI usage** (optional, off by default)
- Enable it in Settings → AI 用量 and the shelf gains a third page: subscription quota for Codex and Claude Code — a double ring per card, outer weekly, inner 5-hour — plus this Mac's token usage for omp, Claude Code and Codex, as a stacked chart over 今天 / 7 天 / 30 天 / 全部.
- Quota checks read the login info those tools already saved on this Mac and send it only to the tools' own endpoints (chatgpt.com, api.anthropic.com) — never anywhere else, and never to sign in or refresh a login.

**Quiet by design**
- One shortcut, `⌃⌥Space`, opens or closes it; clicking anywhere else tucks it away.
- A settings window holds every option: start at login, clipboard history size, excluded apps, automatic cleanup, now playing, AI usage, updates.
- Updates are checked against this repository's GitHub releases. Shelf tells you when one is out and installs it only when you say so.

<a id="install"></a>

## Install

Requires a Mac with Apple Silicon (M1 or later) running macOS 13 or later. Intel Macs are not supported.

1. Download **[Shelf.dmg](https://github.com/henryyu333/shelf/releases/latest/download/Shelf.dmg)**.
2. Open the downloaded file and drag **Shelf** onto the **Applications** folder.
3. Open Shelf from Applications. The first time, macOS will stop it — follow [First launch](#first-launch) below. You only do this once.
4. Shelf lives in the menu bar, not the Dock. Hover the notch, or press `⌃⌥Space`.

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

- **Start at login**: right-click the menu-bar icon → Settings → General → Open at login.
- **Notch drop protection**: Settings → General → Notch drop protection → 开启… (Turn On), then allow Shelf under System Settings → Privacy & Security → Accessibility. While you drag toward the notch, the pointer stays in the drop area instead of hitting the very top edge of the screen. Optional; everyday use needs no permissions. Turn it on again after each update.
- **Uninstall**: right-click the menu-bar icon → Quit, then move Shelf from Applications to the Trash. To also delete your saved items, remove `~/Library/Application Support/Shelf/`.

## Everyday use

| To | Do this |
| --- | --- |
| Open or close the shelf | Press `⌃⌥Space`, click the menu-bar icon, or hover the notch and click it |
| Put something in | Drag it toward the notch and let go; or shake the pointer while dragging and drop on the basket |
| Take something out | Drag it from the shelf into any app; ⌘/⇧-click to take several at once |
| Copy text back | Click a text item, then paste with `⌘V` |
| Copy image data | Select one picture and press `⌘C`, or right-click it → 复制图片 (Copy Image) — the clipboard gets PNG data, not the path |
| Search what you copied | Press `⌃⌥V`, type, pick with ↑/↓, press ⏎ |
| Switch lists | Click 托盘 (Tray), 剪贴板 (Clipboard) — or AI 用量 when enabled — at the top of the shelf |
| Control music | Hover the notch while music plays, or use the card at the top of the open shelf |
| Pin, remove, Quick Look, AirDrop | Right-click an item (Space also opens Quick Look) |
| Clear the shelf | Click the trash icon at the top — pinned items stay; original files are never deleted |
| Change settings | Right-click the menu-bar icon → Settings |
| Check for updates | Right-click the menu-bar icon → 检查更新… (Check for Updates) |

The full manual is in [docs/MANUAL.zh-CN.md](docs/MANUAL.zh-CN.md) (Chinese).

## Privacy

Everything stays on your Mac: no account, no telemetry, no cloud sync. The only network requests are the update check — shortly after launch and then once a day, Shelf asks this repository's latest GitHub release whether a newer version exists — and, only if you enable the optional AI usage page, its quota lookups to the tools' own endpoints (nothing else is sent, and logins are never refreshed). The update request sends nothing but the app version in the standard User-Agent header. Turn it off in Settings → General → 自动检查更新 (Check for updates automatically); checking by hand from the menu still works.

Clipboard text is stored in plain text in `~/Library/Application Support/Shelf/`. If you are about to copy something you don't want kept, pause recording from the menu-bar icon → 剪贴板记录 (Clipboard History), or exclude the app it comes from.

## Build from source

Requires Go 1.27 or later and Xcode Command Line Tools:

```sh
go run github.com/egoist/mygo/cmd/mygo build
```

The packaged `Shelf.app` and a `.dmg` land in a `darwin-arm64` subfolder under the output directory configured in `mygo.json`. Shelf is written in Go with [MyGo](https://github.com/egoist/mygo).

## License

MIT — see [LICENSE](LICENSE).

---

<a id="简体中文"></a>

## Shelf 是什么

在应用之间搬东西时，总需要一个临时放一下的地方。Shelf 把这个地方放进了刘海里。平时它不露面；鼠标移到刘海上，或者拖着东西靠近刘海，一块纯黑的面板就从刘海里长出来，看起来和硬件连成一体。放进去、拿出来，用完它就缩回刘海。

它只在菜单栏里，不占 Dock；所有内容都留在你的 Mac 上；放进来的文件不会被复制，Shelf 只记住它们在哪儿。

## 亮点

**刘海里的托盘**
- 把文件、文件夹、图片或文字往刘海拖，刘海会提示「松开放入」，一松手托盘就打开。
- 文件以缩略图显示（iPhone 拍的 HEIC 照片也有预览），可以一次拖出一个或多个（按住 ⌘ 或 ⇧ 多选），直接拖进任何应用；也可以在设置里让拖出去的东西自动离开托盘。
- 按空格快速查看；右键可以打开、在 Finder 中显示、隔空投送、复制（图片多一项「复制图片」，复制图片数据本身）、置顶或移除。
- 图片和 PDF 可以直接转换：右键图片 →「转换为」JPEG、HEIC、PNG、PDF，或识别图里的文字生成 Word，多张还能合并成一个 PDF；大图可以一键缩小；右键 PDF 可以提取文字生成 Word，或每页存成一张图片。文件和文件夹可以压缩成 zip。全部在本机完成，原文件不动，做好的文件放在托盘最前面。
- 还有别的放法：拖着东西时左右晃动指针，光标旁会出现一个暂存篮；也可以在 Finder 或其他应用里右键 →「服务」→「添加到 Shelf」。

**剪贴板记录**
- 复制过的文字会自动收好，并记住来自哪个应用；截图和复制的图片也会进托盘。
- 按 `⌃⌥V` 搜索：输入即筛选，`↑`/`↓` 选择，回车复制并回到你原来的应用。
- 重要的内容可以置顶；不想被记录的应用可以排除；密码管理器标记为隐私的密码不会被记录。

**正在播放**
- 放音乐时把鼠标移到刘海上，它会变成一个小播放器：封面、进度、上一首 / 播放暂停 / 下一首。
- 打开托盘后，上方也有一条播放条，托盘和剪贴板两页都能看到。

**AI 用量**（可选，默认关闭）
- 在设置 → AI 用量里打开「在刘海里显示 AI 用量」，刘海会多出第三页：Codex、Claude Code 的套餐剩余额度（每张卡内外双圈：外圈每周、内圈 5 小时），以及这台电脑上 omp、Claude Code、Codex 的 token 用量（堆叠柱图，按 今天 / 7 天 / 30 天 / 全部 切换；额度卡和本机来源都可以在设置里单独关掉）。
- 查额度时会读取这些工具保存在本机的登录信息，只用它向工具自己的官方地址（chatgpt.com、api.anthropic.com）查询，不会发给任何其他地方，也不会替你登录或刷新登录。

**安静、不打扰**
- 一个快捷键 `⌃⌥Space` 打开或收起；点别处就自动缩回刘海。
- 所有选项都在设置窗口里：开机自启、剪贴板保留条数、排除的应用、自动清理、正在播放、AI 用量、更新。
- 新版本从本仓库的 GitHub Releases 检查；有更新时会提示你，你点了才会安装。

<a id="安装"></a>

## 安装

需要 Apple Silicon 芯片（M1 或更新）的 Mac，系统 macOS 13 或以上。不支持 Intel 芯片的 Mac。

1. 下载 **[Shelf.dmg](https://github.com/henryyu333/shelf/releases/latest/download/Shelf.dmg)**。
2. 打开下载好的文件，把 **Shelf** 拖到 **Applications（应用程序）** 文件夹上。
3. 从“应用程序”里打开 Shelf。第一次打开会被系统拦下，按下面的[第一次打开](#第一次打开)操作即可，只需要做一次。
4. Shelf 在屏幕顶部的菜单栏里，不在 Dock 上。把鼠标移到刘海上，或按 `⌃⌥Space`。

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

- **开机自动启动**：右键菜单栏图标 → 设置… → 通用 →「开机时自动启动」。
- **刘海拖放保护**：设置… → 通用 →「刘海拖放保护」旁的「开启…」，再到 系统设置 → 隐私与安全性 → 辅助功能 里允许 Shelf。开启后，拖着东西靠近刘海时，指针会停在托盘的接收区里，不会顶到屏幕最上沿。这一项可选，日常使用不需要任何授权。每次更新后需要重新开启一次。
- **卸载**：右键菜单栏图标 → 退出 Shelf，再把“应用程序”里的 Shelf 移到废纸篓。如果连保存的内容也要删除，删掉 `~/Library/Application Support/Shelf/` 文件夹。

## 日常使用

| 想做什么 | 怎么做 |
| --- | --- |
| 打开或收起托盘 | 按 `⌃⌥Space`、单击菜单栏图标，或把鼠标移到刘海上再点一下 |
| 放东西进来 | 拖着它靠近刘海松开；或拖拽时左右晃动指针，放进暂存篮 |
| 拿东西出去 | 从托盘拖到任意应用；按住 `⌘` 或 `⇧` 可以一次拖多个 |
| 把文字复制回来 | 单击文字项，再在任意地方 `⌘V` 粘贴 |
| 复制图片数据 | 选中一张图片后按 `⌘C`，或右键图片 →「复制图片」，再在目标应用 `⌘V` 粘贴（复制的是图片，不是路径） |
| 搜索复制过的内容 | 按 `⌃⌥V`，输入关键词，`↑`/`↓` 选择，回车 |
| 切换列表 | 点刘海左侧的托盘、剪贴板图标——开了 AI 用量时还有第三个 |
| 控制音乐 | 放音乐时把鼠标移到刘海上，或用打开后上方的播放条 |
| 置顶、移除、快速查看、隔空投送 | 右键项目（快速查看也可以直接按空格） |
| 转换图片格式、转 PDF 或 Word | 右键图片 →「转换为」；多张图片右键 →「合并为一个 PDF」；右键 PDF →「转换为」Word 或每页一张图片 |
| 缩小图片、压缩成 zip | 右键图片 →「缩小图片」选大小；右键文件或文件夹 →「压缩为 zip」 |
| 清空当前列表 | 点列表标题右边的垃圾桶图标——只清这一页，置顶的保留，原文件不会被删除 |
| 修改设置 | 右键菜单栏图标 → 设置… |
| 检查更新 | 右键菜单栏图标 →「检查更新…」 |

完整说明见 [使用手册](docs/MANUAL.zh-CN.md)。

## 隐私

一切都留在你的 Mac 上：不用注册账号，没有遥测，没有云同步。联网请求只有两类：检查更新（启动后不久一次、之后每天一次，向本仓库 GitHub 上的最新 Release 询问有没有新版本，请求里只带一个标准的 User-Agent 头）；以及可选的 AI 用量页的额度查询——只有打开它才会发生，查询用本机已保存的登录信息，只发给工具自己的官方地址，不会发给任何其他地方，也不会刷新登录。「自动检查更新」可以在 设置… → 通用 里关闭；关闭后仍可在菜单里手动检查。

剪贴板文字以明文保存在 `~/Library/Application Support/Shelf/`。要复制不希望被保存的内容时，可以先在菜单栏图标 →「剪贴板记录」里暂停记录，或者把来源应用加入排除列表。

## 从源码构建

需要 Go 1.27 或以上，以及 Xcode Command Line Tools：

```sh
go run github.com/egoist/mygo/cmd/mygo build
```

打包好的 `Shelf.app` 和 `.dmg` 在 `mygo.json` 配置的输出目录下的 `darwin-arm64` 子文件夹中。Shelf 用 Go 和 [MyGo](https://github.com/egoist/mygo) 编写。

## 许可

MIT — 见 [LICENSE](LICENSE)。
