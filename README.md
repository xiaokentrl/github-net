# GitHub Net

[![CI](https://github.com/xiaokentrl/github-net/actions/workflows/ci.yml/badge.svg)](https://github.com/xiaokentrl/github-net/actions/workflows/ci.yml)

[中文](#中文) · [English](#english)

---

> **产品身份配置指针**：产品名字的唯一手写源是 `internal/identity/identity.go`，别处不再抄一份；把这些值钉死的金标准在 `internal/identity/identity_test.go`；唯一的安全例外是免密 root 助手 `scripts/github-net-root-install.sh`，它按字面把包名写在自己文件的 `PKG` 一行，一致性靠那个测试反向核对。改户口层的值（产品 ID、包名、数据目录名、Hosts 区段标记）之前，先读那个包的头部注释和金标准，并在 PR 里写清迁移影响：新旧值同时存在的时刻会发生什么、怎么迁、迁失败怎么回来。

## 中文

GitHub Net（GitHub 加速助手）是一个用 Go、Wails v3、React 和 TypeScript 构建的桌面应用。它把 GitHub 等站点的可用 IP 写进系统 Hosts 来加速访问：不改系统代理、不装虚拟网卡、不留常驻后台流量。加速方法按独立策略扩展，一种方法一个 Go 文件。

### 主界面

![GitHub Net 主界面](./docs/screenshots/main-window.png)

1280×800 首次启动的样子：左边分类树，中间服务详情与候选 IP 表，下面任务队列，最后一行状态栏（档位、Hosts 路径与可写性、DNS 来源、上次检测、事件序号、目录计数）。截图是同一套前端在无头浏览器里跑真后端（`go build -tags server`）拍的，不是原生无边框窗口——所以右上角那三个窗口按钮在这张图里点了没作用。

### 功能特性

- **产品目录**：11 个分类、52 个项目、去重后 63 个主机名，数据在 `internal/catalog/catalog.json`。
- **六种加速方法，单文件自注册**：GitHub 分类下是 GitCDN / HelloGitHub / GitLab（ineo6）三家公共源、「全部公共源」（三家合并，GitHub 的默认）和「使用已有储存」（读本机 SQLite 底账）；其余分类用「自动查找可用 IP」（系统配置的 DNS 解析）。新增方法写一个 Go 文件、在 `init()` 里 `Register`，不改注册表、路由和界面。
- **真实可见的检测**：候选逐行显示排队中 / 检测中 / 可用 / 不可用 / 超时 / 已取消 / 预算用完未检测，带延迟；单项一轮 5 秒截止，DNS 查询与 TCP+TLS 探测各 2 秒预算，不伪造进度和剩余时间。
- **省流三档**：省流（每主机名 2 个候选、并发 2）/ 标准（8 / 8）/ 完整检测（16 / 8）；每轮探测总上限默认 126、可填 1–1008，够不够覆盖一轮在「网络与流量」页和工具栏档位按钮上先看得见，批量检测覆盖不全时先弹确认。只做 DNS 查询和 TCP/TLS 握手，不下载网页内容，不自动重试。
- **任务队列**：检测、应用、还原统一入队、串行执行，九档状态；面板给分轮进度与耗时，支持暂停、取消等待、重试失败、清空已完成、清空队列、全部还原；状态存在应用级 store 里，切页面不丢进度。批量应用按项目逐项判定：勾了几项这一轮就有几行，做不了的那几项也进同一轮、以「跳过」落定并把原因写在那一行的结果里（没检测过、最近一轮没有可用候选），不会悄悄少掉。
- **替换逐条看得见**：「已替换」不再只有一个词，但逐条明细不插进主菜单（2026-11-02 定）：主菜单的一行只有这一项自己的名字、色点和状态字。逐条摊开放在两个地方——详情页把域名摊成每个域名一行 `域名 → 现在写到的 IP` 或 `域名 → 未写入`；队列明细的「当前阶段 / 结果」按状态各说一句实话（应用「已写入 2/3 条」、跳过把没做的原因原样写出来、还原「已还原，这 3 条不再由本工具写」、检测「检测完成，若干个域名里若干条可用」），行下的逐条替换默认就铺开，▸/▾ 留着给手动收起。这些数字只来自后端这次读回来的逐条事实（`HostsState.entries`，归属取自特权服务的写入记录）；判不出归属（冲突、读不到）时后端给空清单，这时逐条整块不出现、退回域名清单，界面不自己编 IP。
- **Hosts 安全**：界面进程永不直接写盘——写入交给 root 常驻的辅助服务 `github-net-helper`（Unix socket `/run/github-net/helper.sock`，白名单只有 ping / apply / restoreItem / restoreAll / hostsState 五个操作）。写前备份，只改带本工具标记的区段，原子替换后重读校验，失败自动回滚，可一键还原到应用前状态，也可直接打开 Hosts 查看。
- **权限最小化**：主界面始终以普通用户运行。Linux 装包时由 postinst 一次性 enable 并 restart 那个 systemd 服务，之后读写 Hosts 不再反复要 root。
- **开机自动化**：开机自启（Linux 写 XDG autostart）、启动后自动检测（可多选哪些主菜单参与，一个没勾就不跑）、检测完成自动应用、自动应用队列跑完真退出进程；状态栏回显这一轮跑到哪、哪些菜单没排上、为什么没退出。
- **候选底账**：每轮检测结果落 SQLite（`detect-history.db`，`internal/store`），「使用已有储存」直接复用它，不重复探测。
- **自动更新**：检查 GitHub Releases 最新稳定版，下载前显示版本、来源和大小，SHA256 与 Ed25519 签名双校验通过才交给系统安装器；嵌入公钥缺失一律拒绝。
- **设置中心八页**：布局、界面缩放、外观、网络与流量、托盘、开机自启、检测升级、关于我们；改动即时经后端统一保存，跨重启生效。
- **外观与窗口**：六套主题（晨光白是唯一浅色，另五套深色，含纯黑 OLED 档）、界面缩放四档（90/100/110/125%）、侧栏宽度可拖（200–600）、队列高度三档加拖动条；无边框窗口自绘标题栏，配系统托盘（显示主窗口、最小化到托盘、主题子菜单、退出，托盘提示跟着队列是否在跑变）。
- **提示面板**：窗口右下角的公共提示，四类（错误、警告、提醒、成功），跟着窗口位置走；每条能关闭和复制，整块能清空、收起成胶囊、按住标题栏拖动（双击归位）。那颗胶囊小按钮常驻右下角：一条提示都没有时它也在，点开是「暂无提示」，再收起又回到小按钮（2026-11-02）。错误不自己消失，其余 20/8/6 秒，收起期间倒计时冻结。应用与还原、队列任务到终态、被后端挡下的操作、检查更新都往这里递话；它是补充，按钮旁的原位红字照旧留着。
- **双语界面**：简体中文默认，繁体中文可切换，两份语言包各 395 个键；缺键时界面直接印「缺文案:键名」，不静默兜底。
- **无障碍**：分类树用 `role="tree"` 加方向键操作，设置对话框 `aria-modal` 且 Esc 可关，保存结果与错误经 `aria-live` / `role="alert"` 播报。

### 已知边界

- 只有 Linux x64 进了 CI 与发布流水线，也只有它经过实机验证；Windows 和 macOS 有打包配置但没验证、不在发布矩阵——它们的特权辅助服务和安装交接还没做。arm64 同样没验。
- GitHub 分类不提供系统 DNS 解析法（拍板 2026-10-31）：候选只来自公共源和本机储存。
- 候选表的「评分」列恒为 `—`（评分还没做）；托盘菜单和队列分页那几个词还是硬编码中文，切繁体不会跟着变。
- 工具栏「模式」下拉只有 Hosts 可选，系统代理和 PAC 两项照原型置灰——它们还没实现，选中也不会改变行为。

### 工作原理

1. 取候选：GitHub 走三家公共 hosts 源（或本机 SQLite 底账），其余分类用系统配置的 DNS 解析域名；查询不转发给第三方解析器，也不记录完整查询域名。
2. 验候选：对每个地址做 TCP 连接 + TLS 握手，保留主机名和证书校验，不下载页面内容。
3. 选与写：界面逐行给状态和延迟，由用户决定用哪个；点「应用」入队，辅助服务备份后把选中映射原子写进系统 Hosts，只动本工具标记的区段，写完重读校验。
4. 回来：按恢复元数据还原到首次写入前的状态，或直接「全部还原」。
5. 扩展：加速方法从单个文件自注册，未来加系统代理、PAC 各写一个 Go 文件，不动中心注册表和界面。

### 使用流程

1. 左侧分类树选分类或单个项目。
2. 在详情顶部的「GitHub 全部加速方法」里选方法（GitHub 默认「全部公共源」）。
3. 点「检测」或工具栏「检测全部」，逐个候选确认可用性；档位就在检测按钮旁边，点了立刻切省流 / 标准 / 完整检测。
4. 点「应用到 Hosts」写入；需要撤销时「恢复」回应用前状态。任务都在下方队列里排队，可暂停、取消、重试。

### 下载与安装

到 [Releases](https://github.com/xiaokentrl/github-net/releases) 下载对应平台的安装包（打一个 `v<版本号>` 标签才会触发流水线建 Release，没有发布时该页为空）：

| 平台 | 安装包 | 现状 | 系统要求 |
|---|---|---|---|
| Linux x64 | `.deb` | 发布流水线出包并签名，附 `.deb.sig` 与 `checksums.txt` | Debian 13、Ubuntu 26.04 及兼容发行版（GTK4 / WebKitGTK 6.0） |
| Linux | `.rpm` / `.AppImage` / Arch 包 | 本地任务已备好，未进流水线，不发布 | 同上 |
| Windows | `.exe`（NSIS）/ MSIX | 只有构建配置，未验证、不发布 | — |
| macOS | `.dmg` / `.app` | 只有构建配置，未验证、不发布 | — |

Linux 系统要求详见下文「开发和构建」里的发布线说明。

### 开发环境

- Go 1.27.3 或更高（`go.mod` 的 `go` 指令）
- Wails v3.0.0 命令行（`wails3`）
- Node.js 和 npm

### 开发和构建

```sh
wails3 dev          # 热重载开发
wails3 build        # 当前平台产物
wails3 task check   # 提交前自检：gofmt、go vet、前端语言包一致、前端类型检查
wails3 task test    # Go 测试（每次真跑，不用缓存）
```

Linux 只有一条发布线：默认 GTK4/WebKitGTK 6.0，构建宿主为 Ubuntu 26.04（本机即该版本，或等效容器 `ubuntu:26.04`），也就是上面的 `wails3 build`。发布目标是 Debian 13 与 Ubuntu 26.04。Debian 12 不在目标内——它的仓库没有 `webkitgtk-6.0`，默认后端编不过；Ubuntu 24.04 也已于 2026-10-27 经裁决移出，改由 26.04 LTS 取代。`-tags gtk3` 只留作开发者本机备选，官方不为它出包、不承诺修复。

Linux 构建需要 `libgtk-4-dev libwebkitgtk-6.0-dev`（另需 gcc、pkg-config、binutils，CGO 打开）。

### 打包（出包）

一条命令走完「自检 → 测试 → 构建 → 打安装包」：

```sh
wails3 task release:local
```

产物在 `bin/`，跑完会列出来：`bin/github-net`（主程序）、`bin/github-net-helper`（特权辅助服务）、`bin/github-net.deb`（安装包，发布矩阵里就是这一颗）。任何一步失败即中止，不会带着红的自检去打包。

包里的版本号取自 `main.go` 的 `appVersion`（`scripts/version.sh` 读它，出包前自动同步进 `build/linux/nfpm/nfpm.yaml`）。`release:local` 不改版本号：改版本是发版时的独立动作，打标签时 CI 会核对标签和 `appVersion` 是否一致，自动 +1 只会让两者对不上。

想单独做其中一步：

```sh
wails3 task check              # 只自检，不出产物
wails3 task test               # 只跑 Go 测试
wails3 task build              # 只构建两颗二进制
wails3 task linux:create:deb   # 只打 deb（同族 create:rpm / create:appimage / create:aur）
wails3 task package            # 当前平台全部格式（Linux：AppImage + deb + rpm + Arch，另需 rpmbuild 等工具）
wails3 task                    # 列出全部任务
```

产物能不能装某台机器，看实测的两道门：用 `objdump -T` 查产物要求的最高 `GLIBC_x.y` 是否不高于 `GLIBC_2.41`（最低目标 Debian 13 的 glibc），用 `ldd` 查目标发行版是否有 `libgtk-4.so.1`、`libwebkitgtk-6.0.so.4` 和 `libjavascriptcoregtk-6.0.so.1`。同一份代码在 Ubuntu 26.04 编出的产物要求 `GLIBC_2.38`，放进 Debian 13 容器 `ldd -r` 无未解析符号，所以一台 26.04 宿主可以同时供 Debian 13 与 Ubuntu 26.04 两条线；Debian 13 那边只验过链接，实机运行还没验。Windows 和 macOS 构建需在各自原生 runner 上验证。

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

```text
.
├── main.go                  # Wails 入口：装配服务、无边框窗口、托盘、单实例锁、更新检查、启动自动化链、生命周期
├── cmd/
│   ├── github-net-helper/   # root 常驻特权辅助服务：唯一被允许写系统 Hosts 的进程（systemd 单元在 build/linux/systemd/）
│   └── identity/            # 取值小命令：给脚本和构建项读产品身份（--key / --shell）
├── internal/
│   ├── accelerator/         # 检测编排：budget.go 算一轮预算，detect.go 串起取候选与验候选，管并发、单项 5 秒截止、逐候选状态与带序号事件
│   │   └── strategies/      # 加速方法单文件自注册：契约、注册表与六个方法（三家公共源、全部公共源、使用已有储存、系统 DNS）
│   ├── app/                 # 暴露给前端的 28 条命令、DTO 与错误转换：队列 7 条在 queue.go，检测 detect_items.go + detect.go，应用/还原 apply_items.go，更新 update.go，启动自动化 startup_automation.go，清理源缓存 sources_cache.go，启动时那一次缓存清扫在 cleanup.go（包级函数，不绑给前端）
│   ├── catalog/             # 产品数据：11 分类 / 52 项目 / 63 主机名（catalog.json）
│   ├── config/              # 用户配置：校验、revision、原子保存、旧位置迁移
│   ├── detector/            # 候选连通性与证书校验（TCP+TLS，2 秒预算），被检测编排调用
│   ├── event/               # 给前端推事件的统一出口
│   ├── hosts/               # Hosts 区段判读、渲染、恢复元数据与事务（纯计算，写盘只发生在辅助服务里）
│   ├── identity/            # 产品名字的唯一手写源：户口层常量、派生路径与钉死它们的金标准测试
│   ├── platform/            # 开机自启适配：Linux 写 XDG autostart；Windows/macOS 明确返回不支持
│   ├── privilege/           # 特权辅助服务的客户端、服务端与白名单协议（Unix socket）
│   ├── retention/           # 本机缓存「能存多久」的唯一手写出处：MaxAge = 3 天，底账/地址池/源落盘/升级包与排队新鲜期都引用它
│   ├── sources/             # GitHub 三家公共源的地址与合并解析；每源一份当天落盘缓存（sources-cache/），清掉或超过 3 天就重新下载
│   ├── store/               # SQLite 候选底账（detect-history.db）：按 3 天保留期限 + 最近 500 轮两道界自动清
│   ├── task/                # 内存任务队列：串行执行、九档状态、取消/暂停/重试/两项清理，事件序号与快照水位同源
│   └── updater/             # 检查 Releases、下载并做 SHA256 + Ed25519 双校验，再交给系统安装器
├── frontend/
│   ├── bindings/            # Wails 生成的绑定（不手改）
│   └── src/                 # React 页面与组件、i18n（zh-CN / zh-TW）、样式
├── build/                   # 各平台打包：linux（deb/rpm/AppImage/Arch + systemd + 安装脚本）、windows、darwin
├── scripts/                 # 版本号、Release notes、签名、推送前自动重装等脚本
├── docs/                    # 更新日志、Release 条目清单、界面截图
└── .github/workflows/       # ci.yml（四步校验）、release.yml（标签触发出包+签名+发布）、cleanup-actions.yml
```

### CI 与发布

`.github/workflows/ci.yml` 在 push 和 PR 上跑四步：前端 `npm ci && npm run build`（含 tsc）；后端在 `ubuntu:26.04` 容器里查 `gofmt`、`go vet ./...`、`go test -count=1 ./...`；同在 Ubuntu 26.04 宿主用默认后端出 x64 产物并跑门一（≤`GLIBC_2.41`）与本发行版门二；最后把产物放进 `debian:13` 容器复核门二。当前只覆盖 Linux x64，arm64、Windows、macOS 还没有对应任务，所以本地提交前请自行跑 `wails3 task check && wails3 task test`（就是 `gofmt`、`go vet ./...`、`go test -count=1 ./...` 加前端的语言包一致和 `tsc`）；要连产物一起验就跑 `wails3 task release:local`。

`.github/workflows/release.yml` 由 `v*` 标签触发：先核对标签与 `scripts/version.sh`（唯一来源是 `main.go` 的 `appVersion`）一致，再在 Ubuntu 26.04 容器出 deb、过门一与门二、用 `NET_SIGNING_KEY` 签出 `.deb.sig` 和 `checksums.txt`，最后连同正文建 GitHub Release；正文取仓库根 `release-notes.md`，由 `scripts/gen-release-notes.sh` 按「新增 / 修复 / 更新」三组生成。Windows 与 macOS 不在矩阵里。

### 文档

- [技术规划](./TECHNICAL_PLAN.md) — 目标、模块职责、接口、开发阶段
- [前后端结构规划](./FRONTEND_BACKEND_STRUCTURE_PLAN.md) — 目录、数据流、扩展规则
- [公共组件规划](./PUBLIC_COMPONENTS_PLAN.md) — 前后端公共组件与方法
- [更新日志](./docs/CHANGELOG.md) — 按版本记录改动

### 许可证

[MIT](./LICENSE)

---

## English

GitHub Net is a desktop app built with Go, Wails v3, React and TypeScript. It speeds up GitHub and similar sites by probing working IPs and writing them into the system hosts file: no system proxy changes, no virtual network adapters, no background traffic. Each acceleration method is an independent strategy shipped as one Go file.

### Main window

![GitHub Net main window](./docs/screenshots/main-window.png)

The first-launch window at 1280×800: category tree on the left, service detail with the candidate table in the middle, the task queue below, and a status line (profile, hosts path and writability, DNS source, last probe, event sequence, catalog counters). The capture is the same frontend talking to the real backend in a headless browser (`go build -tags server`), not the native frameless window — so the three window buttons in the top-right corner do nothing in this picture.

### Features

- **Catalog**: 11 categories, 52 items, 63 unique hostnames (`internal/catalog/catalog.json`).
- **Six acceleration methods, self-registering**: for GitHub — GitCDN, HelloGitHub and GitLab (ineo6) public sources, "all public sources" (the three merged, GitHub's default) and "use stored IPs" (the local SQLite ledger); every other category uses "find working IPs" (the OS-configured DNS). A new method is one Go file calling `Register()` from `init()` — no central registry, routing or UI changes.
- **Visible probing**: each candidate gets its own row — queued / probing / reachable / unreachable / timeout / cancelled / skipped-for-budget — with latency; a 5-second deadline per item per round, 2 seconds for a DNS query and 2 for a TCP+TLS probe, and no fake progress bars.
- **Three data profiles**: low data (2 candidates per hostname, concurrency 2) / standard (8 / 8) / full (16 / 8); the per-round probe cap defaults to 126 and is editable from 1 to 1008, whether it covers a round is shown before you start (network page and the profile button in the toolbar), and a batch probe with a shortfall asks for confirmation first. Probing sends DNS queries and TCP/TLS handshakes only — no page downloads, no automatic retries.
- **Task queue**: probing, applying and restoring all run through one serial queue with nine statuses; the panel shows per-round progress and duration and offers pause, cancel waiting, retry failed, clear finished, clear all and restore all; state lives in an app-level store, so switching pages never loses a task. A batch apply judges item by item: tick N items and the round holds N rows — the ones that cannot run stay in the same round as "skipped" with the reason on their own row (never probed, or no usable candidate in the last round) instead of silently disappearing.
- **Every replacement is visible**: "replaced" is no longer a single word, but the per-domain detail stays out of the main menu (decided 2026-11-02): a menu row holds only that item's own name, status dot and status word. The domain-by-domain breakdown lives in two places — the detail page lists one line per domain, `domain → the IP it currently resolves to` or `domain → not written`; the "stage / result" column of the queue panel states what actually happened per status — how many domains an apply wrote and how many are still missing, the reason a skipped item was not done, what a restore gave back, how many candidates a probe found usable — and the per-domain rows under a task are open by default, with ▸/▾ kept as a manual collapse. Those numbers come only from the per-domain facts the backend read on this call (`HostsState.entries`, ownership taken from the helper's write record); when ownership can't be decided (conflict, read failure) no per-domain line is shown and the plain domain list is used instead — the UI never invents an IP.
- **Hosts safety**: the UI process never writes the file — a root-resident helper (`github-net-helper`, Unix socket `/run/github-net/helper.sock`) does, and its whitelist is five operations only (ping / apply / restoreItem / restoreAll / hostsState). It backs up first, touches only the section tagged by this tool, replaces atomically, re-reads to verify, rolls back on failure, and restores on demand; an "open hosts" shortcut is there too.
- **Least privilege**: the UI always runs as a normal user. On Linux the package's postinst enables and restarts the systemd service once, so hosts read/write never asks for root again.
- **Startup automation**: launch at login (an XDG autostart entry on Linux), probe on startup for the categories you ticked (none ticked = the chain never starts), apply automatically once probing settles, and really exit the process when that auto-apply queue drains; the status bar reports how far the round got, which categories were skipped and why the app did not quit.
- **Candidate ledger**: every round is stored in SQLite (`detect-history.db`, `internal/store`), which "use stored IPs" reads back instead of re-probing.
- **Auto updates**: checks the latest stable GitHub Release, shows version, source and size before downloading, and hands the file to the system installer only after SHA256 and Ed25519 signature verification; a missing embedded public key is a hard refusal.
- **Settings, eight pages**: layout, zoom, appearance, network & traffic, tray, startup, update check, about; every change is saved through the backend immediately and survives a restart.
- **Look and shell**: six themes (Daylight is the only light one; OLED Black is pure black), four zoom levels (90/100/110/125%), a draggable sidebar (200–600 px), three queue heights plus a drag bar, a frameless window with a self-drawn titlebar, and a tray menu (show, minimize to tray, theme submenu, quit) whose tooltip follows whether tasks are running.
- **Notice panel**: bottom-right of the window and it follows the window; four levels (error, warning, info, success). Each notice can be dismissed and copied, the panel can be cleared, collapsed into a pill, and dragged by its header (double-click returns it to the corner). That pill is permanent (2026-11-02): it stays in the corner even with no notice at all, and opening it shows "no notices yet". Errors stay until dismissed, the rest expire after 20/8/6 s and the countdown freezes while collapsed. Apply/restore results, tasks reaching a final state, operations the backend refused and update checks all report here; it supplements the inline status text, it does not replace it.
- **Bilingual UI**: Simplified Chinese by default, Traditional Chinese available; both catalogs hold 395 keys, and a missing key prints `缺文案:key` on screen instead of silently falling back.
- **Accessibility**: the tree is `role="tree"` with arrow-key navigation, the settings dialog is `aria-modal` and closes on Esc, and save results and errors are announced through `aria-live` / `role="alert"`.

### Known limits

- Only Linux x64 is covered by CI and the release pipeline, and only it has been verified on a real machine. Windows and macOS have packaging files but no verification and no release-matrix entry — their privileged helper and install hand-off do not exist yet. arm64 is unverified too.
- GitHub deliberately has no DNS method (decided 2026-10-31): its candidates come from public sources and the local ledger.
- The "score" column in the candidate table is always `—` (scoring is not built), and the tray menu plus the queue pager labels are hardcoded Chinese — switching to Traditional Chinese does not translate them.
- In the toolbar's "mode" dropdown only Hosts is selectable; system proxy and PAC are greyed out per the prototype — they are not implemented, and picking them changes nothing.

### How it works

1. Collect candidates: GitHub reads the three public hosts sources (or the local SQLite ledger); other categories resolve through the OS-configured DNS. Queries are never forwarded to a third-party resolver and full hostnames are not logged.
2. Verify: TCP connect plus TLS handshake per address, keeping hostname and certificate validation; no page content is downloaded.
3. Choose and write: the UI lists every candidate with state and latency and the user picks; "Apply" enqueues a task, the helper backs up and atomically writes the chosen mappings into the system hosts file, touching only this tool's section, then re-reads to verify.
4. Undo: restore to the pre-apply state from the recovery metadata, or "restore all".
5. Extend: methods self-register from a single file, so a future system-proxy or PAC method ships as one Go file with no registry or UI changes.

### Usage

1. Pick a category or a single item in the tree.
2. Choose a method under "All acceleration methods for GitHub" ("all public sources" is the default).
3. Hit "Probe" — or "Probe all" in the toolbar — and review each candidate; the profile button next to the probe button switches low/standard/full.
4. Hit "Apply to hosts"; "Restore" returns to the pre-apply state. Tasks queue in the panel below, where you can pause, cancel and retry them.

### Downloads

Installers are published at [Releases](https://github.com/xiaokentrl/github-net/releases) — a `v<version>` tag is what triggers the pipeline, so the page is empty until a release exists.

| Platform | Package | Today | Requirements |
|---|---|---|---|
| Linux x64 | `.deb` | Built and signed by the release pipeline, with `.deb.sig` and `checksums.txt` | Debian 13, Ubuntu 26.04 and compatible distros (GTK4 / WebKitGTK 6.0) |
| Linux | `.rpm` / `.AppImage` / Arch package | Local tasks exist; not in the pipeline, not published | same as above |
| Windows | `.exe` (NSIS) / MSIX | Build files only — unverified, not published | — |
| macOS | `.dmg` / `.app` | Build files only — unverified, not published | — |

See the release-line notes under "Development and build" for Linux requirements.

### Development environment

- Go 1.27.3 or later (the `go` directive in `go.mod`)
- Wails v3.0.0 CLI (`wails3`)
- Node.js and npm

### Development and build

```sh
wails3 dev          # hot reload
wails3 build        # binary for the current platform
wails3 task check   # pre-commit self-check: gofmt, go vet, locale key parity, frontend typecheck
wails3 task test    # Go tests (always re-run, no cache)
```

Linux has a single release line: the default GTK4/WebKitGTK 6.0 backend, built on an Ubuntu 26.04 host (the native OS or an equivalent `ubuntu:26.04` container) via `wails3 build` above. Release targets are Debian 13 and Ubuntu 26.04. Debian 12 is out of scope — its repositories lack `webkitgtk-6.0`, so the default backend does not compile; Ubuntu 24.04 was dropped by decision on 2026-10-27 in favour of 26.04 LTS. `-tags gtk3` remains a developer-side fallback only; no official packages and no fix guarantees for it.

Linux builds need `libgtk-4-dev libwebkitgtk-6.0-dev` (plus gcc, pkg-config, binutils, CGO enabled).

### Packaging

One command runs "self-check → tests → build → installer":

```sh
wails3 task release:local
```

The artifacts land in `bin/` and are listed at the end: `bin/github-net` (main app), `bin/github-net-helper` (privileged helper service), `bin/github-net.deb` (the installer — the one in the release matrix). Any failing step aborts the run, so a red self-check never reaches packaging.

The version inside the package comes from `appVersion` in `main.go` (`scripts/version.sh` reads it and it is synced into `build/linux/nfpm/nfpm.yaml` before packaging). `release:local` does not bump it: bumping is a separate release-time action, and the CI tag job compares the tag against `appVersion`, so an automatic +1 would only make the two disagree.

To run one step on its own:

```sh
wails3 task check              # self-check only, no artifacts
wails3 task test               # Go tests only
wails3 task build              # build the two binaries only
wails3 task linux:create:deb   # deb only (siblings: create:rpm / create:appimage / create:aur)
wails3 task package            # every format for the host platform (Linux: AppImage + deb + rpm + Arch, needs rpmbuild etc.)
wails3 task                    # list all tasks
```

Whether a binary can be installed on a given machine is decided by two measured gates: `objdump -T` must show no `GLIBC_x.y` above `GLIBC_2.41` (the glibc of Debian 13, the oldest target), and `ldd` must find `libgtk-4.so.1`, `libwebkitgtk-6.0.so.4` and `libjavascriptcoregtk-6.0.so.1` on the target distro. A binary built on Ubuntu 26.04 currently requires `GLIBC_2.38` and links cleanly inside a Debian 13 container (`ldd -r` reports no unresolved symbols), so one 26.04 host serves both lines; on the Debian 13 side only linking has been verified, not a real desktop run. Windows and macOS builds need to be validated on their native runners.

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

```text
.
├── main.go                  # Wails entry: service wiring, frameless window, tray, single-instance lock, update check, startup-automation chain, lifecycle
├── cmd/
│   ├── github-net-helper/   # Root-resident privileged helper: the only process allowed to write the system hosts file (systemd unit in build/linux/systemd/)
│   └── identity/            # Small value command: lets scripts and build steps read the product identity (--key / --shell)
├── internal/
│   ├── accelerator/         # Detection orchestration: budget.go works out one round's budget, detect.go chains candidate lookup with verification and owns concurrency, the 5-second item deadline, per-candidate statuses and sequenced events
│   │   └── strategies/      # Methods, one file each: contract, registry and the six implementations (three public sources, all-sources, stored IPs, system DNS)
│   ├── app/                 # The 28 commands bound to the frontend, DTOs and error mapping: 7 queue commands in queue.go, probing in detect_items.go + detect.go, apply/restore in apply_items.go, updates in update.go, automation in startup_automation.go
│   ├── catalog/             # Product data: 11 categories / 52 items / 63 hostnames (catalog.json)
│   ├── config/              # User config: validation, revision, atomic saves, migration from the old location
│   ├── detector/            # Candidate reachability and certificate checks (TCP+TLS, 2-second budget), called by the orchestration
│   ├── event/               # Single outlet for events pushed to the frontend
│   ├── hosts/               # Managed-block parsing, rendering, recovery metadata and transactions (pure logic; writing only happens inside the helper)
│   ├── identity/            # The only handwritten source of product names: identity constants, derived paths and the golden test that pins them
│   ├── platform/            # Launch-at-login: an XDG autostart entry on Linux; Windows/macOS answer "unsupported"
│   ├── privilege/           # Privileged helper client, server and whitelisted protocol (Unix socket)
│   ├── sources/             # The three GitHub public source endpoints and their merged parsing; one on-disk cache per source per day (sources-cache/), re-downloaded only after it is cleared
│   ├── store/               # SQLite candidate ledger (detect-history.db)
│   ├── task/                # In-memory queue: serial execution, nine statuses, cancel/pause/retry/two cleanups, event sequence shared with the snapshot watermark
│   └── updater/             # Latest-release checks, download with SHA256 + Ed25519 verification, then hand-off to the system installer
├── frontend/
│   ├── bindings/            # Wails-generated bindings (do not edit by hand)
│   └── src/                 # React pages and components, i18n (zh-CN / zh-TW), styles
├── build/                   # Packaging per platform: linux (deb/rpm/AppImage/Arch + systemd + install scripts), windows, darwin
├── scripts/                 # Version, release-notes, signing and pre-push reinstall helpers
├── docs/                    # Changelog, release note entry lists, screenshots
└── .github/workflows/       # ci.yml (four gates), release.yml (tag-triggered build + sign + publish), cleanup-actions.yml
```

### CI and release

`.github/workflows/ci.yml` runs four steps on push and PR: frontend `npm ci && npm run build` (including tsc); backend `gofmt`, `go vet ./...` and `go test -count=1 ./...` inside an `ubuntu:26.04` container; an x64 build on the default backend on an Ubuntu 26.04 host with gate one (≤`GLIBC_2.41`) and gate two for that distro; and a re-check of gate two inside a `debian:13` container. Only Linux x64 is covered for now — arm64, Windows and macOS have no tasks yet, so before pushing run `wails3 task check && wails3 task test` locally (that is `gofmt`, `go vet ./...`, `go test -count=1 ./...`, plus frontend locale key parity and `tsc`); use `wails3 task release:local` to cover the artifacts too.

`.github/workflows/release.yml` fires on a `v*` tag: it first checks the tag against `scripts/version.sh` (whose single source is `appVersion` in `main.go`), builds the deb in an Ubuntu 26.04 container, runs both gates, signs with `NET_SIGNING_KEY` into `.deb.sig` plus `checksums.txt`, and publishes the GitHub Release whose body comes from `release-notes.md` — generated by `scripts/gen-release-notes.sh` into the three fixed groups (added / fixed / updated). Windows and macOS are not in the matrix.

### Docs

- [Technical plan](./TECHNICAL_PLAN.md) — goals, module responsibilities, APIs, phases
- [Frontend/backend structure plan](./FRONTEND_BACKEND_STRUCTURE_PLAN.md) — layout, data flow, extension rules
- [Shared components plan](./PUBLIC_COMPONENTS_PLAN.md) — frontend and Go shared components
- [Changelog](./docs/CHANGELOG.md) — changes per version

### License

[MIT](./LICENSE)
