# 前后端目录结构规划

## 一、规划总览

### 1. 技术基线

- 后端：Go ≥ 1.27.3。
- 桌面框架：Wails ≥ 3.0.0。
- 前端：React + TypeScript + Vite。
- 全局共享前端状态：Zustand；组件内临时状态仍使用 React 自带状态。
- 前端测试：Vitest；界面交互和视觉回归测试按需使用 Playwright。
- 新增语言时，只新增一个语言文件；不得为了注册语言而改动其他代码。
- 开始实现前，用指定版本的 Wails 模板验证版本组合和目标系统构建能力。若版本或构建环境不满足要求，先说明阻塞，不私自降低版本。

### 2. 总体设计

- 前端按“应用外壳、功能页面、公共组件、基础服务”分层。
- 后端按业务边界拆包，系统差异集中放在平台适配层。
- Go DTO 是前后端接口的数据源；Wails 生成的绑定文件不手改。
- 颜色、间距、字号和控件状态统一由设计令牌和公共组件管理。
- 界面语言由自动发现的语言文件提供；语言切换、回退和校验由通用代码处理。
- 页面不直接碰操作系统；系统操作只能通过 Wails 调用后端。
- 单文件只在确有独立职责时拆分，不为目录好看制造大量空文件。

## 二、多维度分析

### 1. 人工维护与后续迭代

当前原型把页面、样式、数据和交互放在一个 HTML 文件中，不利于多人修改，也容易在改动时误伤其他功能。新结构按功能划分，文件名直接表达职责，并约定每项功能的页面、状态、接口和测试放在一起。

分类和域名等产品数据不能散落在页面组件里；统一放在后端数据定义或数据文件中，由接口向前端提供。相同规则只保留一个实现，副本原型确认迁移完成后停止维护。

**维护规则：**

- 一个文件尽量只有一个主要职责。
- 文件名使用清楚、稳定的英文名；用户可见文案放在语言资源中。
- 新功能先放进对应功能目录，不随意扩建 `common`、`utils` 等大杂烩。
- 删除、改名或移动接口前，搜索所有调用方，并更新测试和文档。
- 生成文件注明来源和生成命令；手改会被覆盖的文件。

### 2. 多语言扩展

语言目录使用“一种语言一个 JSON 文件”。例如新增日文只增加 `ja-JP.json`，应用会自动发现并展示该语言，不需要再改注册表、组件或代码。

每个语言文件包含语言代码、语言原生名称和完整文案。简体中文作为默认回退语言。自动化测试比较所有语言的键和插值参数，缺项或多项时构建失败，避免页面运行时缺字。

**多语言规则：**

- 组件只使用稳定的文案键，不拼接句子片段。
- 有变量的文案使用命名参数，所有语言保持相同参数名。
- 日期、时间、数字和单位通过 `Intl` 格式化，不在页面手工拼格式。
- 语言列表从语言文件元数据生成；回退行为只由 i18n 模块负责。
- 新增语言只新增资源文件；新增一条产品文案需要更新每种受支持语言，并由测试检查。

语言文件包含元数据和完整 `messages` 对象，例如：

```json
{
  "locale": "zh-TW",
  "nativeName": "繁體中文",
  "messages": {
    "app.title": "GitHub 加速助手"
  }
}
```

`localeDiscovery.ts` 自动扫描 `messages/*.json`，不维护额外的语言注册表。真实资源需包含全部文案键；上例只展示格式。

### 3. GUI 一致性

不同页面各自写 CSS 会造成按钮尺寸、颜色、留白、暗色主题和禁用状态不一致。所有页面使用统一设计令牌，基础控件从公共组件导入，业务页面不自行复制按钮、弹窗和状态颜色。

**一致性规则：**

