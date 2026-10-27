# 前后端目录结构规划

## 一、规划总览

### 1. 技术基线

- 后端：Go ≥ 1.27.3。
- 桌面框架：Wails v3.0.0（首期锁定；升级前重新跑平台验证）。
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
- 简体中文是默认语言。`zh-TW` 只有在所有界面文案、错误、确认框、辅助技术标签和插值参数都齐全，并经繁体中文人工审校后才算受支持；未通过时显示“翻译未完成”，不得把简体回退内容伪装成繁体。
- CI 对受支持语言强制检查键集合、参数集合和格式；运行时缺键可安全回退简体中文，但必须留下可诊断的非敏感错误，不能静默吞掉。

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

### 3.1 GUI 状态所有权与数据流

**总原则：Go 后端是业务数据和系统状态的唯一权威来源；React store 是带版本的界面缓存与投影，不是第二个数据库。** 组件只能通过所属功能的 action/API 读取或修改数据，不自行复制另一份列表或直接调用 Wails runtime。

#### 数据的唯一所有者

| 数据 | 权威来源 | 前端保存方式 | 不允许的副本/做法 |
|---|---|---|---|
| 菜单、服务、项目、域名及稳定 ID | `internal/catalog/` | 一个 catalog snapshot；详情、树、计数、筛选均由它派生 | 在组件/原型再维护一份项目列表；按显示文字而非 ID 关联 |
| 加速方法、范围和能力 | Go 策略自注册目录 | 按 `scopeID` 缓存 `GetStrategies` 的结果 | 前端硬编码方法清单；把不存在的方法显示为可用；把方法名当稳定 ID |
| 用户保存的范围方法选择 | 后端 config | `settingsStore` 缓存带 revision 的后端快照；待保存 draft 单独标记 | 用 `localStorage` 保存第二份业务选择；保存失败仍显示为已保存 |
| Hosts 实际状态、权限和平台状态 | 后端平台/Hosts 服务 | 按后端快照和事件更新的缓存 | 从前端候选检测结果推断“已应用”；将 UI 乐观状态冒充系统实况 |
| 后台任务及其生命周期 | 后端 `task` manager | `features/tasks/taskStore` 保存事件驱动快照 | 独立的全局任务 store、组件本地 task 副本或用 Toast 代替队列记录 |
| 当前导航、搜索词、展开项、弹窗、未提交表单 | 前端交互状态 | 组件局部状态；需跨多个功能共享时放对应 UI store | 写入后端业务配置；将可由现有数据算出的数量/列表再存一份 |
| 用户明确希望重启后保留的配置（业务与界面偏好） | Go `internal/config` | `settingsStore` 缓存带 revision 的后端快照；保存经后端确认 | 后端 JSON 与前端 `localStorage` 同时保存同一配置 |

列表使用稳定 ID 做归一化存储与关联；当前菜单、选中项目、过滤后列表、计数、批量选择摘要等作为派生值计算。更新一项权威记录后，所有使用该 ID 的视图从同一个状态投影更新，不能分别手工维护树节点、详情和队列中的“当前状态”。

#### 配置类别、存储方式与写入规则

**首期采用一个由 Go 后端独占读写的版本化 JSON 配置文件。** 设置体量小、无需关系查询；JSON 易检查、易迁移、无需数据库服务。SQLite 对本地偏好没有实际收益，首期不引入。所有要跨应用重启保留的设置（包括网络策略和界面偏好）统一进此文件；前端 `settingsStore` 只缓存后端快照与编辑状态，不再用 `localStorage` 保存副本。搜索词、展开项、弹窗、未提交草稿等临时 UI 状态留在前端内存。

| 方式 | 结论 | 原因 |
|---|---|---|
| Go 管理的 JSON 文件 | 首期采用 | 单文件、可读、迁移简单，适合少量用户设置；可以做原子替换和 schema 校验 |
| SQLite | 暂不采用 | 没有复杂查询、多用户并发或大量结构化记录需求；引入数据库会增加迁移、备份和损坏处理负担 |
| 浏览器 `localStorage` | 不用于持久配置 | 前端可被清理、与后端形成双写来源，后端也无法据此保证网络上限 |
| 环境变量/命令行参数 | 只用于开发/部署级覆盖（若确有需要） | 不适合作为普通用户设置的长期保存渠道；必须明确优先级，不允许静默覆盖用户设置 |
| 云同步 | 首期不做 | 增加账号、冲突、隐私和秘密数据处理问题；目前没有跨设备同步需求 |

| 配置分组 | 保存内容 | 默认/校验边界 |
|---|---|---|
| `network` | `networkProfile`、每轮候选 IP 探测总上限、计量网络下更新确认 | 新安装省流；总上限暂定默认 108（54 个现有菜单项目 × 每项 2 个候选），只是可调预算，不保证完整扫描；有效预算按唯一 hostname 和档位动态计算，并发和每 hostname 候选受服务端硬上限约束 |
| `accelerator` | `defaultStrategyByScope`（稳定 `scopeID` → `strategyID`） | 只保存仍注册且适用于该范围的选择；失效时提示重新选择，不静默换方法 |
| `appearance` | 语言、主题、缩放 | 使用明确的支持值；无效值回退默认并报告配置问题 |
| `layout` | 侧栏和队列高度等用户可调布局 | 数值限制在 UI 与后端约定范围内 |
| `treeBehavior` | `detectFocusFollow`、`applyFocusFollow` | 两个独立持久开关，默认关闭；只控制分类树临时展开，不移动键盘焦点 |
| `startup` | 开机启动、启动后最小化、启动自动检测/应用 | 自动检测和自动应用默认关闭；有依赖关系的选项由后端再次校验。原型另有「托盘最小化」「显示托盘图标」两个开关，不属于这份已定形状，是否纳入待用户裁决，实现里没自己加 |
| `update` | 用户明确设置的更新偏好（如适用） | 不保存令牌；更新包本身不写入配置 |

配置文件建议采用带 `schemaVersion` 和单调 `revision` 的结构，例如：

```json
{
  "schemaVersion": 1,
  "revision": 1,
  "network": {
    "profile": "low",
    "maxCandidateProbesPerRun": 108,
    "confirmUpdateDownloadOnMetered": true
  },
  "accelerator": {
    "defaultStrategyByScope": {}
  },
  "appearance": {
    "language": "zh-CN",
    "theme": "light",
    "zoom": 1
  },
  "layout": {
    "sidebarWidth": 240,
    "queueHeight": 252
  },
  "treeBehavior": {
    "detectFocusFollow": false,
    "applyFocusFollow": false
  },
  "startup": {
    "autoStart": false,
    "startMinimized": false,
    "autoDetectOnStartup": false,
    "autoApplyOnStartup": false
  }
}
```

- 配置路径固定在应用专属用户配置目录，沿用本文件“配置和运行文件”章节的平台路径约定；目录按用户权限创建，配置文件仅当前用户可读写。不得写入仓库、工作目录、Hosts 文件或散落的主目录路径。
- 文件不存在时从代码中的 `DefaultConfig()` 创建内存默认值，不因为读取而强制写盘；首次成功保存时再创建配置文件。默认值由 Go 统一定义，前端只展示后端返回值。当前勾选项目、搜索词和展开项首期不保存，应用重启后回到未选中状态，避免把上次选择误当成本次检测授权。
- `Load` 按步骤执行：读取 → 检查文件大小/格式/schema → 逐版本迁移 → 业务校验 → 返回带 revision 的快照。迁移必须可重复且有测试；遇到高于当前版本的配置时拒绝覆盖并明确提示，不用旧程序重写新配置。
- `SaveSettings` 使用字段级 patch、`expectedRevision` 和 `mutationID`，不能让一次只改主题的保存覆盖并发修改的网络设置。后端校验完整候选配置，再在配置文件同目录创建唯一临时文件、同步落盘并原子替换；成功后递增 revision，再发布更新。失败不得发布成功事件或更新为“已保存”。
- 设置中心控件分两类，都经过同一个 config service，不各自写文件。即时保存类（预设按钮、档位卡、每轮候选上限）点一下单独提交一个 patch 并显示保存状态，失败时恢复后端快照中的旧值；这一类按原型就是即时落盘（`segSidebar`/`segQueue` 第 1934–1952 行，网络页档位与上限第 1912–1932 行，网络页连页脚都隐藏，第 1875 行）。2026-10-28 设置中心第二屏接的布局页与网络与流量页属于这一类，所以对话框页脚只有「关闭」。draft 类（开关、下拉表单：语言、托盘、开机自启动、布局页的焦点跟随）先写入带 `baseRevision` 的草稿，「保存」提交一次 patch、「取消」丢弃；这批控件对应的界面还没迁，所以该路径目前只是口径，未实现。
- 单进程配置写入由配置服务串行化并进行 revision 比较；重复 `mutationID` 幂等返回原结果。主程序是唯一写入者，特权辅助服务不读写用户配置。任务启动时读取并冻结有效配置快照，之后设置变更只影响新任务。
- JSON 语法损坏或配置整体无法校验时，不静默覆盖原文件：保留原文件到一个有界的损坏隔离备份，使用代码默认值继续运行，并在设置页/启动提示中说明设置已重置及恢复位置。单字段迁移/修复也必须有明确日志和用户可见反馈；不记录配置中的敏感值。
- 不支持跨设备自动同步。首期不实现配置导入/导出；如后续需要，应设计带 schema 校验的用户主动导出/导入流程，导出时剔除凭据并对覆盖当前配置再次确认。
- 设置页可提供“恢复默认设置”操作：显示将被重置的配置范围并二次确认；写入仍经过同一 patch/revision/原子保存链路，失败时原配置继续生效。
- 配置只存非秘密偏好。将来若增加 API Token、代理凭据等秘密，放系统凭据库（Windows Credential Manager、macOS Keychain、Linux Secret Service），JSON 只保存凭据引用；不自行加密密钥后仍放在同一目录。
- `revision` 是内存/命令/事件的一致性版本，不替代磁盘原子保存；磁盘文件中的 revision 仅在完整保存成功后递增。前端若收到冲突，刷新权威快照并提示用户重新应用未保存的修改。

#### 单向数据流

```text
Go catalog / strategy registry / config / Hosts / task manager
        │  Get snapshot (value + revision)
        ▼
Wails generated bindings ← frontend api/client.ts
        │
        ▼
feature action → request validation → backend app service
        │                         ├─ read/update config
        │                         ├─ create/cancel task
        │                         └─ apply/inspect Hosts via platform boundary
        │
        ├─ command response: accepted/rejected + stable IDs + revision
        └─ typed event: entity ID + sequence/revision + state transition
                    ▼
       feature reducer/store (dedupe + stale-order guard)
                    ▼
       selectors → tree/detail/toolbar/queue/status rendering
```

- 用户事件只在对应 action 中发出命令；render 和 selector 必须是纯计算，不在 `useEffect` 中重复提交用户操作。
- Go 返回的 DTO/生成绑定是契约来源；手写前端类型只可包装/收窄生成类型，不能复制一份不受契约测试约束的 API schema。
- 命令响应负责说明“已接受/已保存/被拒绝”；异步任务的持续状态以任务快照和事件为准。事件不是另一个可持久化数据源，而是让前端及时追上后端状态的通知。
- 多个屏幕展示同一数据时只写一次功能 store；组件通过 selectors 查询，不把业务数据复制进局部 state。视图本身的展开、滚动和未提交编辑状态仍可局部保存。
- “空”必须与“尚未加载”“加载失败”“缓存过期”区分。只有后端成功返回的空列表才显示空状态，禁止错误时返回 `[]` 或演示数据伪装成功。

#### 并发、排序与及时一致性

