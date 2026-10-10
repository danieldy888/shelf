<p align="center">
  <img src="resources/icon.png" width="128" alt="Shelf icon">
</p>

<h1 align="center">Shelf</h1>

<p align="center">
  A native macOS shelf that slides out from the left edge of your screen — stash files, text and screenshots, then drag them anywhere.<br>
  从屏幕左边缘滑出的 macOS 临时托盘——暂存文件、文字和截图，随用随拖走。
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

## Features

- Slides out from the left screen edge while you're dragging anything
- Global hotkey ⌃⌥Space to show or hide the shelf
- Drag files, folders, text and images in — and drag them out to any app
- Clipboard text history: keeps the last 30 items, duplicates move to the top instead of creating copies
- Screenshots and clipboard images land in the shelf automatically
- Contents persist across restarts
- Stores file references only — your files are never copied
- Fully local: zero network requests, no telemetry

## Install

### Download the prebuilt app

Grab the latest `Shelf.app` from the [Releases page](https://github.com/henryyu333/shelf/releases).

The app is ad-hoc signed and not notarized. On first open, macOS may warn that the developer can't be verified: try to open it once, then go to **System Settings → Privacy & Security → Open Anyway**. Only do this for a build you trust.

### Build from source

Requires Go 1.27 or later and Xcode Command Line Tools:

```sh
go run github.com/egoist/mygo/cmd/mygo build -skip-dmg
```

The packaged `.app` lands in a `darwin-arm64` subfolder under the output directory configured in `mygo.json`. Requires macOS 13+ on Apple Silicon.

## Usage

| Want to | Do this |
| --- | --- |
| Show or hide the shelf | Press `⌃⌥Space`, or click the menu-bar icon |
| Stash what you're dragging | While dragging anything, touch the left screen edge and pause — the shelf slides out |
| Drag an item out | Drag it from the shelf into the target app |
| Copy a text item back | Click it once, then paste (`⌘V`) anywhere |
| Remove an item | Click the × on its right; file references never delete the original file |
| Quit Shelf | Right-click the menu-bar icon → Quit |

## Privacy

Everything stays on your Mac: no network requests, no telemetry, no cloud sync. Text history is stored in plaintext at `~/Library/Application Support/Shelf/`, so avoid copying secrets you don't want recorded there.

## License

MIT — see [LICENSE](LICENSE).

---

<a id="简体中文"></a>

## 功能

- 拖拽任何东西时碰到屏幕左边缘，托盘自动滑出
- 全局快捷键 ⌃⌥Space 随时显示或收起
- 文件、文件夹、文字、图片都可以拖进来，再拖到任意应用
- 剪贴板文字历史：最多保留 30 条，重复内容移到顶部而不新增条目
- 截图和剪贴板图片自动进入托盘
- 重启后内容保留
- 只记录文件引用，绝不复制你的文件
- 完全本地运行：零网络请求，无遥测

## 安装

### 下载成品

从 [Releases 页面](https://github.com/henryyu333/shelf/releases) 下载最新的 `Shelf.app`。

应用为 ad-hoc 签名，未经过 Apple 公证。首次打开时系统可能提示无法验证开发者：先尝试打开一次，再到 **系统设置 → 隐私与安全性 → 仍要打开**。请只对你确认来源可靠的构建执行此操作。

### 从源码构建

需要 Go 1.27 或以上，以及 Xcode Command Line Tools：

```sh
go run github.com/egoist/mygo/cmd/mygo build -skip-dmg
```

打包好的 `.app` 在 `mygo.json` 配置的输出目录下的 `darwin-arm64` 子文件夹中。运行要求 macOS 13 或以上、Apple Silicon 芯片。

## 使用

| 想做什么 | 操作 |
| --- | --- |
| 显示或收起托盘 | 按 `⌃⌥Space`，或单击菜单栏图标 |
| 暂存正在拖的内容 | 拖着任何东西碰到屏幕左边缘并停留片刻，托盘自动滑出 |
| 拖出项目 | 从托盘中拖住该项，到目标应用中松开 |
| 把文字复制回来 | 单击该文字项，然后在任意应用中粘贴（`⌘V`） |
| 移除项目 | 点击该项右侧的 ×；文件引用不会删除原文件 |
| 退出 Shelf | 右键菜单栏图标 → 退出 Shelf |

## 隐私

一切都在本机：不联网、无遥测、无云同步。文字历史以明文保存在 `~/Library/Application Support/Shelf/`，不希望被记录的密码、验证码等内容请先退出 Shelf 再复制。

## 许可

MIT — 见 [LICENSE](LICENSE)。
