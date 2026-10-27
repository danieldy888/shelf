<p align="center">
  <img src="https://github.com/henryyu333/shelfu/releases/download/v1.10.0/ShelfU-icon.png" width="128" alt="ShelfU icon">
</p>

<h1 align="center">ShelfU</h1>

<p align="center">
  The MacBook notch, turned into a shelf. Drop files, text and screenshots into it, drag them out wherever you need them, and keep your clipboard and music one hover away.<br>
  把 MacBook 的刘海变成一个随手放东西的托盘：文件、文字、截图往里一放，要用时再拖出来；剪贴板和正在播放的音乐，鼠标移过去就在。
</p>

<p align="center">
  <a href="https://github.com/henryyu333/shelfu/releases/latest/download/ShelfU.dmg"><img src="https://img.shields.io/badge/Download_for_macOS-ShelfU.dmg-007AFF?style=for-the-badge&logo=apple&logoColor=white" alt="Download ShelfU.dmg for macOS"></a>
</p>

<p align="center">
  Apple Silicon · macOS 13+ · Free and open source · <a href="#install">Install guide</a> · <a href="https://github.com/henryyu333/shelfu/releases/latest">All downloads</a><br>
  Apple Silicon 芯片 · macOS 13 或以上 · 免费开源 · <a href="#安装">安装说明</a> · <a href="https://github.com/henryyu333/shelfu/releases/latest">全部下载</a>
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

## What is ShelfU

When you move files between apps, you usually need somewhere to put them for a moment. ShelfU puts that place in the notch. It stays out of sight until you need it: hover the notch or click it, and a small black island grows out — the track that is playing on the left, one icon per page on the right. Drag something onto the notch instead and the tray opens on its own, already waiting for the drop. Click an icon and that page opens from the island, 420 × 250 whatever the page holds, so it fits under your menu bar instead of covering it. Only the black shape on screen takes clicks: the menu bar beside it works as usual. Put things in, take them out, and it goes back into the notch.

It lives in the menu bar, has no Dock icon, keeps everything on your Mac, and never copies your files — it only remembers where they are.

## Highlights

**A shelf in the notch**
- Drag files, folders, images or text toward the notch — it shows "release to drop" (松开放入) and the 托盘 (Tray) opens while you are still dragging. The page becomes a drop zone: drop on 放入托盘 (Add to Tray), or on an action tile that fits the files — compress pictures to 1 MB, turn them into a PDF or JPEG, convert PDFs to Word or images, extract archives, zip anything, or AirDrop it. Results land on the shelf; originals stay where they are.
- Files show as thumbnails. Drag one or several (⌘/⇧ to select) straight into any app.
- Press Space for Quick Look; right-click to open, reveal in Finder, AirDrop, copy, pin or remove — pictures get an extra "复制图片" (Copy Image) entry that copies the image itself.
- Convert in place: right-click a picture → 转换为 (Convert To) JPEG, HEIC, PNG or PDF, or recognize its text into a Word document; select several to merge them into one PDF. Right-click a PDF → 转换为 (Convert To) Word (its text) or one image per page. 缩小图片 (Shrink) resizes pictures to 2560, 1600 or 800 px on the long edge. Everything runs on this Mac, the original stays untouched, and results land at the front of the shelf.
- Tidy files where they sit: right-click a picture → 压缩到 (Compress To) to fit it under 500 KB, 1 MB, 2 MB or 5 MB; right-click a zip, tar, 7z or rar archive → 解压 (Extract); right-click files → 重新命名… (Rename…) to rename a batch the way Finder does — replace text, add text, or name plus number — with a live preview and undo.
- Other ways in: shake the pointer while dragging to summon a drop basket beside the cursor, or use Services → "添加到 ShelfU" (Add to ShelfU) in Finder and other apps.

**Clipboard history**
- Text you copy is kept automatically, along with the app it came from. Screenshots and copied images land on the shelf too.
- Press `⌃⌥V` to search: type to filter, ↑/↓ to choose, ⏎ copies it and takes you back to the app you were in. Text inside screenshots and copied pictures is recognized on this Mac and searchable too; right-click a picture → 识别文字并复制 (Copy Recognized Text) to copy what it says.
- Pin what matters. Exclude apps you never want recorded. Passwords that password managers mark as private are skipped.

