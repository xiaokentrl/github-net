# 公共组件规划

## 1. 目标

把多个页面会重复使用的界面和后端能力做成公共组件。组件只负责一类事情，接口清楚，方便复用和测试。

## 2. 设计原则

- 先识别重复需求，不为“可能会复用”提前造组件。
- 公共组件不直接读取或修改页面的全局状态。
- 输入、输出、失败情况要明确；错误不能静默吞掉。
- 组件不绕过 Go 后端直接执行系统操作。
- 先复用现有页面样式和交互，再逐步统一细节。
- 控制单文件大小，组件按功能拆分；不复制第二份实现。

## 3. 前端公共组件

建议放在 `frontend/src/components/`；公共样式和工具分别放在 `frontend/src/styles/`、`frontend/src/utils/`。

| 组件 | 职责 | 主要页面 |
|---|---|---|
| `Button` | 统一普通、主要、危险、禁用和加载按钮 | 工具栏、详情、队列、设置 |
| `Modal` | 显示确认内容，返回确认或取消结果 | Hosts 操作、恢复、取消任务 |
| `Toast` | 短暂显示成功、提示和错误消息 | 全局 |
| `InlineStatus` | 在操作附近显示处理中、成功、错误和重试提示 | 设置、表单、详情操作 |
| `OperationFeedback` | 持续显示任务步骤、进度、耗时和取消入口 | 检测、应用、恢复、更新 |
| `ServiceStrategySelector` | 用“加速方法”等直白文字展示当前服务可用方式，并通过后端设置接口保存该服务以后默认使用的方法 | 当前服务详情；属于策略功能，不是通用 UI 控件 |
| `StatusBadge` | 显示状态文字和颜色 | 分类树、详情、队列 |
| `ProgressBar` | 展示可确定的任务进度 | 工具栏、更新页面 |
| `EmptyState` | 展示没有项目、搜索无结果或没有候选数据 | 分类树、详情、队列 |
| `SettingsSection` | 统一设置页标题、说明和内容区域 | 设置中心 |
| `NetworkProfileControl` | 显示省流/标准/完整检测档位、每档实际候选/并发上限和保存状态；可用于设置页和检测区快速入口 | 网络与流量设置、检测工具栏 |
| `ThemePicker` | 平铺显示主题卡片，支持滚动和当前主题状态 | 设置中心 |
| `LanguageSelect` | 提供受支持语言选项；繁体中文未通过完整性测试和人工审校前明确显示“翻译未完成” | 设置中心 |
| `Table` | 统一表头、空状态和键盘可访问性；具体列由业务页面提供 | 详情、队列 |

组件只接收所需数据和事件回调。例如状态徽章接收状态值，负责显示；它不自行判断检测或应用规则。

每次用户操作都要立即显示开始反馈，结束时显示成功、失败或取消结果。长任务持续显示步骤和进度；错误在操作附近保留，不能只用很快消失的 Toast。

短操作用即时状态变化回应，不显示闪烁的加载动画。进度和错误要有屏幕阅读器可读的状态说明，不能只靠颜色、图标或动画传达。

## 4. 前端公共工具

- **格式化工具**：格式化连接耗时、时间和流量估算值；使用 React 文本节点显示内容。
- **安全工具**：只校验需要打开的动态外链协议和可信域名；React 默认转义已足够处理普通文字，不自行拼 HTML。
- **表单校验工具**：检查设置项格式和范围并返回字段级错误；后端仍负责最终校验。
- **Wails API 封装**：集中调用后端方法和监听事件；统一处理错误和取消订阅。
- **产品目录**：`catalog.json` 由唯一正式原型抽取；前端目录、详情和计数只能使用后端返回的数据，不维护手写副本。
- **设置存储**：Go 配置服务是所有持久设置的唯一读写方；网络/加速/启动设置与主题、语言、布局等界面偏好统一保存到版本化 JSON。前端 `settingsStore` 只缓存后端快照、编辑草稿和保存状态，不用 `localStorage` 保存第二份。搜索、展开项、临时选择等短期 UI 状态留在前端内存。
- **操作状态管理**：任务功能 store 是队列状态唯一来源，统一追踪跨页面仍在运行的任务，避免切换页面后失去进度和结果。
- **多语言工具**：按稳定文案键读取语言文件、替换命名参数并统一回退；日期和数字由 `Intl` 格式化。
- **样式令牌**：统一颜色、间距、字号、圆角、层级和焦点样式，避免页面自行复制控件规则。

随机生成的演示数据和生产数据必须分开，正式接口不可调用模拟工具。

### 4.1 多维度分析

