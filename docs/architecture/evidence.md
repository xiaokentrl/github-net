# 架构证据台账

配合 `github-net-architecture.md` 与同目录三份 `.dot` / 一份 `.dsl` 使用。

- 首轮核对：2026-10-28（逐文件读代码）
- **本轮复核：2026-10-28，逐条重跑取证命令**，修正了 6 处行号越界、2 处比代码弱的说法，并把出包/装包链路查到实际调用点。核对方式列在每条证据里。
- **2026-10-29 追加，只动了 Hosts 这一块**：新增渲染、恢复元数据、事务纯逻辑三条 component 证据（行号来自 `grep -n "^func Render\|Digest\|Check\|validate\|Apply\|RestoreItem\|RestoreAll\|restoreInto"` 的当场输出），并把「零调用点」这条命令实跑过一遍（`grep -rn "hosts\.Render\|hosts\.Apply\|RestoreItem\|RestoreAll\|hosts\.Check\|hosts\.Digest\|hosts\.Recovery" main.go internal/app` → 0 命中）。其余各行仍是 2026-10-28 那轮的事实，本轮没复核，也没改。

可信度口径：**high**＝代码/配置/产物直接支持；**medium**＝多处间接信号一致但无直接来源；**low**＝按命名或惯例推断；**unknown**＝故意留白，待查。

## 1. 节点