- CSS 变量集中定义颜色、间距、字号、圆角、阴影和层级。
- 公共组件统一处理加载、禁用、错误、空内容和键盘焦点状态。
- 页面样式只覆盖本页面布局，不覆盖全局控件规则。
- 主题切换只切换令牌，不给每个页面单独写主题实现。
- 页面宽度、窗口缩放和滚动区域有统一规则；弹窗、下拉菜单和提示使用固定的层级表。
- 关键页面保留基准截图，组件改动时检查亮色、暗色和窄窗口显示。

### 4. 每次操作都要有即时反馈

用户每次点击、输入、切换或提交后，都必须马上看到界面反应。后端还在处理时，要持续显示当前状态，不能让用户猜测程序是否卡住。

**交互规则：**

- 操作开始后 100 ms 内显示响应：按钮进入忙碌状态、输入结果更新，或显示明确的处理中提示。
- 每个操作都有明确状态：待命、处理中、成功、失败或已取消。适用时再显示进度、当前步骤和耗时。
- 短操作不显示闪一下就消失的加载动画；仍要立即显示选中、保存状态或结果变化。
- 可计算进度时显示百分比；无法计算时显示持续活动指示和正在执行的步骤，不伪造百分比。
- 长任务显示持续可见的进度区域和取消入口；页面切换后任务仍运行时，队列和状态栏继续显示它。
- 操作成功后更新相关数据并给出结果；失败时在原操作附近显示原因和可行的重试方式。Toast 只作补充，不能是唯一错误反馈。
- 保存、切换等状态变更在等待后端确认时显示“保存中”；成功后显示已保存，失败时保留输入并提示，不静默丢弃。
- 防止同一操作重复提交，但只禁用互相冲突的控件；不让整个窗口无故失去响应。
- 任务取消、超时、后端断连和窗口关闭都要有明确结果；迟到的旧事件不能覆盖新状态。
- 搜索显示正在筛选或无匹配结果；拖动调整大小时实时更新布局并显示拖动状态。
- 需要确认或授权的操作，先说明将发生什么；等待用户时保持对话框和按钮状态清楚。
- 失败或等待超时必须结束忙碌状态，展示错误或可重试状态，不能无限转圈。
- 状态变化要支持辅助技术读取：进度使用礼貌播报，错误及时播报；同时保留可见文字，不只依赖颜色或动画。

### 5. 卡顿与并发

桌面界面卡顿常见原因是主线程做大量计算、一次渲染大量行、每个任务都推送高频事件，或窗口调整时不断触发完整渲染。前端负责轻量展示，DNS 检测和文件读写由 Go 后端异步执行。

**性能规则：**

- 所有系统操作和网络查询都异步执行；操作期间保留可操作的进度、取消和错误反馈。
- 搜索输入做短延迟合并；窗口拖动用 `requestAnimationFrame` 合并布局更新。
- 列表数据较少时用普通列表；达到约定阈值后启用虚拟列表，避免提前增加复杂度。
- 大批量任务按批处理；进度事件限频或合并，不为每个微小状态变化重绘整页。
- React 状态按功能拆分，组件只订阅所需字段；避免全局状态变化触发所有页面重渲染。
- Wails 事件监听在页面或服务销毁时取消订阅；任务取消后忽略迟到事件。
- 设置和主题更新只刷新受影响的界面，不重新加载整页。
- 先以 200 条以上的可见列表行作为虚拟列表评估点；当前 54 个项目不强制启用虚拟化。
- 进度事件默认每个任务最多每秒推送 10 次；状态切换和最终结果立即推送。
- 目标机器上，常见界面操作以 100 ms 内有反馈为目标；主线程长任务超过 50 ms 时纳入排查。
- 用目标机器和最大预期数据量做性能基准；这些数值是初始门槛，按实测调整并记录原因。

### 6. 系统兼容性与稳定性

Go 文件按系统后缀隔离平台实现，例如 `permission_windows.go`、`permission_darwin.go` 和 `permission_linux.go`。业务代码只调用稳定接口，不自行判断平台路径或执行系统命令。