**Now playing**
- Hover the notch while music plays and it grows two small wings: the cover on the left, a moving equalizer on the right. Nothing drops down over your windows.
- Click the notch and the island carries the player on its left: cover, title, artist, the equalizer and play-pause. The island keeps its size whether or not a track is playing.
- Open 主页 (Home) for the music widget. At 2 × 2 it is the full player: previous / play-pause / next, a progress bar you drag to skip, and the volume bar for your default output device; at 2 × 1 it shows the cover, title and artist with play-pause; at 1 × 1 it is cover and title, and a tap toggles playback.

**AI usage** (optional, off by default)
- Enable it in Settings → AI 用量 and the island gains one more page: the left column holds a double-ring card per tool — outer ring weekly, inner ring 5-hour — and the right column holds this Mac's token usage for omp, Claude Code and Codex, over 今天 (Today) / 7 天 (7 days) / 30 天 (30 days) / 全部 (All), as four numbers and a stacked chart with its legend. The page fits one screen and never scrolls. Hover a card for its reset times; hover the token or cost number for its parts.
- Quota checks read the login info those tools already saved on this Mac and send it only to the tools' own endpoints (chatgpt.com, api.anthropic.com) — never anywhere else, and never to sign in or refresh a login.
- Refresh always shows progress or the wait until the next quota check. Claude Code checks stay at least five minutes apart, and server retry delays still apply.

**Stay awake**
- Right-click the menu-bar icon → 保持唤醒 (Stay Awake) and your Mac won't fall asleep for 30 minutes, 1, 2 or 4 hours, or until you turn it off. Tick 屏幕也保持亮着 (Keep Screen On) to keep the display awake as well.
- While it is on, every open page shows a moon in its top-left row: hover it for the time left, click it to end the hold.

**Home page**
- 主页 (Home) is a canvas: four columns and two rows, and every widget sits in a cell of 1 × 1, 2 × 1 or 2 × 2 — whichever of those it offers. Cells you leave empty stay empty.
- The music widget is the full player at its default 2 × 2: drag the progress bar to skip, and drag the volume bar to set the volume of your output device. At 2 × 1 it shows the cover, title and artist with play-pause; at 1 × 1 the cover and title, and a tap toggles playback.
- 保持唤醒 (Stay Awake), AI 用量 (AI Usage), 最近文件 (Recent Files) and 剪贴板最新 (Latest Clipboard) live here as widgets. The AI usage widget shows the tool with the least left.
- Long-press a widget (or click the pencil) to edit: drag it to a free cell, ⊖ to remove it, the corner handle to change its size, and the dashed + cell to add a widget back from the widget catalogue (a full canvas puts + beside 完成 (Done) instead). 完成 (Done) or Esc to finish. Drag a widget out of the panel to keep it on the desktop.

**Tools page**
- 工具 (Tools) starts with eight action cards, exactly two rows: 框选文字 (Screen Text), 压缩到 1 MB (Compress to 1 MB), 转成 PDF (To PDF), 打成 zip (Zip), 转 JPEG (To JPEG), 转 Word (To Word), 解压 (Extract) and 缩小图片 (Shrink). More cards — including 隔空投送 (AirDrop) — are in the + catalogue and scroll inside the page once they push it past two rows.
- Tap an action card to run it on the pictures or files selected on the shelf, or drop files straight onto it.
- Long-press a card (or click the pencil) to edit: drag to reorder, ⊖ to remove, + to add from the catalogue; 完成 (Done) or Esc to finish.

**Pin to the desktop**
- While editing, drag a card or a widget out of the shelf and it stays on the desktop as a small floating card that still takes taps and dropped files.
- Let go near the left or right screen edge and it tucks into a thin strip: hover to open it, move away and it closes after 0.6 s, or bring a file drag close and it opens to take the drop. Right-click → 始终展开 (Always Open), 放回刘海 (Back to Notch) or 从桌面移除 (Remove from Desktop); drag it up to the notch to put it back — a widget returns to its cell on 主页 (Home), an action card to 工具 (Tools).