| 维度 | 分析结论 |
|---|---|
| 人工维护 | 公共方法按职责放进 `api`、`i18n`、`utils`、`hooks` 或具体功能目录；不把所有东西塞进一个 `utils.ts`。文件名表达用途，改动能快速定位。 |
| 是否重复 | 页面级组件可以因复用价值而共享；纯函数只有在至少两个独立功能确实需要相同规则时才抽取。一次性逻辑留在原功能里，避免抽象反而增加跳转。 |
| 职责清晰 | UI 组件只负责呈现和交互；格式化、校验、后端调用、任务状态分别归属不同模块。不得让“公共方法”偷偷读取或修改不相关的全局状态。 |
| 类型与错误 | 使用明确的 TypeScript 类型和具体返回值；异步调用统一返回可识别错误，不能静默吞掉错误、返回空对象假装成功或滥用 `any`。 |
| 可访问性与一致性 | 状态反馈、错误文案和键盘操作用同一套公共组件；颜色和间距用设计令牌。不能让业务页面各自发明一套加载、错误和禁用表现。 |
| 性能 | 搜索防抖只用于确有频繁输入的入口；事件处理按实际需要订阅并及时取消；不为几十条数据提前做复杂缓存、虚拟化或通用状态框架。 |
| 安全 | React 默认文本渲染已转义，优先使用普通文本；不额外造 HTML 转义器后再用 `dangerouslySetInnerHTML`。外链限制协议和受信任域名，不能执行后端返回的命令或路径。 |
| 多语言 | 可见文案集中在语言 JSON，变量通过命名参数传入；日期、数字和单位用 `Intl` 格式化，不在组件里拼接格式。新增语言继续只增加一个完整语言文件。 |
| 测试 | 公共方法测试边界和失败情况；组件测试交互、加载、禁用和键盘状态；跨页面行为用少量集成或端到端测试覆盖。 |
| 升级兼容 | 只依赖锁定版本支持的 Web API 和 React 能力；封装 Wails 运行时，升级 Wails 时只需适配 API 边界并运行契约测试。 |

### 4.2 值得共享的前端方法

| 文件 | 方法或内容 | 责任与边界 |
|---|---|---|
| `frontend/src/api/client.ts` | `getAppStatus`、`getItems`、`getStrategies`、`detectItems`、`selectStrategy`、`applyItems`、`restoreItems`、`openHosts`、`getSettings`、`saveSettings`、`cancelTask`、`getTasks`、`pauseQueue`、`resumeQueue`、`retryFailedTasks`、`clearFinishedTasks`、`clearQueue`、`checkForUpdates` | 对应技术规划中的 18 个命令接口；集中调用 Wails 绑定，参数和返回值使用明确类型；组件不能直接调用 Wails 运行时，也不能在此实现业务规则 |
| `frontend/src/api/errors.ts` | `toAppError`、`getErrorMessage` | 把调用异常整理成稳定错误码和可展示文案；保留原错误供日志，不能改成成功或空数据 |
| `frontend/src/api/events.ts` | `subscribeTaskProgress` | 订阅带单调递增 `sequence` 的 `TaskProgress` 事件（不是命令接口）并返回取消订阅函数；组件或 hook 销毁时必须调用 |
| `frontend/src/utils/format.ts` | `formatLatency`、`formatBytes`、`formatDuration`、`formatTimestamp` | 统一数值和时间格式，单位明确，日期数字走 `Intl`；不显示没有下载实测依据的“下载速度” |
| `frontend/src/utils/validation.ts` | `validateSettings`、`validateRequiredValue`、`validateNumberRange` | 检查前端表单格式并返回字段级错误；后端仍负责最终校验 |
| `frontend/src/utils/safeText.ts` | `isTrustedExternalURL` | 仅在打开动态外链时检查 `https:` 和允许的 GitHub 主机；普通文字使用 React 默认转义 |
| `frontend/src/i18n/index.ts` | `t`、`setLocale`、`getLocale` | 按稳定文案键读取资源、替换命名参数并统一回退；组件不手工拼翻译片段 |
| `frontend/src/i18n/formatters.ts` | `formatLocaleDate`、`formatLocaleNumber` | 用当前语言和 `Intl` 格式化日期、数字；不自建日期格式规则 |
| `frontend/src/hooks/useAppStatus.ts` | `useAppStatus` | 读取并订阅权限、平台和服务状态；负责加载反馈和取消订阅，不复制全局状态 |
| `frontend/src/hooks/useDebouncedValue.ts` | `useDebouncedValue` | 仅合并搜索等高频输入；组件卸载时清理计时器，不用来掩盖慢请求 |
| `frontend/src/features/tasks/taskEvents.ts` | 任务事件到 task store 的映射 | `api/events.ts` 负责底层订阅和取消；任务事件映射留在任务功能内，不额外建立通用 `useTaskEvents` hook |
| `frontend/src/stores/settingsStore.ts` | 持久配置快照、draft、保存状态和更新 action | 所有需跨重启保留的设置（包括独立的 `detectFocusFollow`、`applyFocusFollow`）通过后端 `GetSettings`、`SaveSettings` 保存；前端不得以 `localStorage` 建立副本 |
| `frontend/src/features/tasks/taskStore.ts` | `replaceTaskSnapshot`、`upsertTask`、`removeTask`、`markTaskCancelled`、`removeFinishedTasks`、`tasksForItem` | 任务状态唯一来源；终态集合为成功、跳过、失败、回滚、取消、已恢复。快照和事件按 sequence 合并；“查看日志”只筛选当前内存队列中该项目的记录，不承诺跨清理或重启保留 |
| `internal/detector/probe.go` | `ProbeTCP`、`ProbeHTTPS` | 单候选探测接收 `context` 截止时间；HTTPS 保留 SNI/主机名和证书校验。多个候选由策略层提交给共享有界 worker pool，不在每个策略中另造并发框架 |
| `frontend/src/features/*/*Selectors.ts` | 功能内纯查询方法 | 仅放同一功能里被多个组件复用的筛选、排序和派生状态，不建跨功能万能 selector |
| `frontend/src/styles/tokens.css` | 颜色、间距、尺寸、圆角、层级和焦点令牌 | 唯一全站视觉令牌来源；组件引用变量，不复制样式常量 |