Wails v3 的生命周期、绑定和事件 API 以锁定的实际版本为准；不同版本的生成目录、配置字段和打包方式可能变化，先建立最小样例并验证后再铺开。

**兼容性规则：**

- 明确最低支持的 Windows、macOS、Linux 版本和 CPU 架构，再设置 CI 构建矩阵。
- 记录各系统支持的 WebView 基线：Windows WebView2、macOS WKWebView、Linux WebKitGTK；字体、DPI、滚动条和原生窗口尺寸在目标系统逐一实测。
- 只使用已验证的 WebView/CSS 能力；不要依赖系统默认按钮、输入框、字体和滚动行为。
- 核心流程支持键盘操作和可见焦点；系统字体不可用时配置合理的跨平台回退字体。
- 平台相关代码有对应平台的编译检查和测试；系统命令通过白名单参数调用，不拼接任意输入。
- 文件路径使用标准库处理；Hosts 文件先备份、再原子替换、再验证。
- 临时文件、权限不足、DNS 服务差异和文件占用都返回明确错误。
- 后端任务使用 `context.Context` 支持取消和关闭时清理。
- Wails 绑定错误和版本升级保持兼容；破坏性 DTO 变更要同步更新前端和契约测试。
- 依赖版本锁定，升级前检查变更说明、跨平台构建和回归测试。

### 7. 测试与故障定位

目录让每个功能的测试和实现相邻，问题可以在对应模块内复现。日志只记录排查所需的信息，不记录密钥、完整 Hosts 文件或不必要的个人数据。

**验证规则：**

- 前端组件测试覆盖正常、加载、空、错误、禁用和键盘操作。
- i18n 测试检查语言文件格式、键集合、插值参数和回退。
- Go 单元测试覆盖业务状态和错误分支；Hosts 测试只操作临时文件。
- 集成测试验证 Wails DTO、事件名称和任务生命周期。
- CI 至少执行格式化、静态检查、单元测试和支持平台编译。
- 视觉回归测试覆盖主要页面、主题和常用窗口尺寸；更新基准截图必须经过人工确认。

## 三、最终目录规划

以下是目标结构。实现时按阶段新增，不能一次性创建大量空文件。

