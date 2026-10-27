# github-net 整体架构（现状态）

生成日期：2026-10-28　依据：当前 main 工作树代码（已提交到远端的 `4475f5b`，加上还没提交的阶段三起手 `internal/accelerator/strategies`、`internal/identity`、`cmd/identity`）。

- **干什么用**：想知道这个工具由哪几块拼成、谁调谁、跑起来是几个进程、怎么编出来装上机器，就看这一份。
- **给谁看**：以后要改这块的人（包括下一次的我）。
- **不是什么**：不是目标态规划，也不是评审结论。图里画的是代码现在真的长什么样，没接上的地方明确画成断线。

同目录的文件分工：

| 文件 | 回答的问题 | 用什么打开 |
| --- | --- | --- |
| `github-net.structurizr.dsl` | L1 系统边界、L2 容器、L3 组件（C4 四层） | Qoder 的 Structurizr DSL 预览器 |
| `dependencies.dot` | 谁 import 谁、谁调谁（含没接线的那几条） | 同目录已渲染好的 `dependencies.svg`，或 Qoder 的 DOT 预览器 |
| `runtime-topology.dot` | 在用户机器上跑成什么、碰哪些文件、跨哪条权限边界 | 同上（`runtime-topology.svg`） |
| `build-release-pipeline.dot` | 怎么编出来、过哪几道门、怎么装上机 | 同上（`build-release-pipeline.svg`） |
| `evidence.md` | 每条断言的证据位置和可信度 | 人读 |

**读图顺序**：先看 DSL 的 `system-context`（只有 4 个外部对象，别被后面的密度吓到）→ 再看 `dependencies.dot` 找断线 → 要看落盘和权限就看 `runtime-topology.dot` → 要出包看 `build-release-pipeline.dot`。

---

## 1. 一句话讲清这套架构

**一个进程、两层界面、三类本机文件，零远端依赖。**

- **一个进程**：`go build` 出一个二进制 `/usr/local/bin/github-net`。窗口的壳、托盘、后端逻辑、事件发射都在这一个进程里（`main.go:29-160`）。它靠 CGO 链 GTK4 和 WebKitGTK 6.0，所以编译环境必须装了这两个 dev 包。
- **两层界面**：Go 侧只有 `internal/app.Service` 一个对外门面，前端只有 `src/api` 一个调用出口。两边中间那张 `frontend/bindings/` 是 Wails 生成的，不许手改——它是前后端形状的唯一对齐点。
- **三类本机文件**：内嵌进二进制的目录数据（`catalog.json`，只读）、用户配置（`config.json`，读写）、系统 Hosts（`/etc/hosts`，**当前只读**）。
- **零远端依赖**：没有服务端、没有数据库、没有更新检查、没有任何第三方网络调用。唯一的出网点是 DNS 查询，而那条路现在还没接进运行时。
  - 这条本轮实跑过：`main.go` 与 `internal/` 里 `http.Get`、`http.Client`、`net.Dial`、`net.Listen`、`websocket` 全部零命中；设置里的 `ConfirmUpdateDownloadOnMetered` 只是一个开关字段，没有任何代码去检查或下载更新。
  - 「运行期不监听端口」**不在已证实之列**：它靠框架常识加上面的零命中，仓库本身不体现 Wails 用自定义 scheme 还是本地回环。已列进第 6 节待实测。

## 2. 模块划分与职责边界

按「谁是唯一来源」来分，不按目录分层：

| 包 | 它独占的事实 | 越界就是错 |
| --- | --- | --- |
| `internal/catalog` | 产品目录（11 分类 / 54 项目 / 65 个去重主机名 / 11 个 scopeID） | 前端不许再自己存一份列表；运行时不许解析原型 HTML |
| `internal/hosts` | 系统 Hosts 管理区段的读法与判读 | 只读；写入、备份、还原属后面的特权服务阶段 |
| `internal/config` | 设置的读盘、校验、字段级 patch 保存 | 校验必须拿目录的 scopeID 当集合，不许放宽 |
| `internal/app` | 对外契约（DTO 形状 + 7 个稳定错误码）与 Hosts 路径/写权限探测 | 内部 cause 不进 JSON，前端不解析中文文案 |
| `internal/event` | 运行状态事件的独占序号与事件名 | 不注册成服务，前端推不动序号 |
| `internal/accelerator/strategies` | 加速方法的自注册表与候选取得实现 | 只产出候选，不放任务队列、不写 Hosts、不碰系统状态 |
| `internal/identity` | 产品身份的字面量唯一手写源 | 别处再抄一份就是债（`4475f5b` 扫了 7 个文件的教训） |
| `cmd/identity` | 把上面那组常量输出给 shell/构建配置 | 取值失败必须非 0 退出且不输出 |
| `frontend/src` | 界面装配、交互、防抖、i18n、设计令牌 | 组件不许直接调绑定；不许自己推断 Hosts 状态 |

## 3. 依赖方向（谁能碰谁）