| 节点 | 类型 | 状态 | 证据（sourceRefs，均为本轮实读/实跑） | 可信度 |
| --- | --- | --- | --- | --- |
| Go 主进程（唯一交付二进制） | container | current | `main.go:16-20` `main.go:29-74` `main.go:109-131` | high |
| 前端 SPA（React 18 + Vite） | container | current | `frontend/package.json` `main.go:16-17`（`go:embed all:frontend/dist`） | high |
| WebView 独立渲染进程 | 运行时事实 | unknown | 本仓库无证据；按 WebKitGTK 多进程模型推断 | medium（待实测） |
| 「运行期不监听端口」 | 运行时事实 | unknown | 本仓 Go 代码里 `net.Listen`、`http.Get`、`http.Client`、`net.Dial`、`websocket` 全部零命中（`grep -rn` 实跑，范围 `main.go` + `internal/`）；但自定义 scheme 还是本地回环由 Wails 内部决定，仓库不体现 | medium（待实测） |
| `cmd/identity` 取值命令 | container | current（未接构建配置） | `cmd/identity/main.go:1-58` | high |
| `internal/app.Service` | component | current | `internal/app/app.go:15-161` `frontend/bindings/github.com/xiaokentrl/github-net/internal/app/service.ts:26-65` | high |
| 6 个绑定方法（RuntimeInfo/Ping/GetItems/GetAppStatus/GetSettings/SaveSettings） | contract | current | 生成的 `service.ts` 逐个函数（grep `export function` 命中 6 处：26/35/43/50/57/65 行） | high |
| 前端只登记 4 个已接命令 | component | current | `frontend/src/api/client.ts:1-57`，第 2 行注释「只登记后端已经接好的 4 个命令，其余 14 个仍是契约」 | high（注释口径）；「14」这个总数来自规划文档，本轮未在代码里逐项数过 |
| `internal/catalog` + `catalog.json` | component + data | current | `internal/catalog/catalog.go:14-188` `internal/catalog/catalog.json:1-12` | high |
| 目录规模 11 分类 / 54 项目 / 65 去重主机名 / 11 scopeID | metric | current | python 实数 `catalog.json`（命令见架构文档第 7 节）：categories=11、items=54、unique hostnames=65、scopes=11 | high |
| 抽取来源＝原型 `CATEGORY_DEFS` 1131-1208 行 | provenance | current | `internal/catalog/catalog.json` 的 `source` 字段（自述，未逐行回查原型） | high（文件自述） |
| `internal/config.Store`（revision + 幂等 + 原子替换 + 隔离） | component | current | `internal/config/config.go:272-490` `config.go:458-490`（writeAtomic） | high |
| `config.json` 路径规则（三平台） | data | current | `internal/config/config.go:147-171` | high |
| `internal/hosts` 区段只读解析（BEGIN/END v1） | component | current | `internal/hosts/parser.go:89-170` `internal/hosts/manager.go:25-38` | high |
| `internal/hosts` 区段渲染（只算字符串，不写盘、无调用点） | component | current | `internal/hosts/renderer.go:27`（`Render`；改坏实测 15 次见技术规划阶段四） | high |
| `internal/hosts` 恢复元数据与冲突判定（`Recovery`/`Digest`/`Check`/形状门） | component | current | `internal/hosts/backup.go:106`（`Digest`） `:134`（`Check`） `:252`（`validate`）；改坏实测 M1、M3–M10 | high |
| `internal/hosts` 应用与还原的事务纯逻辑（`Apply`/`RestoreItem`/`RestoreAll`，快照累计与共认领） | component | current | `internal/hosts/transaction.go:34`（`Apply`） `:115`（`RestoreItem`） `:136`（`RestoreAll`） `:158`（`restoreInto`）；改坏实测 M11–M21，`main.go`/`internal/app` 零引用 | high |
| Hosts 写权限探测（打开即关，不写） | component | current | `internal/app/privilege.go:40-53` | high |
| `app:runtime-status` 事件（2 秒、序号单调递增） | event | current | `internal/event/event.go:9-36` `main.go:25`（`statusTick`） `main.go:140-155` | high |
| 前端事件订阅与丢弃计数 | component | current | `frontend/src/api/events.ts:3-20` `frontend/src/app/App.tsx:128-149`（useEffect：133 订阅、134-137 丢弃并计数、142-148 清理） | high |
| 7 个稳定错误码 | contract | current | `internal/app/errors.go:12-20`（empty_token、catalog_invalid、internal_error、invalid_settings、revision_conflict、settings_save_failed、config_schema_newer） | high |
| `internal/accelerator/strategies`（contract/registry/dns-hosts） | component | current 代码、**已进运行时**（2026-10-29 接进装配层） | `strategies/registry.go:13-59` `strategies/hosts_dns_lookup.go:38-56`；`grep -rn "accelerator/strategies" --include=*.go .` 2026-10-29 重跑：`internal/app/app.go` 2 命中（import + `strategies.IDs()`），此前零命中 | high（代码存在）/ high（确认已接线） |
| `internal/identity` 16 个身份键 | component | current 代码 | `internal/identity/identity.go:15-57` `identity.go:67-86` | high |
| 系统 Hosts 文件 | external | current（只读） | `internal/app/privilege.go:24-36` | high |
| 操作系统 DNS | external | **策略已注册进装配层，但还没有执行路径真的调它** | `strategies/hosts_dns_lookup.go:36`（`net.DefaultResolver`） | high（代码）/ low（运行时会不会走到，取决于检测那一屏） |
| GTK4 + WebKitGTK 6.0 运行库 | external | current | `.github/workflows/ci.yml` 的 `RUNTIME_LIBS`（3 个 .so）`build/linux/nfpm/nfpm.yaml` 的 `depends` | high |
| GitHub Actions CI（4 条作业 + 门一门二） | external | current | `.github/workflows/ci.yml` 全文（本轮整读） | high |
| 打包（deb/rpm/aur） | build | current，**只在本地链上跑** | `build/linux/Taskfile.yml:153-166`（`wails3 tool package -config ./build/linux/nfpm/nfpm.yaml`）`scripts/pre-push-reinstall.sh:80`（`wails3 task linux:create:deb`）；`ci.yml` 里没有任何打包步骤 | high |
| 桌面项文件是构建产物（且已入库） | data | current | `build/linux/Taskfile.yml:171-177`（`generate:dotdesktop` 每次重新生成同名文件并追加 `StartupWMClass`） | high |
| 本机 pre-push 免密重装 | external | current | `.githooks/pre-push:1-7` `scripts/pre-push-reinstall.sh:1-14,80,90,98-99` `scripts/lib/identity-guard.sh:21,58`；`git config --get core.hooksPath` 实跑返回 `.githooks` | high |
| root 助手不 source 仓库脚本（提权入口考量） | decision | current | `scripts/github-net-root-install.sh:7` `scripts/lib/identity-guard.sh:16-18` | high |

## 2. 关键边

