# GitHub Net

[![CI](https://github.com/xiaokentrl/github-net/actions/workflows/ci.yml/badge.svg)](https://github.com/xiaokentrl/github-net/actions/workflows/ci.yml)

[中文](#中文) · [English](#english)

---

## 中文

GitHub Net（GitHub 加速助手）是一个用 Go、Wails v3、React 和 TypeScript 构建的跨平台桌面应用。它通过检测 GitHub 域名的可用 IP 并写入系统 Hosts 文件来加速访问：不改系统代理、不装虚拟网卡、不需要常驻后台流量。加速方法按独立策略扩展，后续可加装系统代理、PAC 等新方式，每种方式一个实现文件。

### 功能特性

- **一站式加速**：选服务 → 选加速方法 → 检测 → 确认应用，四步完成；GitHub 的加速方法独立保存，之后新增 Steam 等服务互不影响。
- **加速方法可扩展**：首期提供「自动查找可用 IP」（DNS 候选 + Hosts）；新增方法只需一个 Go 文件自注册，不改注册表、路由和界面。
- **真实可见的检测**：每个候选 IP 逐行显示排队中 → 检测中 → 可用/不可用/超时的完整状态、延迟和耗时；单项最多 5 秒、整个服务最多 30 秒，不伪造进度和剩余时间。
- **省流三档**：省流（每域名 2 个候选、并发 2）/ 标准（8 / 8）/ 完整检测（16 / 8）；每轮探测预算启动前可见，预算不足先确认；只做 DNS 查询和 TCP/TLS 握手，不下载网页，不自动重试。
- **任务队列**：检测、应用、还原统一进队列，支持进度、取消、重试和一键清空；切页面不丢进度。
- **Hosts 安全**：写入前备份，只修改带本工具标记的区段，原子替换并验证，失败自动回滚；一键还原到应用前状态，也可直接打开 Hosts 查看。
- **权限最小化**：主界面始终以普通用户权限运行；首次安装时一次性启用受限特权辅助服务，之后读写 Hosts 不再反复要管理员/root 权限。
- **自动更新**：检查 GitHub Releases 最新稳定版，下载前显示版本、来源和大小，签名与摘要校验通过才安装。
- **设置中心**：网络与流量、外观、语言、布局、启动（托盘、开机自启动）、检测与应用分页管理；设置经后端统一保存，跨重启生效。
- **六套主题**：晨光白、樱花粉、森林绿、深海蓝、午夜蓝、极夜黑（纯黑 OLED 省电）；另有界面缩放。
- **双语界面**：简体中文默认，繁体中文可切换；日期、数字按语言本地化。
- **无障碍**：键盘可完成核心操作，状态变化对读屏器可见，不只用颜色传达。

### 工作原理

1. 检测：向可信 DNS 解析器查询目标域名的候选 IP，对每个候选做 TCP/TLS 连通性验证（保留主机名和证书校验，不下载网页内容）。
2. 选择：界面逐行展示每个候选的状态、延迟和验证结果，由用户决定用哪个。
3. 应用：把选中的 IP 写入系统 Hosts，只修改带本工具标记的区段，不碰用户和其他程序的内容；写入前备份，可随时还原。
4. 扩展：加速方法采用单文件自注册设计——新增方式各加一个 Go 文件即可，不改中心注册表和界面。

### 使用流程

1. 左侧分类树选服务或单个项目。
2. 在详情顶部的「GitHub 全部加速方法」里选一种方法（首次使用默认「自动查找可用 IP」）。
3. 点「检测」，逐个候选确认可用性；可在检测按钮旁的档位标签里快速切省流/标准/完整检测。
4. 点「应用」写入 Hosts；需要撤销时「还原」回应用前的状态。

### 下载与安装

到 [Releases](https://github.com/xiaokentrl/github-net/releases) 下载对应平台的安装包：

| 平台 | 安装包 | 系统要求 |
|---|---|---|
| Windows | `.exe`（NSIS 安装器） | Windows 10 及以上，WebView2（安装器自动补齐） |
| macOS | `.dmg` | 内置 WKWebView 的受支持版本 |
| Linux | `.deb` / `.rpm` / `.AppImage` | Debian 13、Ubuntu 26.04 及兼容发行版（GTK4 / WebKitGTK 6.0） |

每个版本都附 `checksums.txt` 和签名文件；更新由应用内自动检查，也可手动到 Releases 页下载。Linux 系统要求详见下文「开发和构建」里的发布线说明。

### 开发环境

- Go 1.27.3 或更高版本
- Wails v3.0.0
- Node.js 和 npm

### 开发和构建

```sh
wails3 dev
```

```sh
wails3 build
```

Linux 只有一条发布线：默认 GTK4/WebKitGTK 6.0，构建宿主为 Ubuntu 26.04（本机即该版本，或等效容器 `ubuntu:26.04`），也就是上面的 `wails3 build`。发布目标是 Debian 13 与 Ubuntu 26.04。Debian 12 不在目标内——它的仓库没有 `webkitgtk-6.0`，默认后端编不过；Ubuntu 24.04 也已于 2026-10-27 经裁决移出，改由 26.04 LTS 取代。`-tags gtk3` 只留作开发者本机备选，官方不为它出包、不承诺修复。

Linux 构建需要 `libgtk-4-dev libwebkitgtk-6.0-dev`。产物能不能装某台机器，看实测的两道门：用 `objdump -T` 查产物要求的最高 `GLIBC_x.y` 是否不高于 `GLIBC_2.41`（最低目标 Debian 13 的 glibc），用 `ldd` 查目标发行版是否有 `libwebkitgtk-6.0.so.4` 和 `libgtk-4.so.1`。同一份代码在 Ubuntu 26.04 编出的产物要求 `GLIBC_2.38`，放进 Debian 13 容器 `ldd -r` 无未解析符号，所以一台 26.04 宿主可以同时供 Debian 13 与 Ubuntu 26.04 两条线；Debian 13 那边只验过链接，实机运行还没验。Windows 和 macOS 构建需在各自原生 runner 上验证。

运行 Go 测试：

```sh
go test ./...
```

### 推送前自动重装

本项目的规则：每次 `git push` 前自动跑一遍「打包 → 停旧进程 → 卸载旧包并装新包 → 删配置」，让本机装的永远是这一批要推的代码。实现是 `.githooks/pre-push` 调 `scripts/pre-push-reinstall.sh`。

钩子只对设过 `git config core.hooksPath .githooks` 的机器生效；这条设置只写在 `.git/config`，不在仓库里，换机器要重设一次。

卸载和安装要用 root。仓库里有两条路：

- **root 助手（推荐，配一次以后不再问密码）**：`scripts/github-net-root-install.sh` 只干「卸载旧包 + 装新包」两件事，先一次性配置 `sudo bash scripts/setup-passwordless-reinstall.sh`（这一步要输一次密码），把它装成 root 所有的 `/usr/local/sbin/github-net-root-install` 并给它单独开免密。之后重装脚本检测到这个助手就整个交给它，`git push` 全程不打断。这个助手被免密调用，所以不要往里加别的功能——加什么能力等于给用户开什么 root 权限。撤销配置：`sudo rm /etc/sudoers.d/github-net-reinstall /usr/local/sbin/github-net-root-install`。
- **直接 sudo（没配助手时的兜底）**：脚本按「已有免密凭据 → 终端里问密码 → 图形密码框」依次试。钩子里没有终端，直接 `sudo` 会卡在「需要终端」，所以走钩子推送时会弹一个图形密码框（`scripts/gui-askpass.sh`，用 zenity）。密码只交给 sudo 那一次，不落文件、不进日志，也不给机器加免密。

授权或安装没过就中断推送。删配置排在装好新包之后：这中间没有任何一次运行会写配置，效果一样是新包从默认配置起步，但不会出现「配置已删、包没装」的半截状态。删配置每次都执行，不管之前装没装过。

只改文档想跳过一次：

```sh
GITHUB_NET_SKIP_REINSTALL=1 git push
```

### 项目结构

标了【未建】的目录还只在规划里，仓库里还没有这些文件；其余是当前的真实结构。

```text
.
├── main.go                  # Wails 应用入口：装配服务、托盘、单实例锁、生命周期
├── internal/
│   ├── app/                 # 暴露给前端的接口、DTO 和错误转换
│   ├── accelerator/         # 【未建】加速流程协调与按服务的方法路由
│   │   └── strategies/      # 【未建】加速方法（单文件自注册）
│   ├── catalog/             # 服务、项目、域名等产品数据（catalog.json）
│   ├── config/              # 用户配置：校验、revision、原子保存
│   ├── detector/            # 【未建】DNS 解析、候选探测与排序
│   ├── event/               # 给前端推事件的统一出口
│   ├── hosts/               # 【未建】Hosts 解析、备份、事务写入与还原
│   ├── platform/            # 【未建】Windows/macOS/Linux 系统差异适配
│   ├── privilege/           # 【未建】受限特权辅助服务的 IPC 客户端与协议
│   ├── task/                # 【未建】任务生命周期、取消与进度
│   └── updater/             # 【未建】稳定版检查与更新包校验
├── frontend/
│   ├── bindings/            # Wails 生成的绑定（不手改）
│   └── src/                 # React 页面、组件、i18n、样式
├── scripts/                 # 版本号、Release notes 生成、推送前自动重装等脚本
├── docs/                    # 更新日志与 Release 条目清单（docs/release-notes/）
└── .github/workflows/       # CI 与工作流
```

### CI

`.github/workflows/ci.yml` 在 push 和 PR 上跑四步：前端 `npm ci && npm run build`（含 tsc）；后端在 `ubuntu:26.04` 容器里查 `gofmt`、`go vet ./...`、`go test -count=1 ./...`；同在 Ubuntu 26.04 宿主用默认后端出 x64 产物并跑门一（≤`GLIBC_2.41`）与本发行版门二；最后把产物放进 `debian:13` 容器复核门二。当前只覆盖 Linux x64，arm64、Windows、macOS 还没有对应任务，所以本地提交前请自行跑 `go build ./... && go vet ./... && go test ./...`、`gofmt -l .`（应为空）和 `frontend` 里的 `npm run build`。

### 文档

- [技术规划](./TECHNICAL_PLAN.md) — 目标、模块职责、接口、开发阶段
- [前后端结构规划](./FRONTEND_BACKEND_STRUCTURE_PLAN.md) — 目录、数据流、扩展规则
- [公共组件规划](./PUBLIC_COMPONENTS_PLAN.md) — 前后端公共组件与方法
- [更新日志](./docs/CHANGELOG.md) — 按版本记录改动

### 许可证

[MIT](./LICENSE)

---

## English

GitHub Net is a cross-platform desktop app built with Go, Wails v3, React and TypeScript. It speeds up access to GitHub by probing available IPs for GitHub domains and writing them into the system hosts file: no system proxy changes, no virtual network adapters, no background traffic. Acceleration methods are independent strategies — a system proxy, PAC and other methods can be added later, one file each.

### Status

The project follows the five stages in [TECHNICAL_PLAN](./TECHNICAL_PLAN.md). The catalog and the settings read/write path are wired; detection tasks, the task queue and hosts writing come later.

| Feature | Status |
|---|---|
| Category tree, search, service detail | ✅ Live, reads real backend data |
| Status bar (platform, version, permission) | ✅ Live |
| Settings center: network & traffic, layout | ✅ Live through the backend config service; queue height and the other six pages (zoom, appearance, tray, startup, update check, about) are not wired yet |
| System tray | ✅ Basic tray live |
| Acceleration method selection (DNS candidates written to hosts) | 🚧 Planned |
| IP candidate probing (low / standard / full profiles) | 🚧 Planned |
| Task queue (progress, cancel, retry, clear) | 🚧 Planned |
| Hosts backup, apply and restore | 🚧 Planned |
| Auto update (GitHub Releases) | 🚧 Planned |
| UI localization (zh-CN first, zh-TW follows) | 🚧 Planned |

### Feature plan

Everything below describes the target product; check the table above for what actually runs today.

- **One-stop acceleration**: pick a service → pick a method → probe → apply, four steps. Each service keeps its own method; adding Steam later never affects GitHub.
- **Extensible methods**: the first method is "find working IPs automatically" (DNS candidates + hosts). A new method ships as one self-registering Go file — no central registry, routing or UI changes.
- **Visible probing**: every candidate IP shows its full lifecycle per row — queued → probing → reachable/unreachable/timeout — with latency and elapsed time; a single item takes at most 5 seconds, a whole service at most 30, with no fake progress bars.
- **Three data profiles**: low data (2 candidates per domain, concurrency 2) / standard (8 / 8) / full (16 / 8); the per-round probe budget is shown up front, and shortfalls are confirmed before starting. Probing sends DNS queries and TCP/TLS handshakes only — no page downloads, no automatic retries.
- **Task queue**: probing, applying and restoring all run through one queue with progress, cancel, retry and a clear-all; switching pages never loses a task.
- **Hosts safety**: backup before writing, touch only the section tagged by this tool, atomic replace with verification and automatic rollback; one-click restore to the pre-apply state, plus an "open hosts" shortcut.
- **Least privilege**: the UI always runs as a normal user; a restricted privileged helper is enabled once at install time, so hosts read/write never asks for admin/root again.
- **Auto updates**: checks the latest stable GitHub Release, shows version, source and size before downloading, and installs only after signature and digest verification.
- **Settings center**: network & traffic, appearance, language, layout and startup (tray, launch at login) as separate pages, saved through the backend and kept across restarts.
- **Six themes**: Daylight, Sakura, Forest, Deep Ocean, Midnight and OLED Black, plus UI zoom.
- **Bilingual UI**: Simplified Chinese by default with Traditional Chinese available; dates and numbers localized per language.
- **Accessibility**: core flows are keyboard operable, state changes are announced to screen readers, and color is never the only signal.

### How it works

1. Probe: query trusted DNS resolvers for candidate IPs of the target domains, then verify each candidate with a TCP/TLS handshake (hostname and certificate validation kept; no page downloads).
2. Choose: the UI lists every candidate with its state, latency and verification result; the user picks one.
3. Apply: write the chosen IP into the system hosts file, touching only the section tagged by this tool; the file is backed up first and can be restored at any time.
4. Extend: acceleration methods self-register from a single file — future methods each ship as one Go file, with no central registry or UI changes.

### Usage

The flow below is the planned interaction; probing, applying and restoring are not wired yet, so today the app only shows catalog and settings state.

1. Pick a service or a single item in the category tree.
2. Under "All acceleration methods for GitHub" in the detail header, pick a method ("find working IPs automatically" is the default).
3. Hit "Probe" and review each candidate; switch the low/standard/full profile from the compact label next to the probe button.
4. Hit "Apply" to write hosts; "Restore" returns to the pre-apply state whenever needed.

### Downloads

No stable release exists yet (the Releases page is empty and no version tag has been cut). Once the first stable build ships, installers will be published at [Releases](https://github.com/xiaokentrl/github-net/releases); the planned packages are:

| Platform | Package | Requirements |
|---|---|---|
| Windows | `.exe` (NSIS installer) | Windows 10 or later, WebView2 (installed automatically if missing) |
| macOS | `.dmg` | A supported version with WKWebView |
| Linux | `.deb` / `.rpm` / `.AppImage` | Debian 13, Ubuntu 26.04 and compatible distros (GTK4 / WebKitGTK 6.0) |

Planned releases ship `checksums.txt` and signature files, and updates are checked in-app against the latest stable GitHub Release; none of that is live until the first release exists. See the release-line notes under "Development and build" for Linux requirements.

### Development environment

- Go 1.27.3 or later
- Wails v3.0.0
- Node.js and npm

### Development and build

```sh
wails3 dev
```

```sh
wails3 build
```

Linux has a single release line: the default GTK4/WebKitGTK 6.0 backend, built on an Ubuntu 26.04 host (the native OS or an equivalent `ubuntu:26.04` container) via `wails3 build` above. Release targets are Debian 13 and Ubuntu 26.04. Debian 12 is out of scope — its repositories lack `webkitgtk-6.0`, so the default backend does not compile; Ubuntu 24.04 was dropped by decision on 2026-10-27 in favour of 26.04 LTS. `-tags gtk3` remains a developer-side fallback only; no official packages and no fix guarantees for it.

Linux builds need `libgtk-4-dev libwebkitgtk-6.0-dev`. Whether a binary can be installed on a given machine is decided by two measured gates: `objdump -T` must show no `GLIBC_x.y` above `GLIBC_2.41` (the glibc of Debian 13, the oldest target), and `ldd` must find `libwebkitgtk-6.0.so.4` and `libgtk-4.so.1` on the target distro. A binary built on Ubuntu 26.04 currently requires `GLIBC_2.38` and links cleanly inside a Debian 13 container (`ldd -r` reports no unresolved symbols), so one 26.04 host serves both lines; on the Debian 13 side only linking has been verified, not a real desktop run. Windows and macOS builds need to be validated on their native runners.

Run the Go tests:

```sh
go test ./...
```

### Reinstall before each push

Project rule: every `git push` first runs "build package → stop the old process → purge the installed package and install the new one → delete its config", so the machine always runs exactly the code being pushed. `.githooks/pre-push` calls `scripts/pre-push-reinstall.sh`.

The hook only fires on machines where `git config core.hooksPath .githooks` was set; that setting lives in `.git/config`, is not in the repository, and has to be re-set on a new machine.

Purging and installing need root. The repository offers two ways:

- **Root helper (recommended; one setup, then no more prompts).** `scripts/github-net-root-install.sh` does exactly two things — purge the old package, install the new one. Configure it once with `sudo bash scripts/setup-passwordless-reinstall.sh` (this step asks for a password): it installs the helper as `/usr/local/sbin/github-net-root-install`, owned by root, and grants passwordless sudo for that one file. After that the reinstall script hands the whole swap to the helper and `git push` never interrupts. The helper runs under passwordless sudo, so do not add other capabilities to it — anything you add is a root capability for the user. To undo: `sudo rm /etc/sudoers.d/github-net-reinstall /usr/local/sbin/github-net-root-install`.
- **Plain sudo (fallback when the helper is not installed).** The script tries, in order: cached or passwordless credentials, the terminal prompt when pushing from a terminal, then a graphical password dialog inside a hook (`scripts/gui-askpass.sh`, using zenity) — a hook has no terminal, so plain `sudo` would stop at "a terminal is required". The password is handed to sudo once only: never written to a file or a log, and the machine gets no passwordless rule.

If authorisation or installation fails the push aborts. Config deletion runs after the new package is installed: nothing launches the app in between, so the new package still starts from default settings, but a half state — config gone, package not installed — cannot happen. Config deletion runs on every push, installed before or not.

To skip once, for a documentation-only change:

```sh
GITHUB_NET_SKIP_REINSTALL=1 git push
```

### Project layout

Entries marked [not built] exist only in the plan — those directories are not in the repo yet; the rest is the current layout.

```text
.
├── main.go                  # Wails entry point: service wiring, single-instance lock, tray, lifecycle
├── internal/
│   ├── app/                 # Frontend-facing API, DTOs and error mapping
│   ├── accelerator/         # [not built] Acceleration flow and per-service method routing
│   │   └── strategies/      # [not built] Acceleration methods (single-file self-registration)
│   ├── catalog/             # Product data: services, items, domains (catalog.json)
│   ├── config/              # User config: validation, revision, atomic saves
│   ├── detector/            # [not built] DNS resolution, candidate probing and ranking
│   ├── event/               # Single outlet for events pushed to the frontend
│   ├── hosts/               # [not built] Hosts parsing, backup, transactional writes and restore
│   ├── platform/            # [not built] Windows/macOS/Linux system adapters
│   ├── privilege/           # [not built] Restricted privileged helper IPC client and protocol
│   ├── task/                # [not built] Task lifecycle, cancellation and progress
│   └── updater/             # [not built] Stable-release checks and update verification
├── frontend/
│   ├── bindings/            # Wails-generated bindings (do not edit by hand)
│   └── src/                 # React pages, components, i18n, styles
├── scripts/                 # Version, release-notes and pre-push reinstall helpers
├── docs/                    # Changelog and release note entry lists (docs/release-notes/)
└── .github/workflows/       # CI and workflows
```

### CI

`.github/workflows/ci.yml` runs four steps on push and PR: frontend `npm ci && npm run build` (including tsc); backend `gofmt`, `go vet ./...` and `go test -count=1 ./...` inside an `ubuntu:26.04` container; an x64 build on the default backend on an Ubuntu 26.04 host with gate one (≤`GLIBC_2.41`) and gate two for that distro; and a re-check of gate two inside a `debian:13` container. Only Linux x64 is covered for now — arm64, Windows and macOS have no tasks yet, so before pushing run `go build ./... && go vet ./... && go test ./...`, `gofmt -l .` (must be empty) and `npm run build` in `frontend` locally.

### Docs

- [Technical plan](./TECHNICAL_PLAN.md) — goals, module responsibilities, APIs, phases
- [Frontend/backend structure plan](./FRONTEND_BACKEND_STRUCTURE_PLAN.md) — layout, data flow, extension rules
- [Shared components plan](./PUBLIC_COMPONENTS_PLAN.md) — frontend and Go shared components
- [Changelog](./docs/CHANGELOG.md) — changes per version

### License

[MIT](./LICENSE)