```text
.
├── AGENTS.md
├── TECHNICAL_PLAN.md
├── PUBLIC_COMPONENTS_PLAN.md
├── FRONTEND_BACKEND_STRUCTURE_PLAN.md
├── README.md
├── go.mod
├── go.sum
├── Taskfile.yml
├── wails.json
├── .gitignore
├── .editorconfig
├── .golangci.yml
├── main.go
├── assets.go
├── .github/
│   └── workflows/
│       ├── checks.yml
│       └── release.yml
├── build/
│   ├── appicon.png
│   ├── windows/
│   │   └── installer.nsi
│   ├── darwin/
│   │   └── Info.plist
│   └── linux/
│       └── app.desktop
├── frontend/
│   ├── package.json
│   ├── package-lock.json
│   ├── tsconfig.json
│   ├── tsconfig.node.json
│   ├── vite.config.ts
│   ├── vitest.config.ts
│   ├── playwright.config.ts
│   ├── eslint.config.js
│   ├── .prettierrc.json
│   ├── index.html
│   ├── public/
│   │   └── favicon.png
│   └── src/
│       ├── main.tsx
│       ├── app/
│       │   ├── App.tsx
│       │   ├── AppProviders.tsx
│       │   ├── routes.tsx
│       │   └── ErrorBoundary.tsx
│       ├── api/
│       │   ├── client.ts
│       │   ├── errors.ts
│       │   ├── events.ts
│       │   └── generated/
│       │       └── README.md
│       ├── components/
│       │   ├── layout/
│       │   │   ├── AppShell.tsx
│       │   │   ├── AppShell.module.css
│       │   │   ├── Toolbar.tsx
│       │   │   ├── StatusBar.tsx
│       │   │   └── SplitPane.tsx
│       │   ├── navigation/
│       │   │   ├── CategoryTree.tsx
│       │   │   ├── CategoryTree.module.css
│       │   │   ├── CategoryNode.tsx
│       │   │   └── SearchBox.tsx
│       │   └── ui/
│       │       ├── Button.tsx
│       │       ├── Button.module.css
│       │       ├── Checkbox.tsx
│       │       ├── ConfirmDialog.tsx
│       │       ├── EmptyState.tsx
│       │       ├── ErrorMessage.tsx
│       │       ├── Icon.tsx
│       │       ├── LanguageSelect.tsx
│       │       ├── Modal.tsx
│       │       ├── InlineStatus.tsx
│       │       ├── OperationFeedback.tsx
│       │       ├── ProgressBar.tsx
│       │       ├── Select.tsx
│       │       ├── StatusBadge.tsx
│       │       ├── SettingsSection.tsx
│       │       ├── StrategySelector.tsx
│       │       ├── Table.tsx
│       │       ├── Toast.tsx
│       │       └── Tooltip.tsx
│       ├── features/
│       │   ├── strategies/
│       │   │   ├── StrategyResults.tsx
│       │   │   ├── StrategyResults.module.css
│       │   │   ├── strategyStore.ts
│       │   │   ├── strategyTypes.ts
│       │   │   └── strategySelectors.ts
│       │   ├── items/
│       │   │   ├── ItemDetail.tsx
│       │   │   ├── ItemDetail.module.css
│       │   │   ├── CandidateTable.tsx
│       │   │   ├── itemStore.ts
│       │   │   ├── itemTypes.ts
│       │   │   └── itemSelectors.ts
│       │   ├── tasks/
│       │   │   ├── TaskPanel.tsx
│       │   │   ├── TaskPanel.module.css
│       │   │   ├── TaskTable.tsx
│       │   │   ├── TaskActions.tsx
│       │   │   ├── taskStore.ts
│       │   │   ├── taskTypes.ts
│       │   │   └── taskEvents.ts
│       │   ├── settings/
│       │   │   ├── SettingsDialog.tsx
│       │   │   ├── SettingsDialog.module.css
│       │   │   ├── SettingsNavigation.tsx
│       │   │   ├── AppearanceSettings.tsx
│       │   │   ├── LayoutSettings.tsx
│       │   │   └── StartupSettings.tsx
│       │   ├── theme/
│       │   │   ├── ThemePicker.tsx
│       │   │   ├── themeStore.ts
│       │   │   └── themes.ts
│       │   └── updater/
│       │       ├── UpdatePanel.tsx
│       │       └── updateStore.ts
│       ├── i18n/
│       │   ├── index.ts
│       │   ├── localeDiscovery.ts
│       │   ├── localeSchema.ts
│       │   ├── formatters.ts
│       │   └── messages/
│       │       ├── zh-CN.json
│       │       └── zh-TW.json
│       ├── hooks/
│       │   ├── useAppStatus.ts
│       │   ├── useTaskEvents.ts
│       │   └── useDebouncedValue.ts
│       ├── stores/
│       │   ├── appStore.ts
│       │   ├── operationStore.ts
│       │   └── settingsStore.ts
│       ├── styles/
│       │   ├── tokens.css
│       │   ├── themes.css
│       │   ├── reset.css
│       │   ├── global.css
│       │   ├── layers.css
│       │   └── accessibility.css
│       ├── utils/
│       │   ├── format.ts
│       │   ├── validation.ts
│       │   └── safeText.ts
│       ├── test/
│       │   ├── setup.ts
│       │   └── localeCompleteness.test.ts
│       └── e2e/
│           ├── app-start.spec.ts
│           ├── settings.spec.ts
│           └── visual-regression.spec.ts
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   ├── bindings.go
│   │   ├── dto.go
│   │   ├── errors.go
│   │   └── bindings_test.go
│   ├── accelerator/
│   │   ├── model.go
│   │   ├── strategy.go
│   │   ├── registry.go
│   │   ├── service.go
│   │   ├── service_test.go
│   │   └── strategies/
│   │       ├── hosts_dns_lookup.go
│   │       └── hosts_dns_lookup_test.go
│   ├── catalog/
│   │   ├── catalog.go
│   │   ├── catalog.json
│   │   └── catalog_test.go
│   ├── config/
│   │   ├── config.go
│   │   ├── defaults.go
│   │   ├── migrate.go
│   │   ├── store.go
│   │   └── config_test.go
│   ├── detector/
│   │   ├── detector.go
│   │   ├── resolver.go
│   │   ├── probe.go
│   │   ├── ranking.go
│   │   └── detector_test.go
│   ├── hosts/
│   │   ├── manager.go
│   │   ├── parser.go
│   │   ├── renderer.go
│   │   ├── backup.go
│   │   ├── transaction.go
│   │   └── hosts_test.go
│   ├── platform/
│   │   ├── platform.go
│   │   ├── permission_windows.go
│   │   ├── permission_darwin.go
│   │   ├── permission_linux.go
│   │   ├── hosts_windows.go
│   │   ├── hosts_darwin.go
│   │   ├── hosts_linux.go
│   │   ├── dns_windows.go
│   │   ├── dns_darwin.go
│   │   ├── dns_linux.go
│   │   ├── open_windows.go
│   │   ├── open_darwin.go
│   │   ├── open_linux.go
│   │   └── platform_test.go
│   ├── task/
│   │   ├── manager.go
│   │   ├── model.go
│   │   ├── events.go
│   │   ├── transition.go
│   │   └── task_test.go
│   ├── updater/
│   │   ├── client.go
│   │   ├── manifest.go
│   │   ├── verify.go
│   │   └── updater_test.go
│   ├── event/
│   │   ├── publisher.go
│   │   └── event.go
│   └── logging/
│       ├── logging.go
│       └── redact.go
├── test/
│   ├── integration/
│   │   ├── app_test.go
│   │   ├── hosts_flow_test.go
│   │   └── task_flow_test.go
│   └── fixtures/
│       └── hosts_sample.txt
└── docs/
    ├── architecture.md
    ├── frontend-guide.md
    ├── backend-guide.md
    ├── localization.md
    └── testing.md
```