### 4.3 GUI 状态与数据流

- 后端目录、策略注册表、配置、Hosts 实况和任务 manager 分别是这些业务数据的唯一权威来源；前端 store 只是带版本的缓存/视图状态，不是第二数据库。
- 每种业务数据只在一个 feature store 中维护；按稳定 ID 归一化。树、详情、工具栏、任务队列和计数用 selectors 从同一状态派生，不分别复制同一条记录。
- 数据读取使用“请求中/成功有数据/成功为空/失败/过期”等明确状态。不得把失败吞掉后当空结果或演示数据。
- 用户操作由 feature action 调 API；Go snapshot/command DTO 和生成绑定是接口类型来源。任务进度由 Wails typed event 通知，应用级订阅统一注册和取消。
- 所有可变实体含 revision/sequence；RPC 响应和事件共用版本比较，旧响应、重复事件、迟到进度不覆盖新状态，任务终态不可回退。初次进入先订阅再调用 `GetTasks` 拉取带 sequence 水位的快照；事件序号有缺口、连接重建或窗口重新激活时重拉后端快照。
- 方法选择按 `scopeID` 持久化。保存中与已保存状态分开；确认失败/冲突时不能显示为已生效，也不能因请求乱序覆盖用户较新的选择。
- Hosts 应用、还原等系统操作不做假成功式乐观更新。前端可显示 pending，但只有后端确认/权威事件才能更新“已应用”状态；重复命令用 `operationID` 做幂等保护。
- 检测/应用焦点跟随是两个独立设置，默认关闭；自动展开分类不移动键盘焦点。任务期间用户手动切换分类后，结束时保留用户最新状态，不用旧快照覆盖。
- 项目还原按钮只在该项目有活动的 Hosts 应用状态时显示；后端确认单项还原成功后隐藏入口并显示“已恢复”。共用映射不能因还原其中一个项目而删除其他项目仍在使用的记录。
- 异步请求按查询 key 与请求代次提交结果；快速切换菜单后，旧菜单请求只能更新自己的缓存，不能覆盖当前页面。取消旧请求不能替代版本校验。
- `useEffect` 只用于外部订阅等生命周期同步，并且必须清理；用户点击动作由事件 handler/action 发起，派生状态在渲染时计算，避免重复 Effect 和状态分叉。
- 详细数据所有权矩阵、竞态规则、GUI 风险清单、研究依据和验收测试见 [前后端结构规划](./FRONTEND_BACKEND_STRUCTURE_PLAN.md) 的“GUI 状态所有权与数据流”。

### 4.4 抽取规则和明确不做的事

- 抽取前先搜索真实调用点；纯函数通常要有至少两个独立调用方，并且规则含义完全一致，才提升为公共方法。
- 一个调用方但承担稳定系统边界的模块可以保留，例如 `api/client.ts` 是前端唯一的 Wails 调用入口。
- 仅服务一个页面的状态、映射、专用校验和渲染逻辑放在对应 `features/<功能>/`，不放 `utils`。
- 服务方法选择属于业务配置，由后端配置接口保存；前端 store 只维护读取结果和操作状态，不另行写入 `localStorage` 作为第二数据源。
- 不建万能 `utils.ts`、`helpers.ts`、`common.ts`；也不建只转发一行代码、没有独立规则的包装文件。
- 不自建 HTML 转义后再注入 HTML 的工具；不做自动重试所有请求、静默本地存储回退或捕获后不报告的通用封装。
- 不为一两处简单使用自建缓存、复杂 memo、全局 hook 或抽象状态机；先以真实性能数据和重复维护成本为依据。
- 修改公共方法时搜索所有调用方，更新类型、单元测试和使用文档；删除方法前同样检查依赖。

