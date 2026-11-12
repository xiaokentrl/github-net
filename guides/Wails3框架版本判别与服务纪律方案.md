# Wails3 框架版本判别与服务纪律：Wails3 + Go 专用方案

## 定位与硬性目标

来源：spf13/go-skills 的 wails 技能（MIT，Steve Francia）——本方案吸收其事实清单与纪律，按本项目（Wails3）裁剪为单版本纪律。姊妹方案：《窗口与多窗口技术方案》（窗口细节）、《系统能力与平台集成技术方案》（桥接）。

**硬性目标**：

1. 版本纯度：全库只允许 Wails3 API——任何 v2/v3 混用即缺陷（LLM 与人类都常犯，见判别表）；
2. 服务纪律：绑定服务是跨窗口单例——可变状态必须互斥锁保护；业务逻辑零 Wails 导入；
3. alpha 纪律：wails3 版本钉死 go.mod，升级必须读 changelog。

## 版本判别表（v2 / v3 不兼容 API）

写任何 Wails 代码前先判版本——**从项目判，不从用户措辞猜**：

| 信号 | v2（稳定） | v3（本项目） |
|---|---|---|
| go.mod 导入 | `github.com/wailsapp/wails/v2` | `github.com/wailsapp/wails/v3` |
| CLI | `wails dev` / `wails build` | `wails3 dev` / `wails3 build` |
| 构建编排 | `wails.json`（黑盒） | `Taskfile.yml`（go-task，透明可改） |
| 入口 | `wails.Run(&options.App{...})` | `application.New(application.Options{...})` |
| Go→JS 绑定 | `Bind: []interface{}{app}` | `Services: []application.Service{...}` |
| 前端导入 | `wailsjs/go/main/App` | `frontend/bindings/<go-import-path>/<service>` |
| 运行时调用 | `runtime.EventsEmit(ctx, ...)`（须存 context） | `app.Event.Emit(...)` 等管理器方法，无 context 穿线 |

**混用红旗（见到即拒绝，先于一切修改）**：

- v3 项目出现 `runtime.EventsEmit(ctx, ...)` 或 `pkg/runtime` 导入 → v2 API；
- v2 项目出现 `application.New(...)` 或 `pkg/application` → v3 API；
- v3 前端导入 `wailsjs/go/...`（应为 `frontend/bindings/...`）或反之；
- 对 v3 项目跑 `wails dev`、对 v2 项目跑 `wails3 dev`；
- `application.Options` 里出现 `Bind:` 或 `options.App` 里出现 `Services:`——各属对方版本。

## v3 服务纪律（绑定模型）

服务 = 普通结构体，导出方法即 JS 可调用；两个可选生命周期钩子：

```go
type NoteService struct {
    db *sql.DB
    mu sync.RWMutex // 服务是跨所有窗口的单例——可变状态必须互斥锁
}

// 返回 error 会中止整个应用启动
func (s *NoteService) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
    return nil
}

// 服务按注册逆序关闭
func (s *NoteService) ServiceShutdown() error {
    return s.db.Close()
}

func (s *NoteService) Save(n Note) error { ... } // JS 可调用
```

纪律：

1. **薄适配器**：绑定服务只做参数校验与转发，业务逻辑在零 Wails 导入的独立包（可作纯 Go 测试）；
2. **依赖注入走普通构造函数**：`application.NewService(NewNoteService(db, logger))`——不引 DI 框架；
3. **单例并发**：绑定方法可能被多窗口并发调用——共享可变状态必须 `sync.RWMutex`；
4. **错误显式返回**：绑定方法的 error 变成前端 Promise rejection——不 panic、不吞错；
5. **前端是不可信输入**：绑定方法的参数校验等同 HTTP handler；绝不从前端字符串拼 shell 命令或文件路径；
6. **机密只留在 Go 侧**：打进 `frontend/dist` 的任何内容用户都可读；
7. 事件：`app.Event.Emit` / `app.Event.On`；可取消钩子：`window.RegisterHook(events.Common.WindowClosing, ...)`——「关闭前确认」标准模式。
8. **前端绑定生成**：`wails3 generate bindings`（`wails3 dev` 自动执行）——前端从 `frontend/bindings/<go-import-path>/<service>` 导入，**不是 v2 的 `wailsjs/`**；生成物勿手改（每次 dev/build 重新生成）。

## 构建系统与 alpha 纪律

- 构建由 go-task（`Taskfile.yml`）编排：图标、manifest、打包都是可见可改的任务步骤——**改 tasks，不跟 CLI 死磕**；平台打包：`wails3 package`（macOS .app/dmg、Windows NSIS、Linux AppImage/deb——与本库 `packaging/` 契约对齐）；
- **钉死 alpha 版本**于 go.mod，升级必须读 changelog（nightly alpha 会改名）；
- **API 编译失败且修法不明显时，先查 v3.wails.io/changelog，禁止凭记忆猜**——训练数据滞后于 alpha 是设计使然；
- **`WAILS_MCP=1` 环境变量启动应用**：内嵌 MCP 服务器，AI agent 可驱动运行中的应用（窗口控制、DOM 检查、绑定方法调用）——**项目期验证的核心工具**（核销「待验证」清单的自动化手段，见交付清单第四节）。

## 测试矩阵

| 维度 | 覆盖 |
|---|---|
| 版本纯度 | grep 断言：全库无 v2 信号（`/v2` 导入、`wailsjs/`、`Bind:`、无 context 的 runtime 调用） |
| 服务并发 | 绑定方法并发调用压测（`-race`） |
| 生命周期 | ServiceStartup 返回 error 时应用启动中止；关闭逆序 |
| alpha 钉版 | go.mod wails3 alpha 版本与 changelog 对照（升级必须显式） |

## AI 约束模板

> 写任何 Wails 代码前，先判版本（go.mod 导入路径判 v2/v3）；本项目 = Wails3，全库只允许 v3 API——`application.New` + Services + 管理器风格（`app.Window.NewWithOptions`/`app.Event.Emit`），前端绑定走 `frontend/bindings/`；绑定服务是跨窗口单例，可变状态必须互斥锁；业务逻辑零 Wails 导入；绑定方法显式返回 error、参数按不可信输入校验；.app 包内零写入；wails3 alpha 版本钉死 go.mod，升级读 changelog，API 编译失败先查文档不凭记忆猜；遇到 v2 信号（`runtime.EventsEmit(ctx,...)`、`Bind:`、`wailsjs/`）即报版本混用缺陷。