`main.go` 和 `assets.go` 放在项目根目录，便于 Go 从 `frontend/dist` 嵌入前端构建产物。`assets.go` 只负责嵌入并提供静态资源；Wails 启动和服务绑定由 `main.go` 负责。实际资源配置以锁定版本的 Wails 模板为准。

## 四、目录和文件职责

### 1. 项目入口和资源

- `main.go`：创建 Wails 应用、绑定后端服务并处理应用生命周期。
- `assets.go`：通过 `//go:embed` 提供 `frontend/dist` 静态文件；构建前先生成前端产物。

### 2. 前端

- `app/`：启动应用、装配全局服务、路由和未捕获错误兜底。
- `components/layout/`：窗口整体布局、工具栏、状态栏和分栏尺寸。
- `components/navigation/`：分类树、节点和搜索框。分类树只接收数据和回调，不直接调用后端。
- `components/ui/`：通用控件，只负责呈现与基础交互；不包含 Hosts 或 DNS 业务规则。
- `StrategySelector.tsx`：显示并选择当前主加速方式；不负责检测或修改系统设置。
- `OperationFeedback.tsx`：统一显示操作阶段、进度、耗时、取消和最终结果。
- `InlineStatus.tsx`：在表单或按钮附近显示保存中、成功、错误和重试提示。
- `*.module.css`：页面和组件的局部样式；颜色、字号、间距仍引用统一令牌，不重复定义视觉规范。
- `features/`：按业务功能组织页面、状态、类型和事件处理。
- `features/strategies/`：组合展示各加速方式的检测结果、能力和错误，并允许用户选定主方式；只通过 API 调用后端。
- `features/settings/`：设置页面本身；共享设置状态统一放在 `stores/settingsStore.ts`，避免重复维护。
- `api/client.ts`：前端唯一的后端调用入口；业务组件不直接使用 Wails 运行时 API。
- `api/generated/`：Wails 生成的绑定文件。目录内的 README 说明生成方式；实际文件以锁定版本模板为准，禁止手工编辑。
- `i18n/messages/*.json`：每种语言一个完整资源文件。语言发现、回退和校验由 `localeDiscovery.ts`、`index.ts` 与测试负责。
- `styles/tokens.css`：唯一的颜色、间距、字号、圆角、阴影和层级源头。
- `styles/themes.css`：主题令牌映射；不能由功能页面另写一套主题颜色。
- `styles/layers.css`：声明 reset、token、公共组件和功能样式的 CSS 层级，避免样式加载顺序导致覆盖结果不同。
- `stores/`：跨功能共享状态。页面局部状态留在组件内，避免所有状态都塞进全局 store。
- `stores/operationStore.ts`：只管理需要跨页面持续显示的后台任务状态；短暂的表单状态留在功能页面。
- `test/`：跨功能的前端设置和资源完整性测试；具体组件测试与组件文件相邻，采用 `*.test.tsx` 命名。