## 5. Go 后端公共组件

建议放在 `internal/` 下。只供本应用内部复用的实现保持为私有包，不为尚不存在的外部项目建立公共 Go 模块。

| 组件 | 职责 | 边界 |
|---|---|---|
| `app` | 校验前端请求、调用业务服务、转换对外错误 | 不放 DNS、Hosts 或系统操作实现 |
| `catalog` | 提供服务、项目、域名和稳定 ID | 产品数据只有一个来源 |
| `config` | 读取、校验、迁移和保存用户配置 | 配置写入失败要返回错误 |
| `task` | 管理任务 ID、状态、进度、取消和清理 | 不包含 DNS 或文件操作细节 |
| `event` | 把任务进度和状态变化发给前端 | 事件字段稳定、数据最少、进度有限频 |
| `accelerator` | 自动收集、选择和路由独立加速方法 | 策略同包自注册；新增一种方法不修改中心清单或其他功能代码 |
| `detector` | 域名解析、连通性探测和候选排序 | 不写 Hosts，不吞掉失败原因 |
| `hosts` | 解析、备份、合并、写入、验证和还原 Hosts | 只改带本工具标记的记录 |
| `platform` | 提供平台信息、Hosts 路径、权限查询、辅助服务生命周期、打开文件和 DNS 刷新 | 各系统实现分开；不定义 IPC 协议，也不执行任意命令 |
| `privilege` | 持有固定 IPC 协议、校验请求并调用受限特权辅助服务 | 只提供白名单 Hosts 操作，不接受任意路径或命令 |
| `updater` | 查询稳定版、校验下载清单和签名 | 不绕过校验执行更新 |
| `logging` | 记录操作和故障并清理敏感信息 | 不记录令牌、完整 Hosts 内容或不必要的个人数据 |

检测、更新和具体系统操作由各自业务模块负责，不堆进一个“万能工具包”。

## 6. Go 公共方法规划

### 6.1 多维度分析

| 维度 | 分析结论 |
|---|---|
| 复用范围 | Hosts 事务、任务状态、配置读写等会被多个业务流程共用，应集中在有明确业务职责的包里。只出现一次的逻辑先留在原模块，不提前抽象。 |
| 泛型价值 | Go 泛型适合同一算法处理多种类型；本项目的大多数规则针对特定配置、域名、任务或 Hosts 记录，强行泛型化会让调用和报错更难读。 |
| 类型安全 | 优先使用具体结构体、接口和明确的 `(value, error)` 返回值；不使用 `any`、反射或泛型结果容器绕过类型约束。 |
| 维护成本 | 公共方法按业务边界放在 `config`、`task`、`hosts` 等包；不建立不断堆积无关方法的 `utils`、`common` 或 `helpers` 包。 |
| 错误处理 | 底层用 `%w` 保留错误原因，调用边界补充操作和对象信息；对外错误转换集中在 `internal/app/errors.go`。不返回成功形状的默认值，不吞掉错误。 |
| 并发与取消 | 网络、队列和系统操作接收 `context.Context`；共享状态由所属服务管理锁和生命周期，不把锁或全局状态藏进通用工具。 |
| 安全边界 | 权限辅助服务只暴露固定 Hosts 操作；不能做通用命令执行、任意路径读写或任意内容写入。复用不能扩大权限。 |
| 兼容与升级 | 优先使用锁定 Go 版本的标准库。泛型从 Go 1.18 起已支持；不为使用泛型追最新 Go，也不依赖实验特性。升级时以构建、平台兼容和测试通过为准。 |
| 性能 | 先实现清楚、可测的方案；只有基准测试或真实数据证明有瓶颈，才加入缓存、对象池、并发限制或复杂抽象。 |
| 测试 | 公共方法在所属包旁测试正常、失败、边界和并发行为；跨包流程另做集成测试，Hosts 一律使用临时文件。 |

### 6.2 泛型使用规则

- 使用 Go 泛型，但只用于可复用、类型关系明确且确有多处调用的算法；不把“用了泛型”当成质量目标。
- 优先使用标准库 `slices`、`maps` 等类型安全操作；不重复造 `Map`、`Filter`、`Clone`、`Contains` 等工具。
- 当前不规划自建泛型 `Result[T]`、`Option[T]`、通用缓存、通用任务队列或泛型 DTO。Go 的 `(value, error)`、具体任务模型和业务接口更直白。
- 只有同一段非业务算法在至少两个独立业务包重复、签名保持类型安全、且调用处更清楚时，才评估提取泛型函数；必须有测试和代码评审说明。
- 泛型函数放在实际所属的业务包；不因方便使用 `any`、反射、类型断言链或隐藏错误的默认值。