| 边 | 类型/协议 | 证据 | 可信度 |
| --- | --- | --- | --- |
| `main` → `app.Service` | 进程内注册 | `main.go:63-65`（`application.NewService(service)`） | high |
| `Service` → `catalog.Load` | 进程内，启动一次 | `app.go:41` | high |
| `Service` → `config.NewStore(path, scopeIDs, nil)` | 进程内 | `app.go:62-68`（第 67 行末参就是 `nil`） | high |
| `Service.GetItems` → `hosts.Read` | 进程内 + 文件读，每次现读 | `app.go:99` | high |
| `Service.GetAppStatus` → `ProbeHostsAccess` | 进程内 + 打开即关 | `app.go:116` `privilege.go:40-53` | high |
| `hosts` / `catalog` 哨兵错误 → `app.Error` | 进程内翻译 | `errors.go:56-75` | high |
| SPA → Go（6 方法） | Wails JS↔Go 桥，同步 Promise | `frontend/bindings/.../service.ts:26-65` | high |
| Go → SPA（状态事件） | Wails 事件，异步 | `main.go:142`（`Event.Emit`）`frontend/src/api/events.ts:15`（`Events.On`） | high |
| SPA 资源加载 | 内置资源服务器（自定义 scheme 还是本地回环未定） | `main.go:66-68`（`AssetFileServerFS(assets)`）`main.go:88`（`URL:"/"`） | high（挂载）/ medium（传输方式） |
| `strategies` → `catalog`/`hosts` | 进程内，**注册时走 catalog，检测执行路径还没上** | `hosts_dns_lookup.go:61`（`catalog.Load`）`hosts_dns_lookup.go:85`（`NormalizeHostname`） | high（代码） |
| `pre-push` → `cmd/identity` | shell，eval 一次 | `identity-guard.sh:39` | high |
| 推送 → 出包 → 停进程 → 卸装 → 删配置 | shell，顺序实读 | `pre-push-reinstall.sh:80`（`wails3 task linux:create:deb`）→ `:90`（`pkill -x`）→ `:70-76`（`dpkg --purge` + `apt-get install -y ./deb`）→ `:98-99`（先过空值门再 `rm -rf` 三个用户目录） | high |
| root 助手实际用的包命令 | shell | `scripts/github-net-root-install.sh:37`（`dpkg --purge`）`:39`（`apt-get install -y "$DEB"`，不是 `dpkg -i`） | high |
| nfpm 装 3 个文件 + 挂 postinstall | 打包 | `build/linux/nfpm/nfpm.yaml:19-25`（contents，三个 dst 在 21/23/25 行）`:47-51`（只挂 `postinstall`，`preremove`/`postremove` 是注释掉的；目录里的 `preinstall.sh` 没被引用） | high |
| postinstall 做两件事 | 装包后脚本 | `build/linux/nfpm/scripts/postinstall.sh:5-18`（`update-desktop-database` + `update-mime-database`） | high |
| `nfpm.yaml` → `internal/identity` | 应为构建期取值，**实际仍是抄写字面量** | `build/linux/nfpm/nfpm.yaml:6,9`（name/version）、`:21-25`（三个 dst）对比 `identity.go:36-57` | high（缺口确认） |
| `Taskfile` 与 `build/config.yml` → identity | 同上，未接 | `Taskfile.yml:4`（`APP_NAME: "github-net"`）`build/linux/Taskfile.yml:177`（`DESKTOP_ID: 'io.github.xiaokentrl.githubnet'`）`build/config.yml` 的 `info` 段 | high（缺口确认） |

## 3. 明确不算事实的说法

- 「工具会写 Hosts」——**不成立**。当前只有路径推导、写权限探测、区段只读解析三件事（`privilege.go:10-21` 注释即为此写）。
- 「DNS 检测已就绪」——**不成立**。`DNSDetectionReady` 恒为 `false`（`app.go:117-118`）。
- 「applied/partial 状态会出现」——**当前不会**。这两档要求可比对的写入摘要（`dto.go:130-172`）。
- 「CI 绿 = 各环境可跑」——**不成立**。arm64、Windows、macOS、真机（非容器）都没进门禁。
- 「CI 会出包」——**不成立**。`ci.yml` 只有前端构建、后端检查、`go build` 产物过门；出包只在本地推送链上跑。
- 「已发布 0.1.0」——**不成立**。本轮复核：`git ls-remote --tags origin` 无输出（0 个标签），GitHub Releases API 返回 `200` 且正文是 `[]`（0 个 release）。
- 「界面支持六套主题」——**不成立**。只移植 `light`，其余五套存着但按 light 显示并说明（`App.tsx:24-35`，`App()` 整体在 `:35-291`）。

## 4. 待你落句、图上未定案的分歧

2026-10-29 一轮把下表前五项全部落句并落地，只剩「关窗口＝退出」仍是已定行为、没改。落句口径的完整说明在 `TECHNICAL_PLAN.md` 阶段二末尾「七项待落句一次落定」。