### 3. 后端

- 根目录 `main.go`：创建 Wails 应用、装配服务、配置关闭清理。
- `internal/app/`：Wails 暴露给前端的唯一服务门面、DTO 和错误转换；不放系统操作细节。
- 应用每次启动都创建空的内存任务队列；不读取或创建队列历史文件。
- `internal/accelerator/`：加速方式的统一模型、能力接口、显式注册表、检测协调、主方式选择和应用路由。
- `internal/accelerator/strategies/hosts_dns_lookup.go`：首期策略；根据可信 DNS 解析器获取候选 IP，保留目标域名进行 TLS 证书校验，并在用户确认后调用共享 Hosts 服务应用。
- `internal/accelerator/strategies/hosts_dns_lookup_test.go`：使用模拟解析器、模拟网络和临时 Hosts 文件测试该策略，不访问真实 Hosts。
- `internal/catalog/`：分类、域名等产品数据的唯一来源；前端不得再维护另一份列表。
- `internal/config/`：默认配置、校验、版本迁移和原子保存；保存当前主策略 ID，不保存易过期的检测结果作为可直接应用的数据。
- `config.Config` 中的 `primaryStrategyID` 保存当前默认方式；配置迁移验证该 ID 是否仍在注册表中，未知或已移除的 ID 必须回退到安全默认值并提示用户。
- `internal/detector/`：DNS 查询、候选探测和评分；每个请求可取消并有超时。
- `internal/hosts/`：Hosts 文件解析、备份、写入事务、验证和恢复；不负责选择系统路径或弹系统授权框。
- `internal/platform/`：系统权限、路径、DNS 刷新和打开文件的接口及系统实现。文件后缀使用 Go 识别的 `windows`、`darwin`、`linux`。
- `internal/task/`：任务生命周期、状态转换、取消和进度；通过事件发布器报告变化。
- `internal/event/`：统一事件名称、数据结构和限频发布。
- `internal/updater/`：检查版本、验证清单和签名；不得绕过校验直接执行下载内容。
- `internal/logging/`：统一日志格式和敏感信息清理。
- `test/integration/`：跨包流程测试，只使用临时目录和模拟平台实现。

### 加速方式如何扩展