- 每个可变聚合有单调递增 revision：单文件配置使用全局 config revision（范围方法仍按 `scopeID` 存储）；任务按 task ID、Hosts 状态按项目/管理区段。事件带稳定实体 ID 和 sequence/revision；前端只接受高于当前版本的更新，重复事件幂等忽略，过期事件不回滚状态。
- 每个读取请求带请求代次/请求 ID 和查询 key。快速切菜单、搜索、刷新或重复请求时，只允许当前 key 的最新结果提交；取消旧请求是优化，版本/代次校验才是正确性保障。
- Wails 事件可能先于命令响应到达；响应与事件都走相同的 revision guard，较旧响应不得覆盖较新事件。任务事件至少携带 `taskID`、序号、状态、阶段、时间和终态信息；终态不可被迟到的处理中事件改回。
- 事件订阅在应用服务层只建立一次，先注册监听再取权威快照；快照和事件按 revision 合并，旧版本丢弃。事件有序号缺口、监听重建、窗口重新激活或后端重启时重新读取相关快照，不能假定事件永久可靠或可回放。
- 对配置保存使用 `expectedRevision + mutationID`；具体字段 patch 可含 `scopeID`。全配置文件按全局 revision 串行化或由后端 compare-and-swap；冲突明确提示刷新/重试，不能让较早的慢请求覆盖较晚的用户选择。失败时保留当前已保存值，待保存 draft 标为失败并给出重试/放弃入口。
- 用户切换加速方法时，先显示“保存中”，保存成功/事件确认后才将其标为当前生效选择；保存期间禁用依赖新选择的检测/应用。失败不静默回退，也不让 GUI 显示与后端不一致的“已保存”。
- Hosts 修改、还原、清空队列等非纯 UI 操作都必须后端再次校验；前端禁用按钮只是 UX，不是并发、安全或权限控制。
- 焦点跟随设置由 Go 配置服务持久化。自动展开属于树的临时视图状态，不写回用户手动展开配置；用户在任务期间手动操作的分类状态优先保留。
- 操作有 `operationID`/`mutationID`，防止双击、重试和事件重复导致重复副作用。可安全重放的查询与不可重复写 Hosts 的命令必须区分；重复命令由后端拒绝、返回原结果或按幂等键处理。
- 进度更新可合并/限频，但任务创建、状态切换、失败、取消和完成等终态事件不能丢弃。进度百分比必须来自有定义的工作量；未知时使用不定进度，不伪造数字。
- 启动、窗口重新获得焦点、事件序号缺口、用户显式刷新和关键操作完成后，重新核对后端快照；展示更新时间或过期状态，不能无限显示旧缓存为实时事实。

#### IP 候选并发检测与等待预算

**首期采用有界并发，不逐 IP 串行等待。** 并发结果既要被 GUI 看见，也要有明确的用户等待上限；仅“后台并发但最后一次性返回”不满足交互要求。

| 阶段/范围 | 首期预算 | 规则 |
|---|---:|---|
| 单次 DNS 解析 | 最多 2 秒 | 通过 `context` deadline 取消；超时作为该项目明确结果，不阻塞整个服务扫描 |
| 单个 IP 的 TCP/TLS 可用性探测 | 最多 2 秒 | TCP 建连和保留主机名/证书验证的 HTTPS 探测共享单 IP deadline，不串联出两个完整超时 |
| 单项检测整体 | 最多 5 秒 | 包含 DNS、候选探测和结果整理；超时后返回已完成候选及仍未完成项的超时状态 |
| 服务全部项目扫描 | 最多 30 秒 | 对整个任务设总 deadline；到时保留部分结果，未完成项目标为“本轮未完成/已超时”，不伪报为失败 IP 或成功 |
| 网络并发 | 全任务最多 8 个活动请求 | DNS 与 IP 探测共用有界并发预算，避免每个项目各自启动 worker 导致并发数失控；最多检查每个域名 16 个候选，超出时显示候选数量截断提示 |

- DNS 阶段按项目并发解析；探测阶段按候选 IP 并发验证。单个 IP 失败/超时不阻塞其他 IP，只有同一主机名与候选 IP 的 HTTPS 验证通过才计为可用。
- **每个并发项都可见**：候选 IP 以稳定 `candidateID` 逐行呈现；状态至少包括“排队中、检测中、可用、不可用、超时、已取消、因总时限未开始”。DNS 解析期间显示“正在解析”，解析完成立即列出候选并以“排队中”出现，开始 worker 后转为“检测中”，每项结束后立即定格最终状态、耗时和简短原因；不能等整批扫描结束才绘制列表或仅显示汇总数字。
- UI 同时显示阶段与总进度，例如“解析域名 12/54”“正在检测 IP（8 个并发）”“已完成项目 9/54”。运行中的行应有可见忙碌指示和已耗时，结果行留在列表中而不闪退/被新结果覆盖；支持按状态筛选或折叠已结束项，但默认能清楚看到成功、失败和超时计数。
- 每次候选状态转换立即发送事件；不需要每帧发送 elapsed 更新，前端可用单个显示计时器更新活动行耗时。事件至少包含 `taskID`、`itemID`/`candidateID`、阶段、状态、完成数/总数、活动数、结果、开始/结束时间及序号。前端按序号幂等更新对应行。
- 辅助技术使用稳定、可查询的状态文本和汇总礼貌播报（例如“已完成 9/54 项，3 个可用、2 个超时”），避免每个快速并发事件都触发屏幕阅读器连续打断；详细状态仍可逐行访问。
- 不伪造剩余时间。可以显示已耗时和预算上限（“通常几秒，最多约 5 秒/本轮最多 30 秒”）；只有在可解释的已完成工作量足够时才给剩余时间估算，并标为估算。
- 用户取消立即取消任务 context，并把尚未开始候选标为“已取消”；已完成结果保留。总 deadline 到期是“部分完成/超时”，不能伪装为用户取消或全部失败。
- “用户需要等多久”的产品承诺按初始预算为：单项最多 5 秒；服务全量扫描最多 30 秒后一定有最终状态（完整或部分结果）。典型耗时目标单项 1–3 秒、54 项服务扫描 5–15 秒，但这些是待目标环境实测的体验目标，不是无条件保证。
- 以上数值是初始工程预算，发布前用慢 DNS、黑洞 IP、IPv4/IPv6、不同平台和最大目录规模测量；如需调整，必须同步更新 UI 文案、测试阈值和本节预算，禁止只改前端倒计时。

#### 移动网络与流量控制

**必须区分并发速度和总流量：**并发数影响瞬时带宽、耗电和同时连接压力；总流量主要由候选 IP 探测次数、重试/重复扫描和下载内容决定。单纯把并发从 8 降到 2 会拉长耗时，不一定显著减少总字节；要省流必须限制候选数/总探测数、避免重复请求，并保证探测不拉取网页正文。

| 用户模式 | 每域名最多候选 IP | 全任务最大并发 | 默认行为 |
|---|---:|---:|---|
| 省流 | 2 | 2 | 移动/计量网络建议使用；关闭自动全服务扫描和后台重复检测，只在用户发起时检测所选项目或明确选择的范围 |
| 标准 | 8 | 8 | 正常交互检测；全服务扫描仍须由用户或明确启用的自动化触发 |
| 完整检测 | 16 | 8（安全硬上限） | 用户主动选择；尝试更多 DNS 候选，不提高网络并发硬上限 |

- 设置提供 `networkProfile`（省流/标准/完整检测）、每域名候选上限、每轮总候选上限、自动检测开关、计量网络行为和更新下载确认。配置由后端持久化并经 revision 保持 GUI 一致；具体配置项可在实现时收敛，但不能只提供一个不解释效果的“速度”滑杆。
- 首次安装及配置缺失时默认使用“省流”档（每 hostname 最多 2 个候选、全任务并发 2）；可配置总候选探测上限暂定默认 108，来源为现有 54 个菜单项目 × 每项 2 个候选的估算。菜单项目数不等于唯一 hostname 数，108 只是可调预算，不保证完整扫描，也不是后端硬上限。实现前须按规范化、去重后的 hostname 数核算最大范围；该核算已于 2026-10-27 完成：目录实际为 54 个项目、去重后 65 个 hostname，省流档全量一轮理论值 65 × 2 = 130，高于默认 108，所以按现值执行时全量一轮必须走「启动前确认 + 超额项标未检测」；默认上限是否上调待用户落句，落句前一律按 108 实现。实际每轮预算为 `min(本轮唯一目标 hostname 数 × 档位每 hostname 候选上限, 用户总上限)`，只统计实际启动的 IP 连通性探测，DNS 查询单独计数。目录项保存 hostname 而非完整 URL；URL 项先提取 hostname，大小写不敏感、尾随点统一，国际化域名使用同一 IDNA 规范；端口、路径和 query 不计为不同目标。同一轮按该规范化 hostname 去重。后台自动检测默认关闭，不因默认档位或打开设置而自动发起检测。
- 若用户总上限低于本轮理论最大探测数，启动前展示差额并请求确认；确认后按当前范围顺序执行，按实际启动的候选 IP 探测数扣减预算，预算耗尽后的域名/项目明确标记“预算不足，未检测”。单项、批量、服务范围和应用前补充检测共用同一预算路径；拒绝确认则不启动检测，也不继续应用队列。
- 即使用户选择完整检测，每轮仍受安全硬上限约束：活动网络请求最多 8、候选最多 16/域名、任务 deadline 不超过既定 5 秒/单项与 30 秒/服务。省流配置采用更低上限。界面显示根据本轮范围计算的有效候选预算，不把 108 显示为每轮固定用量；DNS 查询单独说明。
- **探测只做连接/握手，不下载目标网页、测速文件或大响应体。** 首期使用 DNS 查询与 TCP/TLS 建连/证书验证；不发送 GET 下载页面，不做文件测速，不为测速度请求大文件。若未来策略必须读取 HTTP 内容，必须限定响应体最大字节数并纳入流量选项和安全评审。
- DNS/连接失败不自动循环重试；用户手动重试需再次确认/可见。不同候选使用缓存时必须遵循 DNS TTL 和检测结果有效期，不以过期缓存伪装实时检测。
- GitHub 稳定版自动检查只获取 release 元数据，不下载安装包；只有用户主动确认更新后才下载，并在下载前显示版本、来源和文件大小，再确认。禁止后台静默下载更新包。
- 可显示当前任务的候选请求数和估算流量；若实现底层字节统计，标注为“估算/应用可见网络字节”，不声称等同运营商计费值。DNS 经操作系统解析器、TLS/网络栈开销和其他应用流量可能不在应用计量范围，跨平台不能保证精确 MB 硬限额。
- 自动识别计量网络只作为提示/默认建议，不能作为省流保证：各操作系统能力不同，无法可靠识别时按用户显式设置执行，并提供明显的“省流模式”开关。检测到计量网络时不自动启动整服务扫描或更新下载。
- 模式的有效候选探测预算按所选范围动态计算为 `min(唯一 hostname 数 × 每 hostname 候选上限, 用户设置的总候选探测上限)`；目录项中的 URL 先提取并规范化 hostname，同一轮重复 hostname 去重，DNS 查询单独计数。任务开始时冻结设置，预算按实际启动的候选 IP 探测扣减。若用户设置导致理论范围不完整，启动前要求确认；确认后明确标记未检测 hostname/项目，不能静默跳过。

##### 用户界面入口与设置内容