| 项 | 现状 | 影响哪块 |
| --- | --- | --- |
| 每轮候选探测上限 108 vs 130 | **已落句：默认 130、可填上限 1040**（按目录去重后 65 个 hostname × 档位候选上限 2／16 现算；`config.go` 同名常量） | `internal/config` 校验边界、界面预算输入；两道 parity 门钉住前后端数字 |
| 托盘两个开关 | **已落句：本轮不加**——托盘屏未迁进工程，没有真实行为可指，先加就是假开关 | `internal/config` `startup` 分组保持已定形状 |
| 工具栏 A/B | **已落句：B 案**——保留第一屏实测过的档位下拉，旁边补一颗跳转「网络与流量」的按钮 | 前端 `Toolbar.tsx`/`App.tsx`，新增两个文案键 |
| `cleanup-actions.yml` 是否删历史 | **已落句：继续跑，`keep` 1→10**，保住门禁正文/日志的证据窗口；触发不变 | 发布与运维流程，不在运行架构里 |
| 五个未引用资源 | **已落句：删掉**（`frontend/public` 下 5 个，合计 702,045 B 原本随 `embed all:frontend/dist` 进二进制）；同目录的 `Inter Font License.txt` 不在点名的五个里，仍等一句 | 资源与包内容 |
| 「关窗口＝退出」是否改成收进托盘 | 仍是已定行为 | `main.go` 窗口与托盘逻辑 |

## 5. 本轮复核改了什么（留痕，免得下次以为原稿就对）

| # | 原说法 | 复核结果 | 处理 |
| --- | --- | --- | --- |
| 1 | `events.ts:4-24` | 文件只有 20 行，越界 | 改 `events.ts:3-20`；`Events.On` 精确到 `:15` |
| 2 | `client.ts:1-52` | 文件 57 行 | 改 `client.ts:1-57` |
| 3 | `App.tsx:129-152` | useEffect 在 128-149，152 已是 `hostsOf` | 改 `App.tsx:128-149` |
| 4 | `App.tsx:38-210` | `App()` 实为 35-291 | 改 `App.tsx:35-291` |
| 5 | `app.go:118-119` | 119 是右花括号 | 改 `app.go:117-118` |
| 6 | `identity-guard.sh:17-20` | 「例外」注释实为 16-18 行 | 改 `identity-guard.sh:16-18` |
| 7 | 「nfpm 的 postinstall 刷桌面库」 | 它刷两个库（desktop + mime） | 补全，并给出脚本行号 |
| 8 | 「root 助手 dpkg 卸旧包 + 装新包」 | 装新包用的是 `apt-get install -y ./deb`（会自动补依赖），不是 `dpkg -i` | 按实际命令改写 |
| 9 | 「打包（本机或 CI 手动触发）」 | CI 里没有任何打包步骤 | 改成「只在本地链上跑」，补 Taskfile 调用点 |
| 10 | 「nfpm.yaml + 4 个脚本」 | 4 个脚本里只有 `postinstall` 被挂上，`preinstall.sh` 无人引用，另两个在 yaml 里是注释 | 按实际挂载关系写 |
| 11 | 「0 个 release、0 个标签」（沿用记忆） | 标签与 release 本轮都能自助复核且都为 0 | 升成本轮实测证据 |
| 12 | 「无监听端口」当作事实 | 只是框架常识 + 本仓零命中，仓库不体现传输方式 | 降级为 medium，列进待实测 |
| 13 | 未提「桌面项文件是构建产物」 | `generate:dotdesktop` 每次重新生成同名入库文件 | 补进节点表，并说明 G3 抄写会连带影响它 |
| 14 | `dependencies.dot` 里 `bindingsT -> wails` 带 `constraint=false` | 真 dot 报 `Unable to reclaim box space in spline routing`，说明这条边的跨簇走线在挤盒子 | 去掉该属性，让边参与排层；重跑零警告 |
| 15 | `runtime-topology.dot` 用 `shape=cloud` | graphviz 14.1.2 报 `using box for unknown shape cloud`，图面退化 | 改 `shape=note` + 虚线边框；重跑零警告 |

## 6. 渲染状态（本轮实测）

- `dot` 版本 14.1.2（Ubuntu 26.04 候选包）。本机没有 root，走的是「deb 解包到 `~/.local/opt/graphviz` + `~/.local/bin/dot` 包装脚本」这条路，不需要 `sudo`。
- 三张 SVG 已出：`dependencies.svg`、`runtime-topology.svg`、`build-release-pipeline.svg`，同目录；三条渲染命令全部零警告退出。
- 中文标签量得准：SVG 里文字宽度与盒子宽度对得上（误差在字体度量的正常范围内），不再有「非 ASCII 字符没有宽度」那类退化。
- 大幅面提醒：依赖图 3014×872pt、发布链 4960×764pt，得在浏览器里缩放看；这两张是派生产物，改图改 `.dot`。