```
main ──> app.Service ──┬──> catalog（启动 Load 一次）
  │                    ├──> config.Store（读快照 / 原子保存）
  │                    ├──> hosts.Read（每次现读，不缓存）
  │                    └──> privilege.ProbeHostsAccess（现探一次）
  └──> event.StatusEmitter（每 2 秒 Emit）

前端 main.tsx ──> App.tsx ──> api/client ──> bindings(生成) ──> Wails 桥 ──> app.Service
                     └─────> api/events  ──> @wailsio/runtime ──> app:runtime-status
```

三条不成文但真实的规则，代码里都留了抓手：

1. **错误只以码对外**。`config.ErrInvalid`、`catalog.ErrInvalid`、`ErrRevisionConflict`、`ErrSchemaTooNew`、`ErrSaveFailed`、`ErrEmptyMutationID` 在 `errors.go:47-76` 统一换成 `invalid_settings`、`catalog_invalid` 等 7 个码。
2. **静默失败一律当事故**。目录加载失败不会启动时吞掉，而是每次读接口翻译成错误码（`app.go:94-104`）；配置服务没准备好时 `settingsFailure()` 保证一定带回错误，绝不返回「空配置 + nil」（`app.go:130-135`）。
3. **不确定就不装已生效**。Hosts 只读态只能产出 `none`/`conflict`/`readFailed` 三档；`applied`、`partial` 要求拿最后一次成功写入的区段摘要比对，写入还没接，所以「区段里有本工具的映射却没可比对摘要」按冲突报（`dto.go:130-172`）。

## 4. 运行时拓扑要点

见 `runtime-topology.dot`。四件事最值得记：

- **窗口与逻辑同进程，界面渲染在 WebView 里**。前端产物是 `go:embed` 进二进制的静态资源，由 Wails 内置资源服务器提供（`main.go:66-68` 挂 `AssetFileServerFS`，`main.go:88` 窗口 `URL:"/"`）。它怎么把资源送进 WebView——自定义 scheme 还是本地回环——仓库里看不出来，别当成已知。
- **两条心跳是分开的**：后端每 2 秒推一次 `app:runtime-status`（序号单调递增，前端只接受更高的值，重复和过期直接丢弃并计数）；界面数据是前端主动 `GetItems` 现读，两条互不依赖。
- **root 只在装包时跨过去**。运行期不提权、不写 Hosts。推送链上的顺序是：`wails3 task linux:create:deb` 出包（`pre-push-reinstall.sh:80`）→ `pkill -x` 停旧进程（`:90`）→ 先过空值门再 `dpkg --purge` + `apt-get install -y ./deb`（`:70-76`；有免密助手时交给助手做同样两件事，`github-net-root-install.sh:37,39`）→ 装好之后才删三个用户目录里的配置与缓存（`:98-99`，删之前再过一次空值门）。装包用 `apt-get` 而不是 `dpkg -i`，缺依赖它会自己补。
- **免密 root 助手** `github-net-root-install.sh` 故意不 source 仓库里的脚本——仓库目录用户可写，让 root 去读它等于开一个提权入口。nfpm 的 `postinstall` 会刷两个库：`update-desktop-database` 与 `update-mime-database`（`postinstall.sh:5-18`）。

## 5. 缺口清单（图上画成断线/紫虚线的地方）

这几条不是我猜的风险，是代码里现在就是这样：

| # | 缺口 | 证据 | 现在会怎么错 |
| --- | --- | --- | --- |
| G1 | 策略注册表没进装配层：没有任何包 import `strategies`，`config.NewStore` 的 strategies 参数硬传 `nil` | `grep accelerator/strategies` 零命中；`app.go:67` | 任何「默认方法」的选择都会被拒；注册错误 `Errors()` 没人取，静默失败 |
| G2 | `main.go` 仍写死 `appName`、`appVersion`、`appID` 三个字面量，没引用 `internal/identity` | `main.go:23-26` | 改身份要扫多处，正是 identity 包想消灭的那类债 |
| G3 | `nfpm.yaml`、`build/config.yml`、Taskfile 里的包名/图标名/桌面项路径仍是抄写 | `Taskfile.yml:4`（`APP_NAME: "github-net"`）、`build/linux/Taskfile.yml:177`（`DESKTOP_ID: 'io.github.xiaokentrl.githubnet'`）、`build/linux/nfpm/nfpm.yaml:6,9,21-25` | 同上；且 DesktopID 与 AppID 的一致性只有 `internal/identity` 的测试在守 |
| G4 | 五套主题在后端设置里能存，界面只移植了 `light` | `App.tsx:24-35` | 已用「显示 light + 一句说明」兜住，不假装切换成功；但仍是待补 |
| G5 | 检测/DNS 能力没接：`GetAppStatus` 里 `DNSDetectionReady` 恒为 `false` | `app.go:117-118` | 状态栏如实报「没接」，不会显示成已检测 |
| G6 | Hosts 写入、备份、还原整段没实现；`partial`/`applied` 两档目前不会由后端产出 | `dto.go:44-64` 注释 + `privilege.go:10-21` | 只读态安全；等写入接上要重新核对判档 |
| G7 | arm64、Windows、macOS、真机（非容器）未进门禁 | `ci.yml` 全文 4 条作业（前端构建、后端检查、linux-x64 出包过门、debian:13 复核），后两条跑在 `ubuntu:26.04` / `debian:13` 容器里，没有任何实机或其它架构作业 | CI 绿不等于在这些环境能跑 |
| G8 | 出包不在 CI 里：`ci.yml` 没有任何打包步骤，deb 只有本地推送链产出 | `ci.yml` 全文；`build/linux/Taskfile.yml:153-166`（`wails3 tool package`）+ `pre-push-reinstall.sh:80` | 想发版时没有「CI 一键出包」这条路，产物只在开发者机器上生成 |
| G9 | 打包元数据仍是 Wails 模板残留：`homepage: https://wails.io`、桌面项 `Keywords=wails`、`Categories=Development;` | `nfpm.yaml:15` `build/linux/io.github.xiaokentrl.githubnet.desktop:6,8` `build/linux/Taskfile.yml` 的 `CATEGORIES: 'Development;'` | 用户在使用手册页/应用菜单里看到的归类与主页是错的；`generate:dotdesktop` 每次重新生成都会把这些字面量写回入库文件 |