### 6.3 公共方法和文件边界

| 文件 | 规划方法 | 复用范围 |
|---|---|---|
| `internal/app/bindings.go` | `GetAppStatus`、`GetItems`、`GetStrategies`、`DetectItems`、`SelectStrategy`、`ApplyItems`、`RestoreItems`、`OpenHosts`、`GetSettings`、`SaveSettings`、`CancelTask`、`GetTasks`、`PauseQueue`、`ResumeQueue`、`RetryFailedTasks`、`ClearFinishedTasks`、`ClearQueue`、`CheckForUpdates` | 与技术规划中的 18 个命令接口逐项对应；检查请求并转给业务服务 |
| `internal/app/errors.go` | `ToPublicError` | 把内部错误转换成稳定、可展示的错误码和消息；保留底层原因用于日志 |
| `internal/catalog/catalog.go` | `Services`、`ItemsForService`、`FindItem`、`ValidateIDs` | 服务、项目、域名和 ID 的唯一来源 |
| `internal/config/store.go` | `Load`、`Save` | 配置读写、原子保存及明确报错 |
| `internal/config/defaults.go` | `DefaultConfig` | 唯一默认值来源；新安装默认省流、自动检测关闭 |
| `internal/config/validate.go` | `Validate` | 校验配置范围、服务 ID 和已注册的方法 ID |
| `internal/config/migrate.go` | `Migrate` | 把旧配置逐版本迁移到当前结构 |
| `internal/task/manager.go` | `Start`、`Get`、`List`、`Pause`、`Resume`、`Cancel`、`RetryFailed`、`ClearFinished`、`ClearAll` | 暂停只阻止领取新等待任务，不中断执行中任务；重试创建关联原任务的新 ID；`ClearFinished` 只移除终态，可与其他任务并行；`ClearAll` 清除包括排队任务在内的全部记录，但有执行中任务时拒绝 |
| `internal/task/transition.go` | `CanTransition`、`Transition` | 集中检查合法状态变化，不重复散落条件判断 |
| `internal/event/publisher.go` | `Publish`、`PublishTaskProgress` | 统一事件结构和限频；任务状态仍由 task 管理 |
| `internal/detector/resolver.go` | `Resolve` | 可取消、有超时的系统 DNS 解析；首期不自动向第三方解析器发送查询 |
| `internal/detector/probe.go` | `ProbeTCP`、`ProbeHTTPS` | 共享候选连通性检测；HTTPS 保留主机名和证书验证 |
| `internal/detector/ranking.go` | `RankCandidates` | 统一排序候选，不把不同方法的评分硬合并 |
| `internal/hosts/parser.go`、`renderer.go` | `Parse`、`RenderManagedBlock` | 统一解析和生成版本化管理区段；标记异常或用户改动冲突时拒绝覆盖，区段外用户记录原样保留 |
| `internal/hosts/transaction.go` | `Apply`、`RestoreItem`、`RestoreAll` | 持久化每个活动项目应用前的工具映射/所有权；单项正常还原只恢复对应项目映射，共享映射仍被其他项目使用时保留；原始文件快照只用于事务回滚 |
| `internal/platform/platform.go` | `HostPath`、`FlushDNS`、`OpenFile`、`PrivilegeServiceStatus` | 统一跨平台接口，系统差异由平台文件实现 |
| `internal/privilege/client.go` | `Status`、`ReadHosts`、`ApplyHosts`、`RestoreHosts` | 固定 IPC 白名单；不暴露通用文件和命令接口 |
| `internal/updater/client.go` | `CheckStableRelease` | 查询 GitHub 最新正式稳定版，不自动安装 |
| `internal/updater/verify.go` | `VerifyDigest`、`VerifySignature` | 更新包安装前的摘要与签名校验 |
| `internal/logging/redact.go` | `Redact` | 删除日志中的令牌、私人数据和不必要的敏感信息 |

这些是目标接口，不代表现在已有实现。实现时只新增有真实调用方的方法和文件，不一次性创建空文件。

### 6.4 加速方法单文件扩展约束