- 每一种正式加速方式各有一个实现文件，统一放在 `internal/accelerator/strategies/`；测试文件与实现文件相邻。新增方式时只新增该方式的实现和测试，再在 `registry.go` 显式注册。
- 注册表固定列出已支持的方式，不扫描目录、不使用 Go 插件、不靠 `init()` 隐式注册。这样构建结果、能力列表和测试都可预测。
- `strategy.go` 定义稳定 ID、展示信息、能力和检测接口；应用、还原等操作通过明确的能力接口提供。没有能力的策略不显示对应按钮。
- 每种策略只放自身差异逻辑；DNS、连通性探测、任务进度、配置、日志、Hosts 原子写入和回滚继续复用现有公共模块。
- 第一种策略 `hosts_dns_lookup.go` 对应链接中的思路：查询域名 DNS 地址，再由用户选择候选并写入 Hosts。直接使用解析器接口，不抓取 ipaddress.com 网页，不依赖页面结构或手工粘贴页面内容。检测目标 IP 时保留原主机名并校验 TLS 证书，不关闭证书验证。
- 未来可按需新增 `system_proxy.go`、`pac.go` 等单文件策略；实现并通过平台测试前，不建立空文件、不注册、不宣称可用。
- Hosts 记录带应用管理标记和策略 ID。不同方式不能覆盖用户自行维护的行；切换主方式只改变默认选择，不自动修改或清理旧方式留下的系统设置。

### 检测后选择主方式

1. 检测服务并行检测已注册且适用于目标平台的策略；结果按策略分组，分别显示耗时、候选、验证状态和失败原因。
2. 一种策略超时或失败不影响其他策略的结果；所有检测操作都可取消并反馈进度。
3. 用户可将一种已检测的方式设为全局主方式；设置只存策略 ID，升级后若 ID 不再可用，明确提示并回退到安全默认值。
4. 设为主方式不自动写 Hosts、不启用系统代理，也不应用旧检测结果。应用前检查候选未过期并再次验证。
5. 主方式只决定新操作默认采用哪种方式；已有 Hosts 记录或系统配置的恢复必须由用户发起并显示清楚影响范围。
6. 不把不同方式的分数硬凑成一个“最佳”值；显示各自的连通性、耗时、覆盖范围和能力，让用户按检测结果选择。

## 五、扩展与兼容规则

### 新增一种语言

1. 在 `frontend/src/i18n/messages/` 新增一个完整语言 JSON 文件，例如 `ja-JP.json`。
2. 文件元数据写明 locale 和原生语言名称。
3. 语言发现器自动加载并生成语言选项；回退到 `zh-CN`。
4. 完整性测试检查所有键和插值变量。
5. 不改 `index.ts`、页面组件或语言注册表。

### 新增一种操作系统

1. 在 `internal/platform/` 为新系统添加对应文件后缀实现。
2. 实现平台接口并补充该系统的路径、权限和 DNS 测试。
3. 把目标系统加入 CI 构建矩阵和发布流程。
4. 业务包和前端无需增加系统分支。

### 新增一个业务功能

1. 在 `frontend/src/features/<feature>/` 放页面、store、类型和测试。
2. 在 `internal/<feature>/` 放业务逻辑和测试。
3. 只通过 `internal/app/bindings.go` 暴露受控 API。
4. 更新 DTO、接口测试、用户文案和相关文档。
5. 不直接从页面调用平台适配包。

### 新增加速方式

1. 在 `internal/accelerator/strategies/` 新增一个策略实现文件及相邻测试。
2. 复用 `detector`、`hosts`、`platform`、`task` 等模块；不得复制它们的通用实现。
3. 在 `registry.go` 显式登记唯一策略 ID 和能力，并为绑定接口、设置迁移和界面补齐测试。
4. 通过策略注册表逐个检测和选择；一种策略的失败不得中断其他策略。
5. 只有该方式的检测、应用、还原、跨平台行为和用户反馈全部通过后，才作为可用方式显示。

### 清空队列与启动行为