**Copy text from the screen**
- Press `⌃⌥O`, use the menu-bar icon → 框选文字 (Screen Text), or tap its card, then drag over any part of the screen: the text in it is recognized on this Mac, copied to the clipboard and kept in the Clipboard list, marked 框选文字.
- Needs Screen Recording permission — see [Optional](#optional).

**Quiet by design**
- One shortcut, `⌃⌥Space`, opens the island or closes the shelf; clicking anywhere else tucks it away, and Esc steps back — a page closes to the island, the island folds into the notch.
- A settings window holds every option: start at login, clipboard history size, excluded apps, automatic cleanup, now playing, AI usage, updates.
- Updates are checked against this repository's GitHub releases. ShelfU tells you when one is out and installs it only when you say so.

<a id="install"></a>

## Install

Requires a Mac with Apple Silicon (M1 or later) running macOS 13 or later. Intel Macs are not supported.

1. Download **[ShelfU.dmg](https://github.com/henryyu333/shelfu/releases/latest/download/ShelfU.dmg)**.
2. Open the downloaded file and drag **ShelfU** onto the **Applications** folder.
3. Open ShelfU from Applications. The first time, macOS will stop it — follow [First launch](#first-launch) below. You only do this once.
4. ShelfU lives in the menu bar, not the Dock. Hover the notch, or press `⌃⌥Space`.

Prefer a zip? Every release on the [Releases page](https://github.com/henryyu333/shelfu/releases/latest) also has a `.zip` of the same version.

<a id="first-launch"></a>

### First launch: "ShelfU Not Opened"

ShelfU is free and not notarized by Apple, so macOS asks you to confirm it once. Only do this for a copy you downloaded from this repository.

**macOS 15 or later**

1. Double-click ShelfU in Applications. When macOS says ShelfU was not opened, click **Done** (not Move to Trash).
2. Open **System Settings → Privacy & Security** and scroll down to **Security**.
3. Next to "ShelfU was blocked to protect your Mac", click **Open Anyway**.
4. Enter your password or use Touch ID, then click **Open Anyway** once more.

**macOS 13 or 14**

Control-click (or right-click) ShelfU in Applications, choose **Open**, then click **Open** in the dialog.

From then on ShelfU opens normally. If macOS ever says the app **is damaged**, don't bypass it — delete it and download it again from the Releases page. Apple's guide: [Safely open apps on your Mac](https://support.apple.com/102445).

### Optional

- **Start at login**: right-click the menu-bar icon → Settings → General → Open at login.
- **System permissions**: right-click the menu-bar icon → 开启系统权限 (Turn On System Permissions), or Settings → 系统权限 (System Permissions) → 全部开启 (Turn On All). ShelfU opens each System Settings page in turn: Accessibility for notch drop protection, then Screen Recording for 框选文字 (Screen Text). Turn ShelfU on in each; when Screen Recording is on, click 重新打开 ShelfU (Reopen ShelfU). Both are optional; everyday use needs no permissions. Moving from Shelf to ShelfU asks for them once more (see below). Official ShelfU releases keep the grants across later updates; a build you compile yourself is signed afresh each time and may ask again.
- **Notch drop protection** (Accessibility): while you drag toward the notch, the pointer stays in the drop area instead of hitting the very top edge of the screen.
- **Screen text** (Screen Recording): only the area you select is captured, text is recognized on this Mac, and the shot is not kept.
- **Uninstall**: right-click the menu-bar icon → Quit, then move ShelfU from Applications to the Trash. To also delete your saved items, remove `~/Library/Application Support/ShelfU/`.

### Coming from Shelf

ShelfU is the new name of Shelf. The first time ShelfU opens, it copies your saved items from `~/Library/Application Support/Shelf/` to `~/Library/Application Support/ShelfU/`; the old folder stays as it was, and you can delete it once everything is there. macOS treats ShelfU as a new app, so turn its system permissions and Open at login on again.

- **Updating inside Shelf**: after the update is installed, open Shelf from Applications. It renames itself to `ShelfU.app` and opens as ShelfU.
- **Installing from ShelfU.dmg**: quit Shelf and move `Shelf.app` to the Trash first, so that only ShelfU keeps using your items.

## Everyday use

| To | Do this |
| --- | --- |
| Open or close the shelf | Press `⌃⌥Space`, click the menu-bar icon, or hover the notch and click it — the island opens |
| Open a page | Click a page icon in the island: 主页 (Home), 托盘 (Tray), 剪贴板 (Clipboard), 工具 (Tools), or AI 用量 (AI Usage) when enabled |
| Go back | Press Esc — the page closes to the island, and Esc again folds it into the notch; clicking outside the panel (not on the menu bar), 收起 (Collapse) or the shortcut closes the whole shelf |
| Put something in | Drag it toward the notch: the 托盘 (Tray) opens as the drag arrives, and you let go to add it; or shake the pointer while dragging and drop on the basket |
| Take something out | Drag it from the shelf into any app; ⌘/⇧-click to take several at once |
| Copy text back | Click a text item, then paste with `⌘V` |
| Copy image data | Select one picture and press `⌘C`, or right-click it → 复制图片 (Copy Image) — the clipboard gets PNG data, not the path |
| Search what you copied | Press `⌃⌥V` — the 剪贴板 (Clipboard) page opens with the search field ready; type, pick with ↑/↓, press ⏎ |
| Switch pages | Click a page icon in the page's own top row — the current page is there too, with its capsule sliding to the new one; the panel keeps its size |
| Control music | Use the player on the island's left, or the music widget on 主页 (Home) to skip and set the volume |
| Pin, remove, Quick Look, AirDrop | Right-click an item (Space also opens Quick Look) |
| Copy text from the screen | Press `⌃⌥O` and drag over the text |
| Convert a picture or a PDF | Right-click a picture → 转换为 (Convert To); right-click several pictures → 合并为一个 PDF (Merge into One PDF); right-click a PDF → 转换为 (Convert To) Word or one image per page |
| Shrink a picture, zip files | Right-click a picture → 缩小图片 (Shrink) and pick 2560 / 1600 / 800 px; right-click files or folders → 压缩为 zip (Compress to zip) |
| Run a tool card | Open 工具 (Tools), then tap a card for the selected shelf items or drop files on it |
| Arrange widgets or cards | Long-press a widget on 主页 (Home) or a card on 工具 (Tools) to edit; drag it out of the shelf to pin it to the desktop |
| Shrink a picture to a file size | Right-click it → 压缩到 (Compress To) → 500 KB / 1 MB / 2 MB / 5 MB |
| Unpack an archive | Right-click a zip, tar, 7z or rar file → 解压 (Extract); the folder lands on the shelf |
| Rename files | Right-click one or more files → 重新命名… (Rename…), check the preview, then rename; undo from the notice that follows |
| Keep the Mac awake | Right-click the menu-bar icon → 保持唤醒 (Stay Awake) and pick how long |
| Clear the shelf | Click the trash icon in the page's top row — only that page is cleared, pinned items stay, original files are never deleted |
| Change settings | Right-click the menu-bar icon → Settings |
| Check for updates | Right-click the menu-bar icon → 检查更新… (Check for Updates) |

The full manual is in [docs/MANUAL.zh-CN.md](docs/MANUAL.zh-CN.md) (Chinese).

## Privacy

Everything stays on your Mac: no account, no telemetry, no cloud sync. The only network requests are the update check — shortly after launch and then once a day, ShelfU asks this repository's latest GitHub release whether a newer version exists — and, only if you enable the optional AI usage page, its quota lookups to the tools' own endpoints (nothing else is sent, and logins are never refreshed). The update request sends nothing but the app version in the standard User-Agent header. Turn it off in Settings → General → 自动检查更新 (Check for updates automatically); checking by hand from the menu still works.

Clipboard text is stored in plain text in `~/Library/Application Support/ShelfU/`. Recording is always on. To keep an app's copies out, right-click one of its clipboard entries and exclude the app; to copy something you don't want kept, quit ShelfU first.

## Build from source

Requires Go 1.27 or later and Xcode Command Line Tools:

```sh
go run github.com/egoist/mygo/cmd/mygo build
```

The packaged `ShelfU.app` and a `.dmg` land in a `darwin-arm64` subfolder under the output directory configured in `mygo.json`. ShelfU is written in Go with [MyGo](https://github.com/egoist/mygo). The app icon and the menu-bar icon are not in this repository: a build from source uses the default icon and shows the name in the menu bar.

## License

The code is MIT — see [LICENSE](LICENSE). The ShelfU name, app icon and menu-bar icon are not covered by the MIT License.

---

<a id="简体中文"></a>

## ShelfU 是什么

在应用之间搬东西时，总需要一个临时放一下的地方。ShelfU 把这个地方放进了刘海里。平时它不露面；鼠标移到刘海上点一下，一个小黑岛就从刘海里长出来——左边是正在播放的歌，右边每个页面一个图标；如果是拖着东西过来，打开的是「托盘」页，直接等着接收。点图标，那一页就从岛上展开：任何一页都是 420 × 250，所以它停在你的菜单栏下面，不会盖住菜单栏和窗口。屏幕上只有画出来的黑块接鼠标，旁边的菜单栏照常可点。放进去、拿出来，用完它就缩回刘海。

它只在菜单栏里，不占 Dock；所有内容都留在你的 Mac 上；放进来的文件不会被复制，ShelfU 只记住它们在哪儿。

## 亮点

**刘海里的托盘**
- 把文件、文件夹、图片或文字往刘海拖，刘海会提示「松开放入」，还在拖着的时候「托盘」页就已经打开。整页变成投放区：松在「放入托盘」就收进托盘；松在下方的动作格上，就按文件类型直接压缩到 1 MB、转成 PDF 或 JPEG、PDF 转 Word 或图片、解压、打成 zip，或隔空投送。结果放进托盘，原文件不动。
- 文件以缩略图显示（iPhone 拍的 HEIC 照片也有预览），可以一次拖出一个或多个（按住 ⌘ 或 ⇧ 多选），直接拖进任何应用；也可以在设置里让拖出去的东西自动离开托盘。
- 按空格快速查看；右键可以打开、在 Finder 中显示、隔空投送、复制（图片多一项「复制图片」，复制图片数据本身）、置顶或移除。
- 图片和 PDF 可以直接转换：右键图片 →「转换为」JPEG、HEIC、PNG、PDF，或识别图里的文字生成 Word，多张还能合并成一个 PDF；大图可以一键缩小；右键 PDF 可以提取文字生成 Word，或每页存成一张图片。文件和文件夹可以压缩成 zip。全部在本机完成，原文件不动，做好的文件放在托盘最前面。
- 文件整理也能就地完成：右键图片 →「压缩到」500 KB / 1 MB / 2 MB / 5 MB 以内；右键 zip、tar、7z、rar 压缩包 →「解压」；右键托盘里的文件 →「重新命名…」，像 Finder 一样替换文字、添加文字或按序号命名，改之前先预览，改完可以撤销。
- 还有别的放法：拖着东西时左右晃动指针，光标旁会出现一个暂存篮；也可以在 Finder 或其他应用里右键 →「服务」→「添加到 ShelfU」。

**剪贴板记录**
- 复制过的文字会自动收好，并记住来自哪个应用；截图和复制的图片也会进托盘。
- 按 `⌃⌥V` 搜索：输入即筛选，`↑`/`↓` 选择，回车复制并回到你原来的应用。截图和复制的图片里的文字会在本机识别，也能搜到；右键图片 →「识别文字并复制」可以直接复制图中文字。
- 重要的内容可以置顶；不想被记录的应用可以排除；密码管理器标记为隐私的密码不会被记录。

**正在播放**
- 放音乐时把鼠标移到刘海上，刘海只向左右各长出一小截：左边是封面，右边是跳动的音频条，不会向下盖住窗口。
- 点一下刘海，小岛的左边就是播放器：封面、歌名、歌手、音频条和播放暂停。有歌没歌，小岛都是同样大小。
- 打开「主页」有音乐小组件：2×2 时是完整播放器（上一首 / 播放暂停 / 下一首、可拖动跳转的进度条、系统默认输出设备的音量），2×1 显示封面、歌名、歌手和播放暂停，1×1 只有封面和歌名、点一下切换播放。

**AI 用量**（可选，默认关闭）
- 在设置 → AI 用量里打开「在刘海里显示 AI 用量」，岛上会多出一个页面图标：左栏是 Codex、Claude Code 的套餐额度卡（每张卡内外双圈：外圈每周、内圈 5 小时），右栏是这台电脑上 omp、Claude Code、Codex 的 token 用量——今天 / 7 天 / 30 天 / 全部、四个数字、按来源叠加的柱图和图例。整页一屏看全，不用滚动。悬停额度卡看重置时间；悬停 Token 或费用数字看它的组成部分。
- 查额度时会读取这些工具保存在本机的登录信息，只用它向工具自己的官方地址（chatgpt.com、api.anthropic.com）查询，不会发给任何其他地方，也不会替你登录或刷新登录。
- 点刷新会显示进度或下次可查的等待时间；Claude Code 查询至少间隔 5 分钟，也会遵守服务端的重试等待时间。

**保持唤醒**
- 右键菜单栏图标 →「保持唤醒」，选 30 分钟、1 小时、2 小时、4 小时或「一直（直到关闭）」，这段时间里 Mac 不会自动睡眠；勾选「屏幕也保持亮着」，屏幕也不会熄灭。
- 开启后，打开任何一页都能在左上角那一行看到一个月亮图标：鼠标停在上面看剩余时间，点一下结束。

**主页**
- 「主页」是一块画布：四列两行，小组件各占 1×1、2×1 或 2×2 格（每种小组件支持其中几档）；没放东西的格子就空着。
- 音乐小组件默认 2×2，是完整的播放器：进度条可以拖动跳转，音量条调的是系统默认输出设备的音量。缩小到 2×1 显示封面、歌名、歌手和播放暂停，1×1 只有封面和歌名、点一下切换播放。
- 保持唤醒、AI 用量、最近文件、剪贴板最新都以小组件的形式放在这里。AI 用量小组件显示剩余最少的那个工具。
- 长按小组件（或点铅笔图标）进入编辑：拖到空格、点 ⊖ 移除、拖右下角改尺寸、点虚线「+」格从小组件目录加回一个（格子放满时「+」改到「完成」左边）。点「完成」或按 Esc 结束。把小组件拖出面板，它就固定到桌面。

**工具页**
- 「工具」页只放动作卡，默认八张正好两行：框选文字、压缩到 1 MB、转成 PDF、打成 zip、转 JPEG、转 Word、解压、缩小图片。隔空投送等其余动作卡在「+」打开的目录里；加多了超过两行就在页内滚动。
- 点按动作卡，对托盘里选中的图片或文件执行；也可以把文件直接拖到卡片上。
- 长按卡片（或点铅笔图标）进入编辑：拖动排序、点 ⊖ 删除、点 + 从目录添加；点「完成」或按 Esc 结束。

**固定到桌面**
- 编辑时把小组件或卡片拖出面板，它会留在桌面上成为一张小浮窗，照样可以点按、接收拖入的文件。
- 松在屏幕左右边缘附近，它会收成一条窄条：鼠标移上去展开，移开 0.6 秒后收回；拖着文件靠近也会展开接收。右键可选「始终展开」「放回刘海」「从桌面移除」；把它拖回刘海也能放回——小组件回到「主页」原来的格子，动作卡回到「工具」页。

**框选屏幕文字**
- 按 `⌃⌥O`、右键菜单栏图标 →「框选文字」，或点它的卡片，然后在屏幕上框一块区域：里面的文字在本机识别后直接复制到剪贴板，并记进剪贴板页，来源显示「框选文字」。
- 需要「屏幕录制」权限，见下面的[可选](#可选)。

**安静、不打扰**
- 一个快捷键 `⌃⌥Space` 打开小岛或收起；点别处就自动缩回刘海。Esc 一级一级往回退：先关掉当前页回到小岛，再按一次把小岛收回刘海。
- 所有选项都在设置窗口里：开机自启、剪贴板保留条数、排除的应用、自动清理、正在播放、AI 用量、更新。
- 新版本从本仓库的 GitHub Releases 检查；有更新时会提示你，你点了才会安装。

<a id="安装"></a>

## 安装

需要 Apple Silicon 芯片（M1 或更新）的 Mac，系统 macOS 13 或以上。不支持 Intel 芯片的 Mac。

1. 下载 **[ShelfU.dmg](https://github.com/henryyu333/shelfu/releases/latest/download/ShelfU.dmg)**。
2. 打开下载好的文件，把 **ShelfU** 拖到 **Applications（应用程序）** 文件夹上。
3. 从“应用程序”里打开 ShelfU。第一次打开会被系统拦下，按下面的[第一次打开](#第一次打开)操作即可，只需要做一次。
4. ShelfU 在屏幕顶部的菜单栏里，不在 Dock 上。把鼠标移到刘海上，或按 `⌃⌥Space`。

想要 zip 压缩包？[Releases 页面](https://github.com/henryyu333/shelfu/releases/latest)的每个版本里也有同一版本的 `.zip`。

<a id="第一次打开"></a>

### 第一次打开：提示“未打开 ShelfU”

ShelfU 是免费软件，没有经过 Apple 公证，所以 macOS 会让你确认一次。请只对从本仓库下载的版本这样做。

**macOS 15 或更新**

1. 在“应用程序”里双击 ShelfU。系统提示未打开时，点 **完成**（不要点“移到废纸篓”）。
2. 打开 **系统设置 → 隐私与安全性**，往下滚动到 **安全性**。
3. 在“已阻止 ShelfU 以保护 Mac”旁边，点 **仍要打开**。
4. 输入开机密码或用触控 ID 确认，再点一次 **仍要打开**。

**macOS 13 或 14**

在“应用程序”里按住 Control 点按（或右键点按）ShelfU，选 **打开**，再在弹窗里点 **打开**。

之后就能正常打开了。如果系统提示 App **已损坏**，不要绕过，删掉后从 Releases 页面重新下载。Apple 官方说明：[在 Mac 上安全地打开 App](https://support.apple.com/zh-cn/102445)。

### 可选

- **开机自动启动**：右键菜单栏图标 → 设置… → 通用 →「开机时自动启动」。
- **系统权限**：右键菜单栏图标 →「开启系统权限」，或 设置… → 系统权限 →「全部开启」。ShelfU 会依次打开系统设置里的两页：「辅助功能」（刘海拖放保护）和「屏幕录制」（框选文字）；在每页打开 ShelfU 的开关，屏幕录制开好后点「重新打开 ShelfU」。两项都可选，日常使用不需要任何授权。从 Shelf 换到 ShelfU 时要再开一次（见下）；ShelfU 的正式发布版在之后更新时保留授权，自己编译的构建每次签名都会变，可能要重新开启。
- **刘海拖放保护**（辅助功能）：拖着东西靠近刘海时，指针会停在托盘的接收区里，不会顶到屏幕最上沿。
- **框选文字**（屏幕录制）：只截取你框选的那一块，文字在本机识别，不保存截图。
- **卸载**：右键菜单栏图标 → 退出 ShelfU，再把“应用程序”里的 ShelfU 移到废纸篓。如果连保存的内容也要删除，删掉 `~/Library/Application Support/ShelfU/` 文件夹。

### 从 Shelf 升级

ShelfU 是 Shelf 的新名字。ShelfU 第一次打开时，会把 `~/Library/Application Support/Shelf/` 里保存的内容复制到 `~/Library/Application Support/ShelfU/`；旧文件夹保持原样，确认内容都在以后可以删掉。macOS 会把 ShelfU 当成一个新应用，所以系统权限和「开机时自动启动」要重新打开一次。

- **在 Shelf 里应用内更新**：更新装好后，在“应用程序”里打开 Shelf。它会自己改名为 `ShelfU.app` 并以 ShelfU 打开。
- **用 ShelfU.dmg 安装**：先退出 Shelf，把 `Shelf.app` 移到废纸篓，只让 ShelfU 使用你的内容。

## 日常使用

| 想做什么 | 怎么做 |
| --- | --- |
| 打开或收起 | 按 `⌃⌥Space`、单击菜单栏图标，或把鼠标移到刘海上再点一下——打开的是小岛 |
| 打开某一页 | 点小岛上的页面图标：主页、托盘、剪贴板、工具，开了 AI 用量时还有 AI 用量 |
| 退回上一层 | 按 Esc：先关掉当前页回到小岛，再按一次把小岛收回刘海。点面板外（菜单栏高度内不算）、点「收起」或再按快捷键都直接收起 |
| 放东西进来 | 拖着它靠近刘海，还没松手「托盘」页就打开了，松手放进去；或拖拽时左右晃动指针，放进暂存篮 |
| 拿东西出去 | 从托盘拖到任意应用；按住 `⌘` 或 `⇧` 可以一次拖多个 |
| 把文字复制回来 | 单击文字项，再在任意地方 `⌘V` 粘贴 |
| 复制图片数据 | 选中一张图片后按 `⌘C`，或右键图片 →「复制图片」，再在目标应用 `⌘V` 粘贴（复制的是图片，不是路径） |
| 搜索复制过的内容 | 按 `⌃⌥V`，「剪贴板」页直接打开、搜索框已就位；输入关键词，`↑`/`↓` 选择，回车 |
| 切换页面 | 点返回行里的页面图标（当前页的图标也在，底下的胶囊会滑过去）；面板尺寸不变 |
| 控制音乐 | 用小岛左边的播放器；打开「主页」的音乐小组件可以拖动进度和调整音量 |
| 置顶、移除、快速查看、隔空投送 | 右键项目（快速查看也可以直接按空格） |
| 框选屏幕文字 | 按 `⌃⌥O`，框住要复制的文字 |
| 用工具卡 | 打开「工具」页，点按卡片处理托盘里选中的项目，或把文件拖到卡片上 |
| 排列小组件或卡片 | 长按「主页」的小组件或「工具」页的卡片进入编辑；把它们拖出面板就固定到桌面 |
| 转换图片格式、转 PDF 或 Word | 右键图片 →「转换为」；多张图片右键 →「合并为一个 PDF」；右键 PDF →「转换为」Word 或每页一张图片 |
| 缩小图片、压缩成 zip | 右键图片 →「缩小图片」选大小；右键文件或文件夹 →「压缩为 zip」 |
| 压缩图片到指定大小 | 右键图片 →「压缩到」→ 500 KB / 1 MB / 2 MB / 5 MB 以内 |
| 解压压缩包 | 右键 zip、tar、7z、rar 文件 →「解压」，解出的文件夹放进托盘 |
| 批量重新命名 | 右键一个或多个文件 →「重新命名…」，看过预览再点「改名」；改完可以在提示上撤销 |
| 不让 Mac 睡眠 | 右键菜单栏图标 →「保持唤醒」，选时长 |
| 清空当前列表 | 点页面顶部那一行右边的垃圾桶图标——只清这一页，置顶的保留，原文件不会被删除 |
| 修改设置 | 右键菜单栏图标 → 设置… |
| 检查更新 | 右键菜单栏图标 →「检查更新…」 |

完整说明见 [使用手册](docs/MANUAL.zh-CN.md)。

## 隐私

一切都留在你的 Mac 上：不用注册账号，没有遥测，没有云同步。联网请求只有两类：检查更新（启动后不久一次、之后每天一次，向本仓库 GitHub 上的最新 Release 询问有没有新版本，请求里只带一个标准的 User-Agent 头）；以及可选的 AI 用量页的额度查询——只有打开它才会发生，查询用本机已保存的登录信息，只发给工具自己的官方地址，不会发给任何其他地方，也不会刷新登录。「自动检查更新」可以在 设置… → 通用 里关闭；关闭后仍可在菜单里手动检查。

剪贴板文字以明文保存在 `~/Library/Application Support/ShelfU/`。剪贴板记录始终开启。不想记录某个应用复制的内容，右键它的剪贴板条目把应用排除；要复制不希望被保存的内容时，先退出 ShelfU 再复制。

## 从源码构建

需要 Go 1.27 或以上，以及 Xcode Command Line Tools：

```sh
go run github.com/egoist/mygo/cmd/mygo build
```

打包好的 `ShelfU.app` 和 `.dmg` 在 `mygo.json` 配置的输出目录下的 `darwin-arm64` 子文件夹中。ShelfU 用 Go 和 [MyGo](https://github.com/egoist/mygo) 编写。应用图标和菜单栏图标不在本仓库里：自己从源码构建时用的是默认图标，菜单栏里显示文字名称。

## 许可

代码采用 MIT 许可，见 [LICENSE](LICENSE)。ShelfU 的名称、应用图标和菜单栏图标不在 MIT 许可范围内。