- `internal/accelerator/strategies/` 是一个已由应用装配层固定依赖的 Go package。该目录中的方法文件属于同一 package，正常构建会自动编译，不需要逐个 import。
- 一种方法的实现、稳定 `strategyID`、目标 `scopeID`、名称/说明、能力和 factory 放在同一个 `.go` 文件中；文件通过包内 `init` 调用既有 `register`。新增方法不编辑中心 registry、服务路由、产品目录、Wails API/DTO、前端或设置 schema。
- 单文件指源码新增后重新构建应用，不代表运行中热加载；策略使用标准库和项目已有依赖，新增方法不为此改依赖清单。
- `registry.go` 和 `contract.go` 仅是一次性基础设施：集中收集描述符、校验 ID/范围/能力，输出稳定排序的只读方法列表。注册错误必须保存并在启动时作为普通 error 返回；不 panic、不静默覆盖、不依赖文件系统扫描或 Go 插件。
- 每个策略只指向已存在的 `scopeID`。不同菜单可共享一个范围 ID，也可由产品目录分别配置独立范围。新增方法不需要改目录；新增一个菜单/范围本身则属于目录变更，不在单文件新增方法的承诺范围内。
- 空范围返回空列表并展示空状态；一个可用方法可在无已保存选择时预选；多个可用方法在无有效选择时要求用户明确选择。任何已有选择都按 `scopeID` 保存，添加方法不覆盖它，失效选择不自动切换到其他方法。
- 通用契约测试遍历注册表中所有方法，新增一项自动纳入基础校验；复杂策略仍应添加相邻测试，覆盖特定网络算法、超时、错误和平台行为。单文件是“接入新方法”的改动上限，不是免除新逻辑验证。

### 6.5 抽取与删除规则

- DNS 解析、候选连接检测、任务进度和 Hosts 事务各保留一个权威实现；加速方法只组合调用。
- 配置原子保存和 Hosts 写入事务不是同一种业务；不能为少量重复代码合成可写任意路径的通用文件工具。
- 首期配置使用一个带 `schemaVersion`、`revision` 的 JSON 文件；不引入 SQLite、浏览器 `localStorage` 副本或云同步。Go 配置服务负责默认值、迁移、校验、revision 比较和同目录临时文件原子替换。
- `SaveSettings` 按字段 patch 提交，并携带 `expectedRevision`、`mutationID`；配置服务串行化写入，冲突和持久化错误必须显式返回。新版本未知配置不得被旧版本重写；损坏配置保留一个受控副本并提示用户，不能静默覆写。
- 设置中心表单以 draft 编辑，保存时一次提交 patch，取消时丢弃；检测区快速切换档位是单字段即时保存。两者都走同一个后端 API 和 revision 检查，不能各自写存储。
- 持久化用户偏好包含网络档位/探测上限、按 `scopeID` 的默认策略、主题/语言/缩放/布局及启动选项。运行中的任务队列、检测结果、当前勾选项、搜索词和展开项首期不持久化；任务启动时冻结配置快照。
- 配置只存非秘密数据；未来若有令牌/凭据，使用操作系统凭据库，配置 JSON 只保存引用。日志不得记录配置原文或秘密。
- 接口只在需要替换平台实现、模拟外部依赖或维持跨包契约时引入；不为每个结构体机械地定义接口。
- 抽取公共方法前搜索真实调用点；只有一次调用时先留在业务模块。删除公共方法前搜索全部调用方并更新测试。

### 6.6 IP 探测并发和用户等待预算

- 检测 IP 地址应并发探测，不允许对候选列表逐个串行等待超时；首期全任务使用共享有界并发，最多 8 个活动 DNS/IP 请求，单域名最多探测 16 个候选。
- 首期固定预算：DNS 2 秒/查询，单 IP TCP+TLS 整体 2 秒，单项检测最多 5 秒，服务全量扫描最多 30 秒。deadline 经 `context.Context` 传递，取消或超时必须停止尚未完成工作。
- 界面逐个展示每个 `candidateID` 的完整生命周期：排队中、检测中、可用、不可用、超时、已取消或因总时限未开始。DNS 阶段显示正在解析；拿到结果后立即显示候选行，状态转换事件即时更新该行，不等待整批任务完成。
- 已结束的候选行保留结果、耗时和错误/超时原因；活动行显示忙碌状态与已耗时。界面同时报告解析/探测阶段、完成数/总数和活动并发数；允许筛选或折叠已结束项，但默认提供可见汇总。
- deadline 到期返回部分结果，并区分失败候选、超时候选、未开始和用户取消；不把部分完成伪装为全部成功或所有 IP 失败。
- 状态事件含序号以防乱序覆盖；活动耗时由前端本地计时，屏幕阅读器以节流的总体摘要播报，避免高频并发逐条打断。
- 预算是工程初始值，发布前用慢 DNS、超时 IP、IPv4/IPv6 和最大服务目录实测；变更必须同时调整后端 deadline、用户文案和测试。

### 6.7 移动/计量网络流量控制