1. **主要入口：**设置对话框 → “网络与流量”分类（`features/settings/NetworkPane.tsx`，2026-10-28 已建；本条原先写的 `NetworkUsageSettings.tsx` 是实现时按页内命名收敛的同一个文件）。分类名称应直白、可搜索，不藏在高级/开发者页面。对话框的入口是侧栏底部的「⚙ 设置中心」（`components/layout/SettingsEntry.tsx`），与原型第 756–758 行同一位置。
2. **快速入口：**检测按钮旁的紧凑流量状态标签显示当前档位和基于当前选择数估算的有效上限；无选择时显示每域名候选上限。启动时按实际目标范围重新计算并冻结预算；108 是可配置总上限，不是每轮固定用量。点击标签可跳转“网络与流量”。每个服务/范围的详情高度与方法选择区仍保持一行，不额外增加纵向空间。**现状与这一条有一处对不上，待裁决**：原型这里是一个只读的状态按钮（第 743 行 `省流 · ≤108 候选`，第 1908 行 `onclick = () => openSettings('network')`），点它是打开设置对话框，改档位仍在对话框里；而页面迁移第一屏把工具栏做成了可直接切换档位的下拉框（`components/layout/Toolbar.tsx` 用 `Select` + `profileNote`），没有“跳进网络与流量页”这一步。两种都满足“三档解释常驻可见”，差别在改档位要不要多一次跳转。
3. **三档解释：**省流（每域名最多 2 个 IP、并发 2、关闭自动全量扫描）；标准（8 个 IP、并发 8）；完整检测（最多 16 个 IP、并发仍不超过 8）。选择档位时同步说明对请求数量、等待时间和结果覆盖面的影响；不只显示档位名称。
4. **精细上限：**“每轮候选 IP 探测上限”暂定初始为 108，来源是 54 个菜单项目 × 每项 2 个候选；项目数并非 hostname 数，故该值只作可调预算，不保证完整扫描。目录已抽完并核算过：去重后 65 个 hostname，省流档全量一轮理论值 130，确实高于默认 108。有效预算取该值与范围内唯一 hostname 理论候选数的较小者。预算只计实际启动的候选 IP 探测，DNS 查询单独计数。若上限可能导致范围不完整，开始前要求用户确认；确认后按稳定顺序执行并明确列出未检测 hostname/项目，不静默遗漏。
5. **自动化与更新：**分别显示“自动检测”开关和“在计量网络下下载更新前询问”选项；省流档默认关闭后台自动全量检测，更新下载永远要求用户确认。检测本身由用户按钮发起，不因打开设置而启动。
6. **当前网络状态：**若平台可靠识别到计量网络，显示“可能正在使用计量网络”的提示，不未经确认偷偷更改用户选择；识别能力不可用时显示“无法识别，按当前流量设置执行”。
7. **设置保存：**设置变更显示“保存中/已保存/保存失败”，持久化失败不得显示为已应用；同一轮检测使用启动时的配置快照，修改设置只影响下一轮。
8. **可访问性：**所有选项可键盘操作，提供文字说明和当前值；不能只靠颜色或图标表示省流状态。