## 6. 我还不知道的（需要人确认或实测）

- **108 还是 130**：`MaxCandidateProbesLimit = 108` 是代码里的硬上限，你那边还没落句（`config.go:31-33`）。
- **托盘两个开关、工具栏 A/B 方案、cleanup-actions 是否删历史、五个未引用资源**：都还等落句，图上没画成结论。
- **WebKitGTK 是否真的另起渲染进程**、**前端资源走自定义 scheme 还是本地回环**：两条都是按框架常识的推断（confidence: medium），本仓库代码不体现。要确认只能实机看进程树与监听套接字。
- **Wayland 下托盘与单例锁的实际行为**：代码接上了，但「匹配条件成立」不等于已验收，实机无人眼走查。
- **「其余 14 个仍是契约」这个 14**：直接引自 `client.ts:2` 的注释口径，本轮没有在代码里逐个数过 18 这个总数，属规划文档的说法。

## 7. 怎么重新生成这几份图

1. 改了代码就重跑这几条取证，再逐份对照更新（**别只改图**）。下面每条都在 2026-10-28 原样实跑过，右侧是当轮结果：
   ```bash
   # 注册表有没有进运行时（0 命中 = G1 仍未接线）
   grep -rn "accelerator/strategies" --include=*.go .

   # main.go 有没有引用身份常量（0 命中 = G2 仍在写死字面量）
   grep -rn "internal/identity" --include=*.go main.go

   # 出包挂在哪：3 处 wails3 tool package（deb/rpm/aur），CI 里没有
   grep -rn "nfpm" build/linux/Taskfile.yml .github/workflows/

   # 有没有出网或监听代码（零命中；范围含 build/ 时会命中 Wails 自带脚手架，故排除）
   grep -rnE "http\.Get|http\.Client|net\.Dial|net\.Listen|websocket" --include=*.go main.go internal/

   # 目录规模：categories=11 items=54 unique-hostnames=65 scopes=11
   python3 - <<'PY'
import json
d = json.load(open('internal/catalog/catalog.json'))
print('categories', len(d['categories']))
print('items', sum(len(c['items']) for c in d['categories']))
print('unique hostnames', len({h for c in d['categories'] for i in c['items'] for h in i['hostnames']}))
print('scopes', len({c['scopeID'] for c in d['categories']}))
PY

   git ls-remote --tags origin                     # 空 = 0 个标签
   curl -s https://api.github.com/repos/xiaokentrl/github-net/releases | head -3   # [] = 0 个 release
   git config --get core.hooksPath                 # .githooks = 推送链真的挂着
   ```
2. 渲染。本机已有一条不需要 root 的 graphviz：deb 解包到 `~/.local/opt/graphviz`，`~/.local/bin/dot` 是个包装脚本（设 `LD_LIBRARY_PATH` 和 `GVBINDIR`）。跑这条就够：
   ```bash
   for n in dependencies runtime-topology build-release-pipeline; do
     dot -Tsvg docs/architecture/$n.dot -o docs/architecture/$n.svg
   done
   ```
   换机器想用系统版：`sudo apt-get install graphviz`（Ubuntu 26.04 的候选版本是 14.1.2，本轮实测就是它，出图零警告）。
   `.svg` 是派生产物——改图改 `.dot`，别改 svg。
3. DSL 暂时用 Qoder 的 Structurizr 预览器看；插件不提供 DSL→Canvas 的转换管道。
4. 更新时在 `evidence.md` 里同步证据位置和日期。图与代码不一致时，以代码为准并改图。