- 设置项提供省流、标准、完整检测三档；每域名最多候选分别为 2、8、16，全任务并发分别为 2、8、8（安全硬上限 8）。另可配置每轮最大候选数，防止批量项目累加后产生意外探测量。
- 并发主要改变瞬时流量/等待时间，限制候选 IP 数及单轮总请求数才直接减少尝试总数。界面应把这两种影响分别说明，不能用“降低并发”等同于“流量一定减少”。
- 检测只发送 DNS 查询及 TCP/TLS 建连/证书验证，不请求网页 GET 内容或测速文件，不读取大响应体。网络错误不自动重试；用户重试要可见。更新检查只请求 release 元数据，下载更新包必须由用户确认并事先显示大小/来源。
- 设置持久化在后端，检测任务启动时将其解析为不可变的有效配置快照，避免用户在任务中改设置造成并发上限和总候选预算不一致。后端必须强制档位上限，前端限制不能代替后端校验。
- 每轮候选 IP 探测预算为 `min(本轮唯一目标 hostname 数 × 档位每 hostname 候选上限, 用户设置总上限)`；按实际启动的 IP 探测数扣减，DNS 查询单独计数。菜单项目可以包含多个 hostname；同一轮按规范化 hostname 去重，不能用项目数代替域名数。任务启动前显示预算并冻结配置。若用户上限可能导致范围未覆盖，先请求确认；确认后超额 hostname/项目明确标记未检测。单项、批量、服务检测及应用前补充检测共用预算执行器。
- 可显示应用层的请求计数和估算网络字节，但必须标为估算；系统 DNS、协议栈和其他应用流量无法可靠纳入，不能承诺运营商账单级的 MB 硬限额。
- 自动计量网络识别仅可用于建议或默认切省流模式；若跨平台无法判断，不得默认认定无限流量。用户可手工指定当前为计量网络，自动任务在该模式下不启动全量扫描。
- 依据 Go 官方 TLS API，可使用可取消的 TLS 握手进行连通性验证而不下载网页；Go HTTP 的 HEAD 仍是 HTTP 请求，不能视为绝对零流量。Apple 能通过 `NWPath.isExpensive` 标识蜂窝/个人热点，但该 API 不代表 Windows/Linux 均有同等能力。参考：[Go TLS](https://pkg.go.dev/crypto/tls)、[Go HTTP](https://pkg.go.dev/net/http)、[Apple `NWPath.isExpensive`](https://developer.apple.com/documentation/network/nwpath/isexpensive)。
- 用户在设置对话框的“网络与流量”分类调整档位和每轮上限；检测按钮旁常驻一个紧凑状态标签（当前档位 + 本轮候选预算），点击可快速切档或跳转完整设置。
- 设置页必须解释各档对候选覆盖、并发和流量的区别；提供“自动检测”与计量网络下载确认选项，省流档默认关闭自动全量检测。设置保存失败要显示在控件旁，不允许只更新标签就当成功。
- 首次安装或没有有效配置时默认“省流”（每 hostname 最多 2 个候选、并发 2）；用户设置的总候选探测上限暂定为 108，来源为现有 54 个菜单项目 × 每项 2 个候选的估算。菜单项目数不等于唯一 hostname 数，108 是可调整的默认预算，不保证完整扫描，也不是后端硬上限；实现前须按规范化唯一 hostname 数验证最大范围。实际每轮预算随范围和档位动态计算；自动检测默认关闭。

## 7. 建议目录

```text
cmd/
└── privileged-helper/
    └── main.go

frontend/src/
├── api/
│   ├── client.ts
│   ├── errors.ts
│   └── events.ts
├── components/
│   └── ui/
│       └── Select.tsx
├── features/
│   ├── settings/
│   │   └── NetworkUsageSettings.tsx
│   ├── strategies/
│   │   ├── ServiceStrategySelector.tsx
│   │   ├── StrategyStatus.tsx
│   │   ├── strategyStore.ts
│   │   ├── strategyTypes.ts
│   │   └── strategySelectors.ts
│   └── tasks/
│       ├── taskStore.ts
│       └── taskEvents.ts
└── styles/

internal/
├── accelerator/
│   ├── model.go
│   ├── service.go
│   └── strategies/
│       ├── contract.go
│       ├── registry.go
│       ├── hosts_dns_lookup.go
│       └── hosts_dns_lookup_test.go
├── app/
│   ├── bindings.go
│   ├── dto.go
│   └── errors.go
├── catalog/
│   ├── catalog.go
│   └── catalog_test.go
├── config/
│   ├── config.go
│   ├── defaults.go
│   ├── validate.go
│   ├── migrate.go
│   └── store.go
├── detector/
│   ├── resolver.go
│   ├── probe.go
│   └── ranking.go
├── event/
│   ├── event.go
│   └── publisher.go
├── hosts/
│   ├── parser.go
│   ├── renderer.go
│   ├── backup.go
│   └── transaction.go
├── logging/
│   ├── logging.go
│   └── redact.go
├── platform/
├── privilege/
│   ├── client.go
│   └── protocol.go
├── task/
│   ├── manager.go
│   ├── model.go
│   ├── events.go
│   └── transition.go
└── updater/
    ├── client.go
    ├── manifest.go
    └── verify.go
```

目录可以随项目实际代码调整；新增目录前先确认已有模块是否能承担该职责。

## 8. 实施顺序

1. 拆出 `Modal`、`Toast`、`Button` 等重复使用最多的前端组件。
2. 统一状态徽章、空状态、进度条和任务表格。
3. 将设置中心的主题、语言和表单控件拆成独立组件。
4. 定义 Wails API 封装和统一错误格式，再逐页移除模拟逻辑。
5. 后端先抽取 Hosts 操作、任务状态和平台接口；遇到第二个真实调用点时再抽公共实现。
6. 删除旧主题弹窗、重复脚本和不再使用的样式。

每一步都保持页面可运行，不一次性重写所有界面。

## 9. 验收要求

- 同一组件只保留一份实现，页面通过接口传入数据和事件。
- 组件可单独验证加载、空数据、禁用和失败状态。
- 每个可操作控件都能显示处理中和最终结果；慢任务不能无限转圈或只依赖 Toast。
- 超时、后端断连、取消和重复提交都有可见且一致的反馈。
- 任务取消或页面关闭后，不留下仍在运行的事件监听。
- Hosts 组件使用临时文件测试，不修改开发机真实 Hosts。
- 检测和应用焦点跟随分别持久化且默认关闭；自动展开不移动键盘焦点，任务结束时保留用户在运行期间手动调整的分类状态。
- “清理已结束”只移除成功、跳过、失败、回滚、取消和已恢复任务，排队中/执行中任务保留；“清空队列”会移除排队任务，但存在执行中任务时拒绝。清理终态可与其他任务并行。
- Hosts 单项还原恢复该项目应用前的工具映射；多个项目共用映射时，其他所有者仍在使用的记录必须保留。区段外用户内容不变，冲突时不覆盖。
- Hosts 全部还原逐项处理并返回成功/失败清单；元数据或区段冲突时保留现场。恢复成功后隐藏该项目的还原入口，再次成功应用后重新建立还原点。
- 前端显示的真实状态来自后端；模拟数据只在明确的演示环境中使用。
- 选择器使用“加速方法”作为界面名称，选项说明用户能理解的动作，例如“自动查找可用 IP”，不要求用户先懂 DNS、Hosts 等术语。
- 加速方法选择器放在当前服务详情中；GitHub、Steam 等服务分别展示和保存方式，不提供会覆盖所有服务的全局选择器。
- 选择区居中放入有明显底色和边框的单行紧凑标签块；标签显示“GitHub 全部加速方法”等服务名称，删除重复的“用于该服务全部项目”提示，不增加详情栏高度。
- GitHub 下所有二级菜单共用 GitHub 的加速方法选择；Steam 同理。不同服务按稳定服务 ID 分别保存。
- 更换加速方法后持久保存该服务的选择；切换本身不启动检测，之后的服务、单项、批量和自动检测都使用用户已选的方法。
- 服务检测会检测该服务所有项目，但不轮流测试所有加速方法；未注册或未实现的方法不显示为可用。
- 检测失败、超时和进行中状态独立显示，不混进方法名称。
- 检测只获取当前已选方法的结果，不自动写 Hosts；应用由用户另行发起，使用对应服务已选的方法。切换方法后需由用户主动重新检测。
- 检测结果归属到执行检测时所选的服务方法；失败或不可用状态要明确显示。
- 选择某服务以后默认使用的方法，只保存该服务的偏好；不自行改写 Hosts 或系统网络设置，也不影响其他服务。
- 系统权限只在安装或首次启用受限辅助服务时请求一次；后续 Hosts 操作复用该服务，不重复向用户索取 root/管理员权限。主界面保持普通用户权限，辅助服务不可用时明确报错并停止修改。
- 关于页面的项目主页、问题反馈、MIT 许可协议入口使用真实 GitHub 链接，在新标签页打开；许可正文以仓库根目录 `LICENSE` 为准。
- 每种加速方式有独立实现和测试；公共 DNS、任务、配置和 Hosts 操作逻辑只保留一份。
- 删除或替换组件前，搜索所有调用点并更新测试。
- 泛型只用于有重复价值的类型安全算法；标准库已有操作时不重复实现，不出现无调用方的公共工具。
- 公共方法位于职责明确的业务包；错误可追踪、操作可取消，系统权限接口不扩大为任意执行能力。
- Wails 调用只有一个入口，任务事件订阅均有对应清理；服务加速方法的持久数据只由后端配置服务保存。
