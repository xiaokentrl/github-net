// github-net 整体架构 · C4 源文件（Structurizr DSL）
// 生成日期：2026-10-28　依据：当前 main 工作树代码（远端 4475f5b + 未提交的阶段三起手）
// 打开方式：Qoder 的 Structurizr DSL 预览器。本文件是事实源，渲染出的图是派生产物。
// 每条描述带证据位置（file:line）；confidence 写在末尾，别把 low 当已验收结论。

workspace "github-net" "GitHub / Steam 网络加速桌面工具的整体架构模型（现状态，含未接线缺口）" {

  model {

    // ── L1 外部参与者与外部系统 ────────────────────────────────────────
    operator = person "使用者" "在自己桌面上打开窗口、拖动侧栏、改网络档位的人"

    hostsFile = softwareSystem "系统 Hosts 文件" "Linux 是 /etc/hosts，Windows 是 System32 下的 drivers/etc/hosts。当前只读，不写一个字节。证据 internal/app/privilege.go:24-36 internal/hosts/manager.go:25 confidence:high"

    systemDNS = softwareSystem "操作系统配置的 DNS" "策略 dns-hosts 经 net.DefaultResolver 查候选 IP，绝不转发第三方解析器。代码在 confidence:high；尚未接进运行时 confidence:low"

    configStore = softwareSystem "用户配置目录里的 config.json" "Linux 用 XDG_DATA_HOME 或 .local/share/github-net/config/config.json；Windows 用 LOCALAPPDATA 下 GitHubNet；macOS 用 Application Support 下 GitHubNet。证据 internal/config/config.go:147-171 confidence:high"

    desktopEnv = softwareSystem "桌面环境（GTK4、WebKitGTK 6.0、系统托盘、单例锁）" "由 Wails v3 经 CGO 使用；deb 依赖 libgtk-4-1 与 libwebkitgtk-6.0-4。证据 build/linux/nfpm/nfpm.yaml confidence:high"

    // ── 被建模的系统 ──────────────────────────────────────────────────
    githubNet = softwareSystem "GitHub Net（github-net）" "单机桌面应用：产品目录 + 只读 Hosts 状态 + 用户设置 + 运行状态事件。当前没有服务端、数据库、更新服务，也没有任何第三方网络调用。confidence:high" {

      // ── L2 容器 1：交付的 Go 二进制进程 ────────────────────────────
      goProcess = container "Go 主进程（唯一交付二进制）" "go build 出一个 CGO 二进制；前端产物、目录数据、托盘图标都 go:embed 在里面。进程内同时承载窗口、托盘、Wails 服务与事件发射。证据 main.go:16-20 30-74 109-131 confidence:high" {

        bootstrap = component "装配入口 main" "建 Wails 应用、注册服务、建窗口与托盘、单例锁二次启动聚焦、2 秒一次的状态事件循环、退出时取消订阅并销毁托盘。证据 main.go:29-160 confidence:high" "Go"
        service = component "对外门面 internal/app.Service" "暴露给前端的唯一门面，只放前端需要的读操作：RuntimeInfo、Ping、GetItems、GetAppStatus、GetSettings、SaveSettings。证据 internal/app/app.go:15-161 frontend/bindings/github.com/xiaokentrl/github-net/internal/app/service.ts:26-65 confidence:high" "Go"
        publicError = component "错误码翻译 internal/app/errors.go" "把内部哨兵错误整理成 7 个稳定错误码；cause 不进 JSON，前端不解析中文文案。证据 internal/app/errors.go:12-76 confidence:high" "Go"
        privilege = component "Hosts 路径与写权限探测 internal/app/privilege.go" "只探测能不能以写方式打开，不截断不写入；权限不足与其它错误分开报。证据 internal/app/privilege.go:14-53 confidence:high" "Go"
        catalogPkg = component "产品目录 internal/catalog" "go:embed catalog.json 再加结构校验：11 个分类、54 个项目、65 个去重主机名、11 个 scopeID。数据一次性从唯一正式原型的 CATEGORY_DEFS（1131-1208 行）抽取，运行时不解析 HTML。证据 internal/catalog/catalog.go:14-188 internal/catalog/catalog.json:1-12 confidence:high" "Go + JSON"
        configPkg = component "配置读写 internal/config.Store" "全部写入在锁内串行。缺文件给默认值、版本更高就拒读拒改、内容坏了先隔离成 .bad 再用默认值运行；保存按字段 patch，配 revision 乐观锁、mutationID 幂等、同目录临时文件原子替换。证据 internal/config/config.go:272-490 confidence:high" "Go"
        hostsPkg = component "Hosts 只读解析 internal/hosts" "在本工具管理区段（BEGIN/END 标记 v1）里读映射；结构毛病走 Anomaly，不报错也不猜修。证据 internal/hosts/parser.go:89-170 internal/hosts/manager.go:25-38 confidence:high" "Go"
        eventPkg = component "运行状态事件 internal/event" "独占单调递增序号，只发 app:runtime-status 一个事件；不注册成服务，所以前端推不动序号。证据 internal/event/event.go:9-36 confidence:high" "Go"
        identityPkg = component "产品身份 internal/identity" "三层定名的唯一手写源：可换的脸（DisplayName、Description）、默认冻结的户口（AppID、Slug、DataDir、HostsMarkerID）、不动的骨头（模块路径）。16 个键供 cmd/identity 输出。证据 internal/identity/identity.go:15-111 confidence:high（代码）；main.go 与构建配置仍未引用它" "Go"
        strategyPkg = component "加速方法自注册表 internal/accelerator/strategies" "contract + registry + 首个方法 dns-hosts（系统解析器、2 秒预算、按档位裁剪候选）。靠 init 自注册，注册错误攒进 Errors 等装配层启动时取。证据 strategies/contract.go strategies/registry.go:13-59 strategies/hosts_dns_lookup.go:38-56 confidence:high（代码）；2026-10-29 已接进装配层（internal/app/app.go 固定导入并用 strategies.IDs() 作校验集合），但没有执行路径真的调用 Lookup，检测那一屏还没做" "Go"
      }

      // ── L2 容器 2：WebView 里的前端 ────────────────────────────────
      spa = container "前端 SPA（React 18 + Vite，跑在 WebView 渲染进程里）" "构建产物 frontend/dist 被 go:embed 进二进制，由 Wails 内置资源服务器提供。进程分离是按 WebKitGTK 多进程模型推断的。证据 main.go:16-17 frontend/package.json confidence:high（代码）/ medium（独立渲染进程这一说法）" {
        appShell = component "界面装配 App.tsx 与 AppShell、Toolbar、CategoryTree、ItemDetail、StatusBar、SettingsDialog" "三条独立加载链路（状态、设置、目录），各自只认最后一次请求的结果；订阅状态事件并按 sequence 丢弃重复与过期。证据 frontend/src/app/App.tsx:38-210 confidence:high" "TypeScript"
        apiLayer = component "调用层 src/api（client、events、errors）" "只登记后端已经接好的 4 个命令，其余 14 个仍是契约、不写空壳；组件不直接调绑定。证据 frontend/src/api/client.ts:1-52 frontend/src/api/events.ts:4-24 confidence:high" "TypeScript"
        bindings = component "生成绑定 frontend/bindings（勿手改）" "由 Wails 从 Go 服务生成 service.ts 与 models.ts，是前后端形状的唯一对齐点。confidence:high" "TypeScript 生成物"
        themeTokens = component "设计令牌与文案 tokens.css + i18n" "当前只移植晨光白一套配色；另外五套在后端设置里存着，界面按 light 显示并如实说明原因。证据 frontend/src/app/App.tsx:24-35 frontend/src/i18n/messages/zh-CN.json confidence:high" "CSS + JSON"
      }

      // ── L2 容器 3：只在构建期运行 ─────────────────────────────────
      identityCLI = container "构建期取值命令 cmd/identity" "把 internal/identity 的值按 --shell、--key、--list 输出给 shell 脚本；取不到值就退出码非 0 且不输出内容。证据 cmd/identity/main.go:1-58 confidence:high（代码）；Taskfile 与 nfpm 还没接" "Go"
    }

    ciPlatform = softwareSystem "GitHub Actions（ci.yml）" "前端构建 → 后端检查（ubuntu:26.04 容器）→ Linux x64 出包 + 门一 glibc 不高于 2.41 + 门二 ldd 复核 → debian:13 复跑门二。另有 cleanup-actions.yml 清运行记录。证据 .github/workflows/ci.yml confidence:high"

    localReinstall = softwareSystem "本机免密 root 重装链路" "顺序（本轮按行号实读）：wails3 task linux:create:deb 出包（pre-push-reinstall.sh:80）→ pkill -x 停旧进程（:90）→ dpkg --purge + apt-get install -y ./deb（:70-76，有助手时交给助手，助手脚本 :37,39）→ 装好之后先过空值门再删三个用户目录（:98-99）。CI 里没有打包作业，deb 只在这条链上产生。证据 .githooks/pre-push:1-7 scripts/pre-push-reinstall.sh scripts/lib/identity-guard.sh:21,58 confidence:high"

    // ── 关系 ──────────────────────────────────────────────────────────
    operator -> goProcess "打开窗口、点托盘、改设置" "桌面交互"
    goProcess -> hostsFile "只读：打开、读完、关闭，找本工具管理区段" "文件读"
    goProcess -> configStore "读快照；保存时原子替换写回" "JSON 文件"
    goProcess -> desktopEnv "建窗口、托盘、单例锁；CGO 调 GTK4 与 WebKitGTK" "本地库调用"
    goProcess -> spa "内置资源服务器提供 dist 静态资源" "自定义 scheme HTTP"
    spa -> goProcess "调用 6 个绑定方法（Promise）" "Wails JS 到 Go 的桥，同步"
    goProcess -> spa "每 2 秒发 app:runtime-status，sequence 单调递增" "Wails 事件，异步"
    bootstrap -> service "application.NewService(service)" "进程内"
    bootstrap -> eventPkg "Next(visible) 取独占序号" "进程内"
    service -> catalogPkg "启动时 Load 一次；失败就把错误留下，每次读目录翻译成稳定错误码" "进程内"
    service -> configPkg "NewStore(path, 目录的 scopeIDs, nil)：方法集合传 nil，任何默认方法选择都会被拒" "进程内"
    service -> hostsPkg "GetItems 每次调用现读一遍、不缓存；只读态能产出 none、conflict、readFailed 三档" "进程内"
    service -> privilege "GetAppStatus 里现探一次写权限" "进程内"
    service -> publicError "内部错误换成对外错误码" "进程内"
    configPkg -> publicError "ErrInvalid、ErrRevisionConflict、ErrSchemaTooNew、ErrSaveFailed 被翻译" "进程内"
    catalogPkg -> publicError "ErrInvalid 换成 catalog_invalid" "进程内"
    appShell -> apiLayer "加载、防抖查询、共用一条保存链路（revision 只数一遍）" "进程内 TS"
    apiLayer -> bindings "只 import 生成绑定，不自己拼协议" "进程内 TS"
    appShell -> themeTokens "设 locale、按已移植主题集设 data-theme" "进程内 TS"
    strategyPkg -> catalogPkg "init 时取 scopeIDs 当适用范围，取不到就不注册" "进程内，尚未进运行时"
    strategyPkg -> hostsPkg "NormalizeHostname 统一比较形状" "进程内，尚未进运行时"
    strategyPkg -> systemDNS "查候选 IP，只走系统解析器" "DNS 查询，尚未接进界面与服务"
    identityPkg -> identityCLI "常量换成键值输出" "进程内"
    localReinstall -> identityCLI "eval 一次取身份，任何删除或卸载动作之前先过空值门" "shell"
    identityPkg -> localReinstall "包名、二进制、桌面项、产物路径的字面量唯一来源；nfpm.yaml 目前仍是抄写、未接线" "构建期"
    githubNet -> ciPlatform "推 main 或提 PR 触发" "Git 事件"
    ciPlatform -> goProcess "在 ubuntu:26.04 容器里 CGO_ENABLED=1 go build，产物过门一与门二" "构建"
  }

  views {

    systemContext githubNet "system-context" "L1：系统与外部边界（当前只碰本机文件与系统 DNS）" {
      include *
      autoLayout
    }

    container githubNet "containers" "L2：交付二进制、WebView 里的前端、构建期取值命令" {
      include *
      autoLayout
    }

    component goProcess "components-go" "L3：Go 侧组件与依赖方向" {
      include *
      autoLayout
    }

    component spa "components-frontend" "L3：前端分层，界面装配到调用层到生成绑定" {
      include *
      autoLayout
    }

    theme {
      background "#f7f8fa"
      fontSize "13px"
    }
  }
}