**资料依据：**Go 官方 `crypto/tls` 提供可带 context 的握手能力，支持在超时下只验证 TLS；Go `net/http` 的客户端接口包含 HEAD，但协议/服务端行为需要处理响应体关闭，不能将 HEAD 当作可靠的零流量保证。[Go TLS](https://pkg.go.dev/crypto/tls)、[Go HTTP](https://pkg.go.dev/net/http)。Apple `NWPath.isExpensive` 能标识蜂窝或个人热点等高成本路径，但这是 Apple 平台 API；不能据此承诺 Windows/Linux 或所有网络连接都有可靠的计量网络检测。[Apple `NWPath.isExpensive`](https://developer.apple.com/documentation/network/nwpath/isexpensive)。

#### GUI 常见坑与应对

| 风险 | 规划要求 |
|---|---|
| 同一数据在多个 store/组件各存一份，更新后界面互相矛盾 | 先指定权威 owner；用 ID 归一化实体，重复视图只由 selectors 派生 |
| 把 `loading`、`success`、`error` 拆成可冲突布尔值 | 为每项请求/操作使用明确状态机（idle/loading/success/empty/error/cancelled 等），合法状态和转换可测试 |
| 旧请求先发后到，覆盖用户新选择 | query key + request generation/revision 校验；验证切换菜单、重复刷新和快速保存 |
| 事件先于 RPC 返回、重复送达或迟到 | 统一序号/revision guard、幂等 reducer、终态保护；事件与响应不能分别直接写 UI |
| 切页卸载事件监听或重复挂载监听 | 应用级单一订阅服务，保留并调用 Wails `unsubscribe`；开发 StrictMode 和窗口重激活下测试订阅次数 |
| 事件漏掉后 UI 永久落后 | 启动订阅后拉快照；检测 sequence gap/重新连接/焦点恢复时 resync |
| 乐观更新把未成功的系统写入显示成成功 | 仅轻量 UI 偏好可按需乐观；业务配置和 Hosts 先显示 pending，后端确认后才提交视图 |
| 搜索/筛选/选中/批量计数各自保存 | 只存原始数据与用户输入；筛选结果、计数和状态徽标从同一源派生 |
| 只显示 Toast 或只改变颜色 | 关键结果在原操作附近保持可见文字；通过状态角色让辅助技术可获知，不抢焦点 |
| 错误被当作空数据、加载动画无期限 | 独立的加载、空、失败、过期与重试状态；超时/后端断连有明确错误 |
| 动态方法菜单与硬编码 UI 分叉 | 每次启动/刷新按 scope 从后端取得注册方法；UI 由 descriptor/capabilities 通用渲染，零方法有专门空状态 |
| 保存选择后自动启动检测或旧结果仍能用于应用 | 保存与检测为不同命令；检测结果绑定 `scopeID + strategyID + itemID + timestamp/revision`，选择变更后旧结果不可直接应用 |
| 多 IP 检测后台并发但界面无反馈，或串行等待让用户久等 | 有界并发上限、候选逐项结果事件、真实阶段/进度和 5 秒单项/30 秒服务级总 deadline |
| 用户使用手机流量时无意触发大量请求/文件下载 | 省流/标准/完整检测预设，限制候选与单轮请求数；只握手、不下载正文；无自动重试/静默更新下载；计量状态不确定时不假设无限流量 |

#### 外部资料与采用原则

- React 官方建议避免矛盾、冗余和重复状态；共享状态放在最近共同所有者，派生值在渲染时计算。因此此规划采用单一 owner、稳定 ID 和 selector 派生，避免把同一业务记录复制到多个 store。[Choosing the State Structure](https://react.dev/learn/choosing-the-state-structure)、[Sharing State Between Components](https://react.dev/learn/sharing-state-between-components)
- React 官方把 Effect 用于与外部系统同步，并要求订阅/连接提供清理；用户点击引发的副作用应由事件 action 处理。因此前端将 Wails 事件订阅放在应用级 API 层并明确取消，不用 Effect 镜像派生状态或隐式重复提交命令。[You Might Not Need an Effect](https://react.dev/learn/you-might-not-need-an-effect)、[Synchronizing with Effects](https://react.dev/learn/synchronizing-with-effects)
- Wails v3.0.0 Events API 文档说明前端监听和取消订阅；实现按锁定版本的 API 和生成结果做契约测试，不把事件当作持久队列。[Wails Events API](https://v3.wails.io/reference/events/)
- WCAG 2.2 要求不抢焦点的状态消息可被辅助技术程序化识别、键盘可操作；普通文字对比度至少 4.5:1，大号文字至少 3:1。实现与验收据此覆盖状态播报、键盘路径和色彩对比。[Status Messages](https://www.w3.org/WAI/WCAG22/Understanding/status-messages.html)、[Keyboard](https://www.w3.org/WAI/WCAG22/Understanding/keyboard.html)、[Contrast (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html)

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

Wails v3.0.0 的生命周期、绑定和事件 API 以锁定版本的生成结果为准；后续版本可能改变生成目录、配置字段和打包方式，先建立最小样例并验证后再铺开。

**已批准的首发矩阵：**

| 平台 | 最低版本 | 架构 | WebView / 构建要求 |
|---|---|---|---|
| Windows | Windows 10 | x64 | WebView2 |
| macOS | macOS 13 | x64、arm64 | WKWebView |
| Linux | Debian 13 | x64、arm64 | 默认 GTK4/WebKitGTK 6.0 构建，在 Ubuntu 26.04 构建宿主（或等效容器）上编译，不加 `-tags gtk3` |
| Linux | Ubuntu 26.04 LTS | x64、arm64 | 同一套 GTK4/WebKitGTK 6.0 产物，就是上面那台 Ubuntu 26.04 宿主构建 |

未在目标系统/架构实测的构建不标记为已验证。**两个发行版已先后移出发布目标**：Debian 12（2026-10-27 决定）——它的仓库里没有 `webkitgtk-6.0`，只能走 `-tags gtk3` 的 GTK3/WebKit2GTK 4.1 老后端；Ubuntu 24.04 LTS（同日稍后决定）——用户改把 Ubuntu 线定在 26.04 LTS，不再为 24.04 出包。Linux 只有一条发布线：默认 GTK4/WebKitGTK 6.0，构建宿主固定 **Ubuntu 26.04**（本机即该版本，容器镜像 `ubuntu:26.04` 等价），依赖 `libgtk-4-dev libwebkitgtk-6.0-dev`。

产物能不能装某台机器，按**两道各自实测的门**判，不按「在哪台机器编译」推断：

- **门一：glibc 符号版本。** 用 `objdump -T` 查产物要求的最高 `GLIBC_x.y`，必须不高于目标发行版的 glibc。两个最低目标里较小的一档是 Debian 13 的 2.41（Ubuntu 26.04 是 2.43），所以发布门槛为 `GLIBC_2.41`。CI 强制跑这条检查，不达标就不得发布该目标。
- **门二：运行期库文件。** 产物链接的 `libwebkitgtk-6.0.so.4` / `libgtk-4.so.1` / `libjavascriptcoregtk-6.0.so.1` 必须在目标发行版仓库里存在。Debian 13 与 Ubuntu 26.04 都有；Debian 12 没有，这正是它出局的原因。

**一处自判翻转：**上一版写「本机 Ubuntu 26.04 编出的产物只算开发调试，发布产物一律由 Debian 13 宿主出」。这句话当时的前提是 Ubuntu 24.04 还在目标里——24.04 仓库里的 GTK4 只有 4.14，比构建用的 4.22 低整整八个小版本，怕的是产物引用了新符号在目标上取不到；24.04 一出局，剩下的最低目标 Debian 13 有 GTK 4.18.6 / WebKitGTK 2.54.0，实测反而站在反面：Ubuntu 26.04 宿主产物要求 `GLIBC_2.38`，低于新门槛 `GLIBC_2.41`；放进 Debian 13 容器 `ldd -r` 未解析计数为 0，`libgtk-4.so.1`、`libwebkitgtk-6.0.so.4`、`libjavascriptcoregtk-6.0.so.1` 三条都解析到真实路径。所以**一台 Ubuntu 26.04 宿主可以同时供 Debian 13 与 Ubuntu 26.04 两条目标线**，不再需要第二台低版本构建宿主。留着的口子写清楚：`ldd -r` 只证明符号接得上，不证明跑得好——Debian 13 的 GTK/WebKit 版本与构建用的不是同一套，界面渲染、托盘和退出仍要在 Debian 13 实机走查一遍才算验完。

2026-10-27 容器实测（同一份代码，`go build -trimpath`，Go 1.27.3 + Wails v3.0.0）：Debian 13（glibc 2.41）默认 GTK4 构建成功，产物最高只要求 `GLIBC_2.34`；Ubuntu 24.04（glibc 2.39）当时同样成功、同样要求 `GLIBC_2.34`，现已随该发行版出局而不再复测；本机 Ubuntu 26.04（glibc 2.43）与其等效容器 `ubuntu:26.04` 的默认 GTK4 都编过，两边产物同样要求 `GLIBC_2.38`。作为 Debian 12 的出局依据：它的默认 GTK4 构建直接失败于 pkg-config 找不到 `webkitgtk-6.0`，只有 `-tags gtk3` 能编过。

**这套门槛的实际余量：**Ubuntu 26.04 宿主产物要求 2.38，门槛 2.41，差三档；Debian 13 宿主产物要求 2.34，差七档。门一仍不太会卡住，真正决定支持面的是门二的库面。`-tags gtk3` 保留为开发者本机备选（例如在无 WebKitGTK 6.0 的环境里临时编译验证），不是受支持的发布路径，CI 不为它出包，出问题不承诺修复。

**兼容性规则：**

- CI 构建矩阵按上表执行；Linux 只有一条 GTK4/WebKitGTK 6.0 产物线，x64 与 arm64 各自构建和验收，缺任意一项不得宣称该架构已支持。构建宿主为 Ubuntu 26.04（或等效容器），构建前装 `libgtk-4-dev libwebkitgtk-6.0-dev`，构建命令用默认后端，不加 `-tags gtk3`。每个 Linux 产物发布前必须跑 `objdump -T` 取最高 `GLIBC_x.y` 并与 `GLIBC_2.41` 比对，同时用 `ldd` 确认产物链接到的 `libwebkitgtk-6.0.so.4` / `libgtk-4.so.1` 在目标发行版仓库中存在；两项任一不过就不出该目标的包。CI 不为 `-tags gtk3` 出包。不因 Wails 能构建其他平台/架构就自动承诺支持。**落地状态**：上述判据已写成 `.github/workflows/ci.yml`，当前只覆盖 Linux x64 一条线（Ubuntu 26.04 宿主出包 + 门一 + 本发行版门二 + Debian 13 复核门二）与前端构建、后端 `gofmt`/`vet`/`test`；arm64、Windows、macOS 的 job 待匹配 runner 后补，补齐前这些目标不算进门禁。
- 记录各系统支持的 WebView 基线：Windows WebView2、macOS WKWebView、Linux 为 WebKitGTK 6.0（Debian 13 与 Ubuntu 26.04）；字体、DPI、滚动条、原生窗口尺寸和弹窗层级在每个目标逐一实测。
- 只使用已验证的 WebView/CSS 能力；不要依赖系统默认按钮、输入框、字体和滚动行为。
- 核心流程支持键盘操作和可见焦点；系统字体不可用时配置合理的跨平台回退字体。
- 平台相关代码有对应平台的编译检查和测试；系统命令通过白名单参数调用，不拼接任意输入。
- 文件路径使用标准库处理；Hosts 文件先备份、再原子替换、再验证。
- 临时文件、权限不足、DNS 服务差异和文件占用都返回明确错误。
- 后端任务使用 `context.Context` 支持取消和关闭时清理。
- Wails 绑定错误和版本升级保持兼容；破坏性 DTO 变更要同步更新前端和契约测试。
- 依赖版本锁定，升级前检查变更说明、跨平台构建和回归测试。

#### Wails v3 最小纵向验证

Wails v3.0.0 是稳定版，提供生成绑定、事件 API 和系统托盘能力。样例已从临时验证搬进正式工程：后端 `internal/app`（只暴露 `RuntimeInfo` 与 `Ping`）+ `internal/event`（事件名与自增序号的唯一主人）、入口 `main.go`（托盘显示/隐藏/退出，退出时取消订阅并停发事件）、前端 `frontend/src/App.tsx`（加载/成功/失败三态，保存并调用 unsubscribe，重复序号丢弃。这块自检面板已在 2026-10-27 页面迁移中删除，同样三条做法现在在正式入口 `frontend/src/app/App.tsx` 与 `frontend/src/api/events.ts`）。2026-10-27 在本机 Ubuntu 26.04 x64（Wayland，GTK 4.22.4 / WebKitGTK 2.52.6）实测：WebView 真实请求 `/`、JS 与 CSS；`RuntimeInfo` 返回真实运行环境；空令牌调用回 `empty_token: 令牌不能为空`（`/wails/runtime` 返回 422）；前端 `Events.On` 收到后端事件并回传 `evt:3:running:true`；退出清理确实执行（用临时探针注入验证，验完已 diff 还原，仓库内不留调试代码）。此记录只覆盖本机 Ubuntu 26.04 x64 的默认 GTK4 路径；`-tags gtk3` 只算本机编译通过且已不是发布目标；本机没有截图工具，界面排版、托盘图标与菜单点击无人眼走查；StrictMode 双挂载只在代码里做了请求号作废，未实跑 `wails3 dev`；关闭窗口仍按 Wails 默认＝退出进程（关到托盘属阶段二）。Windows、macOS 和 arm64 都未验证。

2026-10-27 已在正式工程上补测 Linux 构建链：Debian 13 与 Ubuntu 24.04 两个容器的默认 GTK4 构建通过，新宿主 Ubuntu 26.04 的同一构建先在本机（同一版本）实测通过、同日稍后在等效容器 `ubuntu:26.04` 里把 CI 的那串命令原样跑完（`gofmt -l` 空、`go vet`、`go test -count=1`、`go build -trimpath`、门一 2.38 ≤ 2.41、门二未解析计数 0），产物再交给 `debian:13` 容器复核门二，同样未解析计数 0；Debian 12 容器证明默认 GTK4 无法构建，因此退出发布目标（数据见「首发矩阵」一节）。这些是容器/主机的编译与链接实测，不是实机运行验证——Debian 13 的实机启动、托盘和退出清理仍待原生 runner 验证。

同日把这两道门落成了 CI：`.github/workflows/ci.yml` 的 `frontend` 作业跑 `npm ci` 和 `npm run build`（含 tsc）；`backend` 与 `linux-x64` 都在 `ubuntu:26.04` 容器里执行，前者查 `gofmt -l`、`go vet ./...`、`go test -count=1 ./...`，后者用默认后端 `go build -trimpath` 出 x64 产物并跑门一（`objdump -T` 最高 `GLIBC_x.y` ≤ `GLIBC_2.41`）和 Ubuntu 26.04 侧门二（`ldd -r` 无未解析，且 `libgtk-4.so.1`/`libwebkitgtk-6.0.so.4`/`libjavascriptcoregtk-6.0.so.1` 三条都解析到真实路径）；产物再交给 `debian:13` 容器的 `linux-x64-debian13` 装 `libgtk-4-1 libwebkitgtk-6.0-4` 后复核门二。工作流里这几条判据已在同名真容器逐条实测：Debian 13 产物门一 2.34 通过、把门槛压到 2.20 能正确判不过（负向对照）、两个发行版 `ldd -r` 未解析计数为 0；`ubuntu:26.04` 容器产物门一 2.38 通过（同样用 2.34 做过负向对照，正确拦住）、放进 `debian:13` 后 `ldd -r` 未解析计数同样为 0、三条运行库都解析到真实路径。还没被本地复现的只剩 Actions 自己的调度层：容器内 `setup-go` 装 Go 与在线拉模块（本机复测是把宿主 Go 1.27.3 与模块缓存拷进容器、`GOPROXY=off` 跑的），以及 `upload-artifact`/`download-artifact` 的跨作业传递——这几步要等首次真跑确认。arm64、Windows、macOS 还没有 job，缺任意一项就不能宣称该目标已进门禁。

正式工程仍须按锁定版本复核以下契约，并在匹配的原生 runner 上验证批准矩阵中其余系统和架构：

1. Go DTO 生成 TypeScript 绑定，重新生成后差异可审查，生成文件不手改。
2. 前端调用 Go、错误传回前端；Go 推送类型化事件，前端保存并调用 unsubscribe。
3. 窗口关闭/重开和开发 StrictMode 下事件监听不会泄漏或重复。
4. 托盘可创建、更新和清理；窗口关闭行为符合目标平台要求。
5. 已批准的每个构建目标可构建并打包；记录 WebView、签名和系统依赖。

样例和平台验证记录通过后才铺开应用，不因文档示例替代实际版本测试。

#### 特权辅助服务平台验证

主程序保持普通用户权限。以下是阶段四的候选机制，不是已经拍板的跨平台承诺；阶段零先按首发 OS 版本验证安装、授权、IPC、升级和卸载流程，再决定实现。系统权限可能被用户撤销、组织策略限制或升级重置，不能承诺安装时授权后永远不会再需要确认。

| 平台 | 候选机制 | 必须验证 |
|---|---|---|
| Windows | 由安装器注册受限 Windows 服务；主程序通过带用户/会话 ACL 的命名管道通信 | 首次安装提升、服务身份与最小权限、管道 ACL、服务升级/卸载、非授权用户拒绝、服务故障恢复 |
| macOS | 评估 `SMAppService` 注册随应用提供的 LaunchDaemon helper；按最低 macOS 版本确认系统批准流程 | helper 签名/公证、用户批准与系统设置状态、root 服务 IPC ACL、应用升级/移动/卸载、批准撤销后的清晰恢复提示 |
| Linux | 在明确的首发发行版上评估 systemd system service + polkit 授权；避免让 GUI 以 root 运行 | polkit 身份/动作授权、D-Bus 或本地 IPC ACL、服务安装包、发行版差异、授权缓存/撤销与服务升级 |

所有平台共用 `internal/privilege/` 定义的版本化 IPC 和结构化白名单请求。helper 只接受经校验的管理区段操作与恢复操作；不接收任意命令、任意路径或任意文件正文。请求按稳定项目 ID、规范化 hostname、合法 IP 和操作类型校验。每个平台验证都覆盖越权用户、伪造/重放请求、主程序退出、helper 中断和磁盘写失败；任一检查失败都不得写 Hosts。

候选机制的官方资料：[Wails v3.0.0 发布说明](https://github.com/wailsapp/wails/releases/tag/v3.0.0)、[Apple `SMAppService`](https://developer.apple.com/documentation/servicemanagement/smappservice)、[Windows 服务](https://learn.microsoft.com/en-us/windows/win32/services/services)、[Windows 命名管道安全](https://learn.microsoft.com/en-us/windows/win32/ipc/named-pipe-security-and-access-rights)、[polkit](https://polkit.pages.freedesktop.org/polkit/)、[systemd service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)。这些资料用于验证候选方案，不替代目标系统实测。

#### DNS 与隐私边界

- `detector/resolver.go` 首期使用 Go 系统解析器，遵循操作系统的 DNS、VPN、分流和本机网络配置；不内置公共 DNS 地址，不默认使用 DoH 或第三方解析 API。
- 使用系统解析器不代表 DNS 查询保密：操作系统配置的 DNS 服务、VPN 或网络管理员仍可能看到查询。应用只承诺不自行增加第三方查询路径，不承诺隐藏网络层 DNS 流量。
- 解析器接收 `context.Context` 和明确 deadline；DNS 查询单独计数，不把解析失败转成空候选或自动换用外部服务。
- 日志不记录完整 hostname、解析响应或用户网络配置；诊断信息只记录错误类别、耗时和请求 ID。
- 测试以注入的假 resolver 验证超时、取消、IPv4/IPv6、错误和候选去重；集成测试确认默认路径遵循系统解析配置。以后增加外部 resolver 时须先做隐私评估和用户可见选择。

#### Hosts 写入契约

任何真实系统写入前，格式和冲突行为固定如下，并以版本化测试保护：

- 只管理一个区段，使用 `# BEGIN github-net managed hosts v1` 与 `# END github-net managed hosts v1` 两个固定标记。标记外的用户行必须原样保留；文件无标记时只追加区段。
- 标记重复、嵌套、缺一端、顺序错误或版本未知时拒绝写入；不得猜测修复或删除用户数据。
- 区段只包含校验通过的 IP/hostname 记录，按规范化 hostname 和 IP 稳定排序并去重；不接受任意注释或用户提供的原始行。
- 在 `backups/hosts/` 的同一恢复元数据中保存原始文件事务快照、最后一次成功写入的管理区段摘要，以及每个活动项目的应用前工具映射/所有权快照；不另存重复的 Hosts 明文副本。每次验证成功后更新摘要，重启后仍可还原并识别用户改动。
- 区段与已保存摘要不一致、存在工具标记但无有效恢复点/摘要时，视为冲突，停止覆盖并提示用户处理；重新检测或切换方法不能静默抹掉改动。
- `RestoreItem(itemID)` 正常还原的是**该项目应用前的工具管理映射状态**，不是恢复整个 Hosts 原文，也不是清空整个工具区段。该项目原先没有工具映射时，移除本次新增映射；原先有映射时，恢复原映射。区段外用户行始终保留。
- 同一 hostname/IP 可由多个项目共用；区段记录带稳定项目所有者集合。单项还原只释放该项目所有权，其他活动项目仍引用时保留该记录；最后一个所有者还原后才恢复此前映射或移除新映射。
- `RestoreAll` 逐项目执行上述还原语义；有冲突的项目单独失败并保留现场，其他无冲突项目可继续，最终明确报告成功/失败清单。原文件备份只用于失败事务恢复，不得整文件覆盖用户之后的修改。
- 单项还原成功后任务标记“已恢复”，该项目当前不再显示还原入口；下一次成功应用生成新的应用前快照并重新显示入口。已恢复的队列记录仍可单独从任务队列清理。
- 写入前保存事务恢复点；临时文件与目标文件位于同一文件系统，保留必要的权限属性，原子替换后重新读取并校验。只有目标仍匹配本次事务的预期版本时才执行回滚。
- 备份损坏、哈希不匹配、替换失败或恢复校验失败时明确报错并保留现场；不删除最后有效的恢复点。

所有工具管理记录均成功还原且验证通过后，才可清除不再需要的恢复元数据。实现契约前只允许在临时 Hosts 文件和模拟 helper 上开发、测试，不触碰真实系统 Hosts。

#### 分类树焦点跟随

- 保留“检测时自动展开当前项目”和“应用时自动展开当前项目”两个独立持久设置，均默认关闭；避免使用容易和键盘导航混淆的“键盘焦点”表述。
- 开启检测跟随时，服务/批量检测按当前项目顺序临时展开该项目所在分类，并高亮当前项目；应用跟随同理。关闭时不自动收起或展开分类，仅更新当前项目状态高亮。
- 开始任务时保存当时的用户展开状态。跟随只产生临时显示覆盖，不改写该基线，也不移动 DOM 焦点或键盘焦点。
- 任务成功、失败、取消、超时或被关闭时清除临时覆盖并恢复基线；若用户在任务期间手动切换分类，保存其最新选择，结束时不得用旧快照覆盖。
- 同时存在多个任务时只跟随队列当前实际执行的项目；任务切换时更新高亮/展开目标，避免多个并发项目争夺树焦点。

### 7. 测试与故障定位

目录让每个功能的测试和实现相邻，问题可以在对应模块内复现。日志只记录排查所需的信息，不记录密钥、完整 Hosts 文件或不必要的个人数据。

**验证规则：**

- 前端组件测试覆盖正常、加载、空、错误、禁用和键盘操作。
- 状态层测试覆盖事件早于 RPC 响应、重复/乱序事件、过期快照、revision 缺口、请求竞态、重复命令、终态保护、事件重订阅和快照重新同步。
- 同一条 Hosts/任务/配置状态变化后，菜单树、详情、工具栏、队列和计数的投影必须一致；不能只断言单个组件收到一条 event。
- 生命周期集成测试覆盖首次订阅与快照并发、窗口焦点恢复、后端重启/断连恢复、StrictMode 下监听只注册一次及销毁后无残留监听。
- 配置交互测试覆盖保存中、成功、失败、revision 冲突和快速连续选择；失败时不得显示未持久化选择为“已保存”。
- 配置存储测试覆盖首次启动默认但不落盘、版本逐级迁移、非法/损坏 JSON 保留恢复副本、高版本 schema 不被降级覆写、原子保存中断后旧文件仍完整、磁盘写失败 revision 不递增及敏感字段不进入日志。
- 配置默认值与迁移测试必须验证 `detectFocusFollow`、`applyFocusFollow` 两个字段默认均为 `false`，旧配置升级后值稳定且互不覆盖。
- Hosts 应用/还原测试验证幂等请求 ID、重复点击、超时后重试、单项/全部还原、共享 hostname 所有权、外部修改冲突和再次应用建立新恢复点；前端禁用状态之外，后端必须独立验证。
- 焦点跟随测试覆盖检测/应用开关独立生效、结束/取消/失败后恢复视图状态、任务中用户手动切换优先，以及自动行为不移动键盘焦点。
- 队列测试覆盖终态集合与清理按钮一致、终态清理保留等待中/执行中任务、暂停时不再领取新任务但运行中任务继续、重试创建新任务且终态不可回退、清空队列移除等待任务但在存在执行中任务时拒绝。
- IP 探测测试覆盖并发峰值不超过 8、单 IP 2 秒 deadline、单项 5 秒 deadline、服务 30 秒总 deadline、慢 DNS/黑洞 IP/部分完成/取消、候选截断提示和任务总时间上限。
- 流量控制测试验证各档候选与并发硬上限、`min(范围理论候选数, 用户总上限)`预算公式、按实际 IP 探测数扣减、DNS 与 IP 探测分开统计，以及单项/批量/服务/应用前补充检测共用预算；预算不足先确认，取消不启动/不继续应用，确认后超额项显示未检测。
- 默认值测试验证新安装为省流档、总候选探测上限 108、后台自动检测关闭；另验证不自动重试、不读取 HTTP 正文，以及更新元数据检查不会触发安装包下载。
- i18n 测试检查语言文件格式、所有受支持语言的键集合和插值参数；`zh-TW` 还须覆盖全部页面、错误、确认框和辅助技术标签，并通过人工审校后才算正式支持。
- Go 单元测试覆盖业务状态和错误分支；Hosts 测试只操作临时文件。
- 集成测试验证 Wails DTO、事件名称和任务生命周期。
- 可访问性验收覆盖完整键盘操作、可见焦点、不抢焦点的状态播报、错误关联和 WCAG 2.2 AA 最低文字对比度。
- CI 至少执行格式化、静态检查、单元测试和支持平台编译。
- 视觉回归测试覆盖主要页面、主题和常用窗口尺寸；更新基准截图必须经过人工确认。

### 8. 结合新增需求的目录调整结论

**结论：不需要推倒重排前后端目录；现有按功能拆分的主结构仍合适，但要消除重复状态和职责交叉。** Hosts/DNS 策略可作为 `internal/accelerator/strategies/hosts_dns_lookup.go` 加入现有策略结构，不需要为单个加速方法另建顶层目录，也不需要提前建立泛型工具包。该策略此前引用的 CSDN 原始链接未保存在规划中，准确 URL 补回前不将其作为已核实来源。

加速方法的扩展性是额外的硬约束：**给一个已存在的加速范围新增一种方法，只新增该方法的一个 `.go` 实现文件，不改中心注册表、路由、菜单、前端或配置代码。** 后端通过 Go 包内自注册收集方法，不通过运行时扫描目录或 Go 插件发现实现。

| 调整 | 原因 | 归属原则 |
|---|---|---|
| 将 `StrategySelector` 移入 `features/strategies/`，并命名为 `ServiceStrategySelector`；将多策略导向的 `StrategyResults` 改为 `StrategyStatus` | 方法选择是加速业务，不是通用 UI；检测只运行已选择的方法 | `components/ui/` 放通用控件，功能专属的选择、结果和状态留在策略功能 |
| 移除全局 `operationStore`，由 `features/tasks/taskStore.ts` 唯一管理任务状态 | 避免相同队列状态在全局 store 和任务功能里重复保存 | 全局 `stores/` 只存跨功能应用状态及设置 |
| 移除 `themeStore`，由 `settingsStore` 保存主题选择；任务事件只保留 API 订阅层和任务功能映射层 | 避免同一偏好/事件状态有多个来源；通用 hook 不承载任务业务 | `api/events.ts` 管订阅生命周期，`features/tasks/taskEvents.ts` 管事件到任务状态的转换 |
| 明确 `internal/event`、`internal/privilege` 和 `internal/platform` 的职责边界 | 防止事件 DTO、IPC 协议和操作系统服务管理重复定义 | `event` 唯一拥有事件契约；`privilege` 拥有受限 IPC；`platform` 仅实现系统差异和 helper 生命周期 |
| 测试跟随实现文件，按 `format.test.ts`、`taskEvents.test.ts` 等职责命名 | 避免一个大型公共工具测试文件耦合无关功能 | 单元测试相邻放置；跨功能契约和流程测试留在 `test/` |

以上是规划目录的职责收敛，不代表这些源码文件已经实现。实施时仅创建实际阶段需要的文件；若真实代码出现新的重复调用或边界需求，再根据调用关系调整，不预先增加空包和抽象层。

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
├── cmd/
│   └── privileged-helper/
│       └── main.go
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
│       │   │   ├── SettingsEntry.tsx
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
│       │       ├── Segmented.tsx
│       │       ├── StatusBadge.tsx
│       │       ├── SettingsSection.tsx
│       │       ├── Table.tsx
│       │       ├── Toast.tsx
│       │       └── Tooltip.tsx
│       ├── features/
│       │   ├── strategies/
│       │   │   ├── ServiceStrategySelector.tsx
│       │   │   ├── ServiceStrategySelector.module.css
│       │   │   ├── StrategyStatus.tsx
│       │   │   ├── StrategyStatus.module.css
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
│       │   │   ├── taskEvents.ts
│       │   │   └── taskEvents.test.ts
│       │   ├── settings/
│       │   │   ├── SettingsDialog.tsx
│       │   │   ├── SettingsDialog.module.css
│       │   │   ├── LayoutPane.tsx
│       │   │   ├── NetworkPane.tsx
│       │   │   ├── profileOptions.ts
│       │   │   ├── SettingsNavigation.tsx
│       │   │   ├── AppearanceSettings.tsx
│       │   │   └── StartupSettings.tsx
│       │   ├── theme/
│       │   │   ├── ThemePicker.tsx
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
│       │   ├── useDebouncedValue.ts
│       │   └── useSettingsSave.ts
│       ├── stores/
│       │   ├── appStore.ts
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
│       │   ├── format.test.ts
│       │   ├── validation.ts
│       │   ├── validation.test.ts
│       │   ├── safeText.ts
│       │   └── safeText.test.ts
│       ├── test/
│       │   ├── setup.ts
│       │   ├── localeCompleteness.test.ts
│       │   └── apiContract.test.ts
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
│   │   ├── service.go
│   │   ├── service_test.go
│   │   └── strategies/
│   │       ├── registry.go
│   │       ├── contract.go
│   │       ├── hosts_dns_lookup.go
│   │       └── hosts_dns_lookup_test.go
│   ├── catalog/
│   │   ├── catalog.go
│   │   ├── catalog.json
│   │   └── catalog_test.go
│   ├── config/
│   │   ├── config.go
│   │   ├── defaults.go
│   │   ├── validate.go
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
│   ├── privilege/
│   │   ├── client.go
│   │   ├── protocol.go
│   │   ├── installer.go
│   │   └── privilege_test.go
│   ├── platform/
│   │   ├── platform.go
│   │   ├── permission_windows.go
│   │   ├── permission_darwin.go
│   │   ├── permission_linux.go
│   │   ├── helper_service_windows.go
│   │   ├── helper_service_darwin.go
│   │   ├── helper_service_linux.go
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
- `components/ui/Segmented.tsx`（2026-10-28 已建）：互斥预设按钮组，选中态用 `aria-pressed`；`onChange` 可缺省以支持整组禁用，禁用原因用同一段说明文字渲染。样式取自原型 `.seg`（第 430–434 行）。
- `features/strategies/ServiceStrategySelector.tsx`：放在当前服务详情区，用“GitHub 全部加速方法”等直白名称显示并选择该服务以后默认使用的方法；切换后持久保存，后续检测自动使用此方法，不因切换而自动开始检测。它是业务组件，不放入通用 `components/ui/`。
- `OperationFeedback.tsx`：统一显示操作阶段、进度、耗时、取消和最终结果。
- `InlineStatus.tsx`：在表单或按钮附近显示保存中、成功、错误和重试提示。
- `*.module.css`：页面和组件的局部样式；颜色、字号、间距仍引用统一令牌，不重复定义视觉规范。
- `features/`：按业务功能组织页面、状态、类型和事件处理。
- `features/strategies/`：服务方法选择和方法检测状态的业务组件；`strategyStore.ts` 只管理方法列表及临时检测结果，不保存用户偏好，默认方法以 `settingsStore` 从后端读取的配置为唯一来源。方法列表动态来自后端自注册目录；零方法时显示空状态并禁用检测/应用。检测结果只对应用户当前选择的方法，不是多策略比较列表。
- `features/settings/`：设置中心对话框本体与分页内容；共享设置状态的落点见本节末尾 `stores/settingsStore.ts` 的现状说明。业务设置与需持久化的界面偏好都经后端配置 API 保存；前端不维护 `localStorage` 副本。
- `features/settings/SettingsDialog.tsx`（2026-10-28 已建）：设置中心模态框。导航只列已真正实现的页——布局、网络与流量两组两项，原型另外六页（界面缩放、外观、托盘、开机自启动、检测升级、关于我们）没有放假入口；副标题随页切换，取自原型 `PANE_SUBTITLES`（第 1810–1816 行）。Esc 监听挂在 `document` 上（焦点没进对话框时键盘也能退出），打开时初始焦点落在「关闭」按钮，点遮罩同样关闭；因为页内每项都即时保存，页脚只有「关闭」。导航列表暂时留在本文件里，没有单独拆成规划中的 `SettingsNavigation.tsx`——它只有一处调用，等出现第二个需要同一份导航的界面再拆。这些行为都在真 WebView 里逐项实测过，见技术规划阶段二对应条目。
- `features/settings/LayoutPane.tsx`（2026-10-28 已建，即本节原先规划的 `LayoutSettings.tsx`）：布局页；侧栏宽度 240/340/480 三档预设即时保存并立刻改变界面宽度。队列高度三档预设按原型原样呈现但整组禁用，并写明「任务队列面板还没迁进界面」，不假装可调。原型同页的「检测与应用 → 焦点跟随」两个开关本轮没搬：后端配置字段已在，但界面还没有检测/应用执行过程可跟随。
- `features/settings/NetworkPane.tsx`（2026-10-28 已建；即本节原先规划的 `NetworkUsageSettings.tsx`，实现时按页内命名收敛为 `NetworkPane`）：网络与流量页；三张档位卡（`aria-pressed` 表选中态）与每轮候选上限输入框，数字唯一来源是 `profileOptions.ts`，预算预览用后端目录的真实主机名数（65）算「一轮全量需要多少次、还差多少盖不住」。输入框先在前端挡非整数与越界并显示 `aria-invalid`＋原因，**不发给后端**，所以 revision 不动；合法值才提交。设置仍由后端 config 权威保存。
- `components/layout/SettingsEntry.tsx` + `AppShell.tsx` 的 `sidebarFooter` 槽（2026-10-28 已建）：侧栏底部「⚙ 设置中心」入口。`AppShell` 因此把侧栏改成「树滚动区 + 固定底部」两段（`.treeScroll`/`.sideFoot`），照搬原型第 125–133 行的 `.sidebar`/`.tree-scroll`/`.sidebar-footer`/`.sf-btn`；原型底部那另一个「重算演示状态」按钮属于演示逻辑，仍然不迁（见阶段二第一屏）。入口属于布局骨架，不放进 `CategoryTree`。
- 检测按钮附近显示当前档位和本轮请求预算，并提供紧凑的快速切换入口；它与详情顶部加速方法选择保持同一行的既有布局约束，不增加详情栏高度。完整设置仍从设置中心进入。
- `features/theme/`：主题列表和选择界面；主题选择读取并更新 `settingsStore`，不另建 `themeStore`。
- `api/client.ts`：前端唯一的后端调用入口；业务组件不直接使用 Wails 运行时 API。公共方法集中在此文件，禁止业务组件自行拼接调用。
- `api/errors.ts`：把后端错误转换为稳定错误码和可展示消息；不能把失败伪装成成功或空数据。
- `api/events.ts`：提供任务事件订阅并返回清理函数；页面退出时取消订阅。
- `api/generated/`：Wails 生成的绑定文件。目录内的 README 说明生成方式；实际文件以锁定版本模板为准，禁止手工编辑。
- `i18n/messages/*.json`：每种语言一个完整资源文件。语言发现、回退和校验由 `localeDiscovery.ts`、`index.ts` 与测试负责。
- `styles/tokens.css`：唯一的颜色、间距、字号、圆角、阴影和层级源头。
- `styles/themes.css`：主题令牌映射；不能由功能页面另写一套主题颜色。
- `styles/layers.css`：声明 reset、token、公共组件和功能样式的 CSS 层级，避免样式加载顺序导致覆盖结果不同。
- `stores/`：只放确实跨多个功能共享的应用状态。队列任务状态只由 `features/tasks/taskStore.ts` 管理，禁止同时维护 `operationStore` 等第二份任务 store。
- `stores/appStore.ts`：应用启动状态及 `useAppStatus` 使用的状态源；hook 只提供 React 订阅方式，不另存一份状态。
- `stores/settingsStore.ts`：所有持久配置的前端快照、编辑草稿、保存状态和 revision 冲突处理。业务设置与界面偏好统一通过后端配置 API 读写；组件不得另存 `localStorage` 副本。现状（2026-10-28）：文件还没建。配置快照现在只有 `app/App.tsx` 一个持有者，保存通道与三态、冲突后重读快照都收在 `hooks/useSettingsSave.ts`；等出现第二个需要共享同一份配置快照的功能（任务队列、检测区）再落成 store，避免同一份配置有两个来源。
- `features/tasks/taskEvents.ts`：把 API 层收到的任务事件转换为 task store 更新；事件订阅的创建和清理由 `api/events.ts` 提供的订阅函数负责，不再额外建立通用 `useTaskEvents` hook。
- `hooks/useDebouncedValue.ts`：仅供搜索等高频输入使用；卸载时清理计时器，不用于掩盖慢操作。
- `hooks/useSettingsSave.ts`（2026-10-28 已建）：所有设置写入共用的通道——提交带 `expectedRevision` 与 `mutationID` 的 patch、只认最后一次请求的结果、维护「保存中 / 已保存（第几版·改的哪一项）/ 保存失败」三态、失败后重读后端快照。它只管保存这件事，不持有配置快照，也不承载任务业务。
- `hooks/`：只保留多个功能确实共用的 React 生命周期或输入行为 hook；任务事件的业务映射留在 `features/tasks/`。
- `utils/format.ts`：集中格式化延迟、速度、文件大小和时间，使用明确单位及 `Intl`。
- `utils/validation.ts`：集中前端表单格式和范围校验；后端仍是最终校验方。
- `utils/safeText.ts`：只校验动态外链的协议和可信主机；普通文字使用 React 默认转义，不配合 `dangerouslySetInnerHTML`。
- `i18n/index.ts`、`i18n/formatters.ts`：统一文案查找、命名参数、回退和本地化日期数字格式。
- `features/*/*Selectors.ts`：仅放同一功能中被多个组件复用的纯查询逻辑，不跨功能建万能 selector。
- `styles/tokens.css`：全站唯一颜色、间距、字号、层级和焦点令牌来源。
- `test/apiContract.test.ts`：验证前端 API 封装的参数、返回类型和错误传递，Wails 绑定升级时用于尽早发现接口变化。
- `test/`：跨功能的前端设置和资源完整性测试；具体组件测试与组件文件相邻，采用 `*.test.tsx` 命名。

### 3. 后端

- 根目录 `main.go`：创建 Wails 应用、装配服务、配置关闭清理。
- `internal/app/`：Wails 暴露给前端的唯一服务门面、DTO 和错误转换；不放系统操作细节。
- 应用每次启动都创建空的内存任务队列；不读取或创建队列历史文件。
- `internal/accelerator/`：加速流程协调、按范围读取用户选择的方法，以及检测、应用路由；不维护需要每次新增方法时编辑的手工清单。
- `internal/accelerator/strategies/contract.go`、`registry.go`：策略元数据与能力契约、包内自注册表、注册错误收集和排序后的只读查询。该包被应用装配层固定导入一次；新增同包 `.go` 文件会随正常 Go 构建自动编译。
- `internal/accelerator/strategies/hosts_dns_lookup.go`：首期策略；文件内声明稳定方法 ID、目标加速范围、用户文案、能力和实现，通过 `init` 注册；默认使用系统 DNS 配置取得候选 IP，保留目标 hostname 进行 TLS 证书校验，并调用共享 Hosts 服务应用。不得自动把查询发给第三方解析器。
- `internal/accelerator/strategies/hosts_dns_lookup_test.go`：使用模拟解析器、模拟网络和临时 Hosts 文件测试该策略，不访问真实 Hosts。
- `internal/catalog/`：分类、域名等产品数据的唯一来源；阶段二从唯一正式原型抽取初始 `catalog.json`，前端不得再维护另一份列表。
- `internal/config/`：默认配置、校验、版本迁移和原子保存；按稳定 `scopeID` 保存选择的方法 ID，不保存易过期的检测结果作为可直接应用的数据。
- `config.Config` 中的 `defaultStrategyByScope` 用稳定加速范围 ID 映射用户选择的方法 ID。当前若多个二级菜单共享服务选择，它们映射到同一范围 ID；以后需要某菜单独立选择时，由产品目录定义独立范围。保存的方法不存在或不适用时不得静默切换到另一个方法，须清除有效选择并提示用户重新选择。
- `internal/detector/`：DNS 查询、候选探测和评分；每个请求可取消并有超时。
- `internal/hosts/`：Hosts 文件解析、备份、写入事务、验证和恢复；不负责选择系统路径或弹系统授权框。管理区段格式与冲突策略在真实写入前冻结，详见“Hosts 写入契约”。
- `internal/privilege/`：普通权限主程序与受限特权辅助服务之间的固定 IPC 协议、客户端和安装器；不接受任意命令、任意路径或任意文件内容。
- `internal/platform/`：操作系统差异适配，包括系统路径、DNS 刷新、打开文件、权限查询及辅助服务的系统级安装/启动/状态检查。特权 IPC 的协议、客户端请求校验和 Hosts 操作白名单由 `internal/privilege/` 唯一负责；平台实现不得再定义一套 IPC 协议。文件后缀使用 Go 识别的 `windows`、`darwin`、`linux`。
- `cmd/privileged-helper/main.go`：独立的受限特权辅助服务入口，只接收固定 IPC 请求；不运行任意命令、不接受任意路径或脚本参数。
- 特权辅助服务按阶段四平台验证结果安装并复用。目标是避免每次 Hosts 操作都要求用户重复授权，但系统撤销授权、组织策略或服务损坏时必须遵守平台授权流程并明确提示；不得绕过授权。主界面始终以普通用户身份运行，辅助服务仅执行白名单内的 Hosts 操作。
- 如果辅助服务被用户移除、系统重置授权或服务文件损坏，停止相关操作并明确提示修复方式；不得在 GUI 中静默提权。
- `internal/task/`：任务生命周期、状态转换、取消和进度；通过事件发布器报告变化。
- `internal/event/`：统一事件名称、数据结构和限频发布，是事件契约的唯一来源；任务包通过注入的发布器发送事件，不重复定义 `TaskProgress` 等事件 DTO。
- `internal/updater/`：查询 GitHub 最新稳定版、比较语义化版本、验证签名清单、下载摘要和平台签名；未通过完整校验不得交给安装流程。
- `internal/logging/`：统一日志格式和敏感信息清理。
- `test/integration/`：跨包流程测试，只使用临时目录和模拟平台实现。

#### Hosts 写入契约

在任何真实系统写入前，格式和冲突行为固定如下，并以版本化测试保护：

- 只管理一个工具区段，使用固定标记 `# BEGIN github-net managed hosts v1` 与 `# END github-net managed hosts v1`。标记外的用户行必须原样保留；文件不存在标记时只追加区段。
- 存在重复、嵌套、缺一端或版本未知的标记时，视为文件冲突并拒绝写入；不得猜测修复或删除用户数据。
- 记录按规范化 hostname 与 IP 生成，禁止插入注释外的任意文本；区段排序稳定，同一 hostname/IP 按所有项目所有权去重。
- 在 `backups/hosts/` 保存首次写入前的原始文件事务快照、最后一次成功写入的管理区段摘要，以及每个活动项目的应用前工具映射/所有权快照。更新不得覆盖原始事务快照；临时文件与 Hosts 位于同一文件系统，保留必要权限属性，替换后重新读取并校验。
- 若当前管理区段与最后一次成功写入的摘要不一致，或存在工具标记但元数据缺失/损坏，视为冲突并停止；不得用重新检测静默抹掉改动。
- `RestoreItem(itemID)` 还原该项目应用前的工具管理映射，不恢复整份 Hosts 文件，也不清空整个工具区段。项目原先没有工具映射时，移除本次新增映射；原先有映射时，恢复原映射。区段外用户行始终保留。
- 共用的映射带项目所有者集合。单项还原只释放该项目所有权；其他活动项目仍引用时保留映射，最后一个所有者还原后才恢复此前映射或移除新映射。
- `RestoreAll` 逐项目执行上述语义；冲突项目单独失败并保留现场，无冲突项目可继续，最终明确返回成功/失败清单。原始快照只用于事务失败回滚，不得整文件覆盖用户后续更改。
- 事务中途失败时，仅当目标文件仍匹配本次事务的预期版本才回滚；备份损坏、摘要不匹配、替换失败或恢复校验失败都明确报错并保留现场。
- 所有工具管理记录均成功还原且验证通过后，才可清除不再需要的恢复元数据。

#### 产品目录抽取

`internal/catalog/catalog.json` 由唯一正式原型的一次性抽取生成，不在运行时解析 HTML，也不让前端保留第二份菜单表。原型确认后按以下步骤迁移：

1. 收集服务、分类、稳定 ID、项目标题、目标 hostname、scopeID 和原型显示顺序；记录每条数据来自哪个原型区域。
2. 如果原型字段是 URL，解析后只保存 hostname；校验格式、转小写、去除尾随点、使用一致的 IDNA 规范，并按规范化结果去重。端口、路径、查询参数不创建不同 hostname。
3. 不自动合并稳定 ID 不同但显示名相同的项目；重复 hostname 保留明确的项目关系，并在目录测试中验证预期共享关系。
4. 用单元测试检查 ID 唯一、引用完整、hostname 有效、重复项可解释、GitHub/Steam 范围映射正确；再用原型计数和抽样人工核对。
5. 抽取完成后，前端只从 `GetItems` 加载目录；原型停止作为运行数据源。

原型文件里字符串或链接出现次数不等于目录条目数，不能用文本命中数代替抽取与校验。

### 加速方式如何扩展

- **新增方式的最小改动严格为一个 `.go` 文件**：在 `internal/accelerator/strategies/` 同一 Go package 中新增实现文件；文件描述自己适用的稳定 `scopeID`、唯一 `strategyID`、用户可见名称/说明、能力和实现，并调用包内 `register`。不需要再编辑 `registry.go`、`accelerator/service.go`、前端、Wails DTO/API、配置结构或手写菜单列表。
- `strategies/registry.go` 与 `contract.go` 是一次性基础设施：在应用装配阶段固定依赖该 package 一次。Go 构建会自动编译该 package 目录下符合构建条件的所有 `.go` 文件，包内 `init` 完成自注册；不运行时扫描文件系统、不使用 Go 插件，也不为每个实现添加单独 import。
- 这是“源码加一个文件并重新构建应用”的扩展方式，不是对运行中二进制热加载 `.go` 文件；策略实现使用标准库及项目已有依赖，不为单个方法增加新模块依赖。
- 自注册表只负责收集静态描述，不执行策略操作。注册结果按稳定 ID 排序；重复 ID、空 ID、未知 `scopeID`、无效能力或注册错误必须在应用启动/查询目录时返回明确错误，不能 panic、吞错或随机覆盖。策略实现不得依赖 `internal/accelerator` 根包，避免 import cycle。
- 每个方法文件由描述符提供 factory/实现，公共契约接收请求、`context.Context` 和窄化的共享服务依赖；不得自行创建第二套 DNS、任务、配置、日志或 Hosts 写入实现，也不能暴露任意命令/路径操作。
- 方法绑定到已有的稳定 `scopeID`。同一范围的服务/菜单共享该方法列表和所选方法；要新增方法不改产品目录。**增加一个全新的菜单/加速范围本身**仍须增加目录元数据，这与“给已有菜单增加一种方法”是不同改动。
- 已有范围可以注册零个、一个或多个方法。查询零个方法返回空列表（不是错误）；前端显示“暂无可用加速方法”并禁用检测/应用。一个方法可按既定初始化规则预选；多个方法且无有效保存选择时要求用户明确选择。保存的选择始终按 `scopeID` 持久化，新增方法不得覆盖用户已有选择。
- 公共注册契约测试自动遍历全部自注册方法，检查元数据唯一、范围有效、能力与操作一致、描述可展示、构造成功和错误行为明确；每个复杂策略可再加相邻 `_test.go` 验证其专属算法。**实现接入不强制要求修改中央测试清单**，但上线前必须有足够的算法/平台测试。
- 每种方法只放自身差异逻辑；DNS、连通性探测、任务进度、配置、日志、Hosts 原子写入和回滚继续复用现有公共模块。
- 第一种策略 `hosts_dns_lookup.go` 的设计是查询域名 DNS 地址，再由用户选择候选并写入 Hosts。直接使用解析器接口，不抓取 ipaddress.com 网页，不依赖页面结构或手工粘贴页面内容。检测目标 IP 时保留原主机名并校验 TLS 证书，不关闭证书验证。此前提及的 CSDN 原文 URL 尚未找到；补回前不声称此设计已由该链接核实，也不编造来源。
- 未来可按需新增 `system_proxy.go`、`pac.go` 等单文件策略，并在其描述符中列出适用范围；实现并通过测试前不建立空文件、不自注册为可用、不宣称支持。新增方法不影响其他范围。
- Hosts 记录带应用管理标记和方法 ID。不同方法不能覆盖用户自行维护的行；更改默认方法只影响后续操作，不自动修改或清理旧方法留下的系统设置。

### 按服务检测并选择默认加速方法

1. GitHub 与 Steam 等当前共享选择的服务按稳定 `scopeID` 分别持久保存方法，互不覆盖；同一范围内的二级菜单共用设置。
2. 用户点击“检测 GitHub”或“检测 Steam”后，检测该服务下的所有项目，但每个项目只使用该服务当前保存的默认方法，不自动比较所有方法。
3. 单项、批量和自动检测也按目标所属范围读取已保存的方法；更换方法后，所有后续检测都使用新选择。
4. 更改选择只保存偏好，不自动检测或修改系统。点检测按钮只产生结果，不写 Hosts；应用仍由用户单独发起。
5. 后续应用按目标所属范围选择保存的方法。应用前检查结果未过期并再次验证；已有 Hosts 记录或系统配置的清理由用户单独发起。
6. 不把不同方法的分数硬凑成一个“最佳”值；分别展示连通性、耗时、覆盖范围和能力，交由用户选择。
7. 工具栏不放全局方法下拉框；当前服务详情顶部居中以单行紧凑标签显示有独立底色和边框的选择区，标为“GitHub 全部加速方法”或“Steam 全部加速方法”，并放置对应检测按钮，不增加详情栏高度。
8. 方法名称面向普通用户，说明实际动作，例如“自动查找可用 IP”；DNS、Hosts 等术语放在简短说明中。检测状态单独显示。
9. 只显示已注册并适用于当前范围的方法；无方法时显示明确空状态，未实现方法不做成可选项或虚假的检测结果。

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

1. 选择已有稳定 `scopeID`，在 `internal/accelerator/strategies/` 新增一个 Go 源文件（同一 `package strategies`）。
2. 在该文件中定义唯一 `strategyID`、显示名称/说明、适用范围、支持能力、实现/factory，并用包内 `register` 自注册。
3. 复用注入的 `detector`、`hosts`、`platform`、`task` 等服务；不复制通用实现，不访问任意路径，不执行任意命令。
4. 无需改 `registry.go`、服务路由、菜单目录、配置 schema、Wails API/DTO、前端组件或方法清单；启动时通用校验自动发现该方法。
5. 检测只运行该范围当前保存的方法；更换后后续任务使用新选择。所选方法失败时明确报告，不暗中切换或轮询其他方法。
6. 公共契约测试自动覆盖新注册项；复杂算法另增该方法自己的 `_test.go`。只有实现、错误处理、Hosts 事务和适用平台行为满足验收后，该方法才标记为可用。

### 清空队列与启动行为

1. 队列默认只保存在内存中；应用每次启动都从空队列开始，不生成或读取历史队列文件。
2. “清理已结束”只移除终态任务：成功、跳过、失败、回滚、取消和已恢复；保留排队中与执行中的任务，允许在其他任务运行时清理。按钮启用状态使用与后端相同的终态定义。
3. “清空队列”移除全部任务记录，包括尚未执行的排队任务；存在执行中任务（检测、应用或还原）时拒绝，避免运行中的任务从队列消失。
4. 两种清理都不修改 Hosts、候选检测结果、用户设置或恢复快照。`ClearFinishedTasks` 和 `ClearQueue` 都是内存操作，不存在持久化删除失败。
5. 详情页“查看日志”从 `GetTasks` 快照筛选当前队列中的该项目任务记录；不另存历史文件。清理队列或重启后，已移除记录不可恢复，也不把该入口说成持久审计日志。
6. 若未来需求要求持久化队列，必须先更新规划；启动时删除上次运行的任务记录，清理操作同时清理存储并明确反馈失败。

### 队列控制与项目任务记录

- 暂停队列只停止领取新的等待任务；已经执行的检测、应用或还原任务继续运行。暂停期间新加入的任务保持等待状态；继续队列后按原顺序执行。重复暂停/继续请求幂等成功。
- 重试只接受失败或已回滚的应用任务。后端重新检查项目、方法和候选地址；候选已失效时明确要求重新检测。每次重试创建新任务 ID，并用 `retryOf` 关联原任务，原终态不回退。批量重试逐项返回新任务 ID 或拒绝原因。
- “查看日志”使用 `GetTasks` 快照筛选当前队列中该项目的任务状态、耗时和错误信息。它不是独立审计日志；清理任务或退出应用后，记录不保留。
- `GetTasks` 返回队列快照与 sequence 水位。前端先订阅 `TaskProgress` 再取快照，只应用水位之后的事件；序号缺口、断线重连和窗口恢复时重拉快照。快照失败时明确显示同步错误。

### 配置和运行文件

- 应用专属用户数据根目录按系统规范确定：Windows 使用 `%LOCALAPPDATA%\GitHubNet`，macOS 使用 `~/Library/Application Support/GitHubNet`，Linux 使用 `${XDG_DATA_HOME:-~/.local/share}/github-net`。首期配置文件位于 `config/config.json`；所有需持久保留的用户设置由 Go 配置服务读写，前端 `localStorage` 不保存配置副本。
- 根目录只包含 `config/config.json`、`backups/hosts/`、`logs/` 和 `tmp/`；配置、日志、Hosts 备份和临时文件不得写入仓库、当前工作目录或用户主目录顶层。
- 不创建数据库、队列文件或转储。配置采用带 schema 版本和 revision 的 JSON；为保证原子替换，配置临时文件必须与 `config.json` 位于同一目录，成功后立即删除。其他临时文件才放 `tmp/`；最多保留一个受控配置迁移/损坏隔离备份。
- `backups/hosts/` 最多保留一份当前有效恢复元数据，其中含原始事务快照、区段摘要和活动项目应用前映射/所有权快照；原始快照只用于事务失败回滚。所有工具管理记录均成功还原且验证通过后，才可删除不再需要的元数据；不得删除仍被有效 Hosts 修改引用的记录。
- `logs/` 最多保留当前日志及 4 个轮转文件，每个文件不超过 5 MiB；按大小轮转，启动时清除本应用过期临时文件。
- 临时文件使用应用专属目录和固定命名规则；每次操作结束时删除，启动时只清理本应用遗留文件。
- 保存失败、磁盘空间不足或清理失败必须显示错误；不留下无用途的垃圾文件。

## 六、从原型迁移的顺序

### 阶段零：冻结行为基线和关键决策

1. 已暂停两份 HTML 原型的并行编辑，并核对候选版的省流设置、检测预算、候选操作按钮布局和自定义下拉弹层。已将这些确认项合入 `github-net唯一前端原型.html`；该正式原型是唯一迁移基线。副本冻结留档，不删除，也不继续维护。
2. 验证正式原型的省流默认值、档位、预算、取消、队列状态、候选操作和下拉弹层行为；修复后再进入页面迁移。
3. Go 最低版本为 1.27.3，Wails 锁定 v3.0.0；纵向样例已落进正式工程（`internal/app`、`internal/event`、`main.go`、`frontend/src/App.tsx`），并在本机 Ubuntu 26.04 x64 实测通过绑定调用、错误回传、事件接收与退出清理；本机 GTK4 构建与前端构建也通过。正式工程已在 Debian 13 与 Ubuntu 24.04 两个容器通过默认 GTK4 构建；新的构建宿主 Ubuntu 26.04 先在本机（同一版本）实测通过，等效容器 `ubuntu:26.04` 也已按 CI 的命令逐条跑完（含门一与门二，产物在 `debian:13` 复核门二通过）。Debian 12 经实测无法走默认后端，已于 2026-10-27 退出发布目标，Ubuntu 24.04 同日经用户裁决改由 26.04 LTS 取代。Debian 13 的实机运行、Windows、macOS 及 arm64 仍待原生 runner 验证。
4. 使用已批准矩阵：Windows 10 x64、macOS 13 x64/arm64、Linux x64/arm64 且发行版最低 Debian 13 与 Ubuntu 26.04 LTS；CI 使用匹配的原生 runner。Linux 只有默认 GTK4/WebKitGTK 6.0 一条发布线，统一在 Ubuntu 26.04 构建宿主（或等效容器）编译，不加 `-tags gtk3`；产物兼容性按「两道门」实测判定：`objdump -T` 的最高 glibc 符号版本不高于 `GLIBC_2.41`（Debian 13 的 glibc，两个最低目标里较小的一档），且目标发行版提供产物链接的 `libwebkitgtk-6.0.so.4` 与 `libgtk-4.so.1`。这套判据已落成 `.github/workflows/ci.yml`，当前只覆盖 Linux x64 一条线加前端与后端检查，arm64、Windows、macOS 的 job 待补。
5. 确认系统 DNS 隐私规则、Hosts 管理区契约，以及各平台特权 helper 验证计划。更新器和繁中按各自发布门槛推进，不阻塞只读界面或检测样例。

**门槛：**唯一正式原型、已通过本机 Linux 验证的 Wails 样例和首发平台矩阵均已明确；正式迁移仍需建立工程并由原生 runner 验证其余目标。没有完成 helper 验证的平台不得开放 Hosts 写入。

**基线与开发机的落差：**2026-10-27 起这条落差已经消掉一半——发布门槛是产物最高要求不超过 `GLIBC_2.41`（Debian 13 的 glibc，两个最低目标里较小的一档），而开发机与构建宿主现在是同一个系统（Ubuntu 26.04，glibc 2.43），它编出的产物要求 `GLIBC_2.38`，门一有余量三档，并且实测放进 Debian 13 容器后 `ldd -r` 无未解析符号，所以不再需要「另找一台低版本宿主出包」。剩下的落差是另一件事：Debian 13 目标上的 GTK 是 4.18.6、WebKitGTK 是 2.54.0，与构建用的 4.22.4 / 2.52.6 不同源同版本，链接接得上不等于跑得对，Debian 13 的实机启动、托盘与退出清理仍需人在这台机器上走查一次。判定一律以产物实测为准：同一份代码在 Debian 13 编出的产物要求 `GLIBC_2.34`、在 Ubuntu 26.04 编出的要求 `GLIBC_2.38`，相差四档，说明「在哪台机器编」不能替代「产物要求哪个版本」——所以两道门都要真跑，只是宿主选定为 Ubuntu 26.04 之后，门一和门二可以在同一条 CI 里一次测完。

### 阶段一：目录和只读界面

1. 从唯一正式原型抽取 `catalog.json`，完成稳定 ID、hostname 规范化、共享 hostname 关系和引用完整性测试。不同项目可共享规范化 hostname；每轮检测按 hostname 去重，不合并项目 ID。**已完成（2026-10-27）**：`internal/catalog/`（`catalog.json` + `catalog.go` + `catalog_test.go`）落进工程，11 分类 / 54 项目 / 去重后 65 个 hostname（引用 68 次），三条来自 URL 的原型字段只保留 hostname、`手动映射` 标为用户自填、演示状态 `init` 未迁移；`scopeID` 取分类 ID。三组共用主机名已逐项登记并写成断言，54 个 `protoLine` 由测试回查原型原文。`GetItems` 曾记为「随真实状态来源一起接」，2026-10-27 由用户改判先接：Hosts 状态这一列现在给 `unknown`（本意是「后端还没读过」，不是伪造的「未替换」），等阶段三「Hosts 与特权服务」的读取服务到位再换真值——总纲 TECHNICAL_PLAN 把同一件事编为阶段四，两份文档阶段编号不同，引用时以所在文件为准。
2. 搬入设计令牌和公共 UI 控件，按 `catalog`、详情、设置和队列的顺序迁移页面。
3. 先接入只读后端状态；不把原型模拟数据伪装成真实检测或系统状态。**对外形状、错误格式、`GetItems`、`GetAppStatus`、`GetSettings`/`SaveSettings` 已接线（2026-10-27）**：`internal/app/dto.go` 给出 `CatalogCategory`/`CatalogItem`/`HostsState`/`ItemsSnapshot`/`AppStatus`/`HostsAccess`，`internal/app/errors.go` 给出稳定错误码（`empty_token`、`catalog_invalid`、`internal_error`、`invalid_settings`、`revision_conflict`、`settings_save_failed`、`config_schema_newer`）和带不可见 `cause` 的 `Error`；内部审计字段（原型行号、抽取来源、`schemaVersion`）不进对外 JSON，`HostsStatus` 当前只返回 `unknown`，其余五档等 Hosts 读取服务到位。设置本体在 `internal/config/`，按本文件第六节那份 JSON 走：字段级 patch + `expectedRevision` + `mutationID`、串行写、同目录唯一临时文件同步后原子替换、成功才递增 revision、失败明确返回且不改 revision；文件缺失给默认值不写盘，损坏隔离到 `<路径>.bad` 并在 `configNotice` 里说明，版本比程序新拒读拒改；目录 0700、文件 0600。权限一栏只是只读探测（以写模式打开但不截断、不写字节），Hosts 写入仍在阶段三。前端这一轮先落在自检面板上，2026-10-27 页面迁移后自检面板已删除，同样的数据改由正式页面显示：顶栏权限徽章、状态栏的 Hosts 路径与写权限、DNS 选型、档位、revision 和事件序号、左树的分类/项目/去重主机名数、工具栏档位切换；`unknown` 明写为「后端还没读过系统 Hosts，不是未替换」，档位切换用 `SaveSettings` 并按「保存中/已保存/保存失败」三态回显，保存期间控件禁用、失败后重读后端当前值，界面不停在旧副本上；前端不再有第二份配置副本，也没用 `localStorage`。页面迁移（本阶段第 2 条）已开工，第一屏是目录列表页（`frontend/src/app/App.tsx` + `src/api/` + `src/components/` + `src/features/items`）；完整设置对话框、任务队列面板、侧栏与队列拖拽、另外五套主题仍不在工程里。绑定已随这 4 个命令重新生成：`service.ts` 6 个方法，新增 `internal/config/models.ts`；`wails3 generate bindings` 必须带 `-ts`，否则会用 `.js` 覆盖并删掉工程里已提交的 `.ts`。

### 阶段二：检测与任务

1. 使用系统解析器实现 DNS，先通过假 resolver、模拟网络和流量预算测试。
2. 接入任务事件、逐候选状态、取消和预算限制；只做 DNS/TCP/TLS 检测，不修改 Hosts。
3. 完成网络和平台边界测试后才开放真实目标的检测。

### 阶段三：Hosts 与特权服务

1. 在批准平台验证安装、授权、白名单 IPC、升级、卸载和故障行为。
2. 按冻结的区段格式实现解析、事务写入、冲突检测、恢复和回滚。
3. 仅通过临时文件集成测试和 helper 验收后启用对应平台的 Hosts 操作。

### 阶段四：发布能力

1. 托盘、启动行为按批准平台验收。
2. 更新信任清单、密钥轮换、平台签名和失败回滚通过验证后才开放下载安装；否则保持“检查版本并打开发布页”。
3. `zh-TW` 全键与插值测试、人工校对通过后才标记正式支持。

## 七、验收清单

- [ ] 新增一种语言时只需新增一个语言资源文件。
- [ ] 语言缺少文案或插值参数不一致时，测试和构建会失败。
- [ ] 所有页面共用一套主题令牌和基础控件。
- [ ] 加载、空数据、失败、禁用和键盘状态在相关组件中表现一致。
- [ ] 每个可操作控件都有可见的开始反馈和成功、失败或取消结果；慢操作有持续进度或步骤提示。
- [ ] 超时和断连会结束忙碌状态并显示错误或重试入口；没有只靠 Toast 提示的关键失败。
- [ ] 自动化测试覆盖慢请求、失败、取消、重复点击和迟到事件，不会出现无限加载或旧状态覆盖。
- [ ] 异步 RPC 与 Wails 事件按 revision/sequence 合并；重复和过期更新不会覆盖新状态，丢失事件后能重新读取快照。
- [ ] IP 候选探测同时并发且不超过 8；GUI 能逐项显示正在检测/可用/失败/超时和整体进度，不只在任务结束后一次性呈现。
- [ ] 单项检测不超过 5 秒、服务级全量扫描不超过 30 秒；上限到达后返回部分结果和明确终态，取消可及时停止未完成探测。
- [ ] 用户可设置省流/标准/完整检测；各档的并发数、每域名候选数和每轮探测数有可见上限，后端验证生效，不仅是前端控件状态。
- [ ] 探测不下载目标网页/文件正文，不自动重试或下载更新；系统计量网络不可判断时不会自动假设无限流量。
- [ ] 同一业务实体在菜单树、详情、工具栏和队列中的显示从同一权威状态派生，更新后无互相矛盾的状态。
- [ ] 方法选择保存失败、冲突或请求乱序时，界面不会把未提交选择显示为已生效。
- [ ] 键盘可完成核心操作；状态消息可被辅助技术识别；颜色对比满足 WCAG 2.2 AA 要求。
- [ ] 清空队列移除全部记录；有任务运行时拒绝清理；重启后队列为空。
- [ ] “清理已结束”只移除成功、跳过、失败、回滚、取消和已恢复任务；排队中和执行中任务保留，且终态清理可在其他任务运行时执行。
- [ ] “清空队列”会移除排队任务；存在执行中任务时拒绝，并且不影响 Hosts、配置或恢复元数据。
- [ ] 检测/应用焦点跟随分别持久化且默认关闭；自动展开不移动键盘焦点，任务结束不覆盖用户运行期间的手动分类操作。
- [ ] Hosts 单项还原恢复该项目应用前的映射；共享记录在仍有其他项目所有者时保留，区段外用户内容不变。
- [ ] Hosts 全部还原逐项报告结果；冲突不覆盖文件，已还原项目隐藏入口，再次成功应用后建立新的还原状态。
- [ ] 队列不产生持久化历史文件；配置、日志、备份和临时文件只写入有界的应用专属目录。
- [ ] GitHub 与 Steam 的默认加速方法分别保存；更改一个服务不改变另一个服务，批量检测和应用按目标所属服务选择方法。
- [ ] 切换方法不启动网络请求；服务检测按钮只启动检测，不修改 Hosts；未注册方法不会显示为可用。
- [ ] 后端耗时操作不会阻塞界面；进度推送有限频，取消后无迟到状态覆盖。
- [ ] 所有已批准的首发平台和架构均可编译；未批准平台不宣称支持，系统能力缺失时返回明确的“不支持”错误。
- [ ] Hosts 测试只使用临时文件，不改开发机真实 Hosts。
- [ ] 主界面不以 root/管理员身份运行；在已批准平台验证 helper 的安装、授权和后续复用。授权撤销或系统策略要求重新确认时遵守系统流程并给出明确提示。
- [ ] 辅助服务不可用、IPC 请求无效或授权失败时，明确报错且不修改系统；接口不能执行任意命令或访问任意路径。
- [ ] 关闭并重新打开应用后可复用正常的 helper；服务损坏、授权撤销或用户移除时安全失败，不静默提权。
- [ ] Wails 生成绑定不手改；重新生成后检查接口差异。
- [ ] 关键窗口尺寸、主题和页面通过视觉回归检查。
- [ ] 所有已批准首发平台的 WebView 字体、DPI、滚动、快捷键和弹窗层级通过实机检查。
- [ ] 在最大预期数据量下记录交互延迟和主线程长任务；超出门槛时有复现步骤和修复记录。
- [ ] Go、Wails、前端运行时和依赖版本固定并经过 CI 验证。