1. 队列默认只保存在内存中；应用每次启动都从空队列开始，不生成或读取历史队列文件。
2. “清空队列”移除所有队列记录，不只清除已完成项；正在执行检测、应用或还原任务时禁止清理。
3. 清空队列不修改 Hosts、候选检测结果、用户设置或恢复备份。
4. 若未来需求要求持久化队列，必须先更新规划；启动时删除上次运行的任务记录，清空操作同时清理存储并明确反馈失败。

### 配置和运行文件

- 应用数据根目录按系统规范确定：Windows 使用 `%LOCALAPPDATA%\GitHubNet`，macOS 使用 `~/Library/Application Support/GitHubNet`，Linux 使用 `${XDG_DATA_HOME:-~/.local/share}/github-net`。
- 根目录只包含 `config/config.json`、`backups/hosts/`、`logs/` 和 `tmp/`；配置、日志、Hosts 备份和临时文件不得写入仓库、当前工作目录或用户主目录顶层。
- 不创建数据库、队列文件或转储。配置原子保存时的临时文件只在 `tmp/` 短暂存在，保存成功后立即删除；最多保留一个配置回滚备份。
- `backups/hosts/` 最多保留一个当前有效恢复点；成功还原且验证通过后删除。不得清理仍被有效 Hosts 修改引用的恢复点。
- `logs/` 最多保留当前日志及 4 个轮转文件，每个文件不超过 5 MiB；按大小轮转，启动时清除本应用过期临时文件。
- 临时文件使用应用专属目录和固定命名规则；每次操作结束时删除，启动时只清理本应用遗留文件。
- 保存失败、磁盘空间不足或清理失败必须显示错误；不留下无用途的垃圾文件。

## 六、从原型迁移的顺序

1. 锁定 Go ≥ 1.27.3 与 Wails ≥ 3.0.0 的实际构建模板和最低支持平台。
2. 建立前端、Go 后端和 CI 骨架，先运行空白应用。
3. 搬入设计令牌和公共 UI 控件，确保主题与基础布局统一。
4. 按 `catalog`、详情、设置和队列的顺序迁移原型页面。
5. 先接入只读后端状态，再接检测任务，最后接 Hosts 修改和系统功能。
6. 建立跨语言完整性、平台编译、Hosts 临时文件和任务生命周期测试。
7. 新结构通过验收后，指定唯一原型来源并停止维护重复副本。

## 七、验收清单

- [ ] 新增一种语言时只需新增一个语言资源文件。
- [ ] 语言缺少文案或插值参数不一致时，测试和构建会失败。
- [ ] 所有页面共用一套主题令牌和基础控件。
- [ ] 加载、空数据、失败、禁用和键盘状态在相关组件中表现一致。
- [ ] 每个可操作控件都有可见的开始反馈和成功、失败或取消结果；慢操作有持续进度或步骤提示。
- [ ] 超时和断连会结束忙碌状态并显示错误或重试入口；没有只靠 Toast 提示的关键失败。
- [ ] 自动化测试覆盖慢请求、失败、取消、重复点击和迟到事件，不会出现无限加载或旧状态覆盖。
- [ ] 清空队列移除全部记录；有任务运行时拒绝清理；重启后队列为空。
- [ ] 队列不产生持久化历史文件；配置、日志、备份和临时文件只写入有界的应用专属目录。
- [ ] 后端耗时操作不会阻塞界面；进度推送有限频，取消后无迟到状态覆盖。
- [ ] 目标平台均可编译，系统能力有平台实现或明确的“不支持”错误。
- [ ] Hosts 测试只使用临时文件，不改开发机真实 Hosts。
- [ ] Wails 生成绑定不手改；重新生成后检查接口差异。
- [ ] 关键窗口尺寸、主题和页面通过视觉回归检查。
- [ ] Windows、macOS、Linux 的 WebView 字体、DPI、滚动、快捷键和弹窗层级通过实机检查。
- [ ] 在最大预期数据量下记录交互延迟和主线程长任务；超出门槛时有复现步骤和修复记录。
- [ ] Go、Wails、前端运行时和依赖版本固定并经过 CI 验证。
