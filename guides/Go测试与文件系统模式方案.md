# Go 测试与文件系统模式：Wails3 + Go 专用方案

## 定位与硬性目标

来源：spf13/go-skills 的 `go` 与 `afero` 技能（MIT，Steve Francia）。母法方案：《日志与可诊断性技术方案》（观测面）、《状态码方案》（错误码机器检索）、《默认安装目录规划方案》（目录契约——本方案是其 Go 侧实现的测试纪律）。本方案管两件事：**文件 I/O 的可测试性**（注入文件系统）与 **Go 单测的模式纪律**（表驱动、fake、golden）。

**硬性目标**：

1. 文件 I/O 全走注入接口：业务逻辑零 `os` 直调——`os` 只允许出现在 `main`（选实现）与测试（临时目录）；一处漏网的 `os.WriteFile` 就让内存测试说谎；
2. 单测不碰磁盘不碰网络：逻辑测试注入内存实现，磁盘真实行为单列磁盘测试（差异见第三节——内存绿测 ≠ 契约）；
3. 测试就是普通 Go：表驱动 + 手写 fake——禁 BDD 框架、禁 mock 代码生成器。

## 一、按「代码做什么」选接口

| 代码… | 接受 | 测试注入 | 生产注入 |
|---|---|---|---|
| 只读 | `fs.FS`（标准库，零依赖） | `fstest.MapFS` | `os.DirFS(dir)` / `embed.FS` |
| 读+写 | `afero.Fs` | `afero.NewMemMapFs()` | `afero.NewOsFs()` |
| 必须困在目录内，名字不可信 | `*os.Root` | `os.OpenRoot(t.TempDir())` | `os.OpenRoot(dir)` |

只读代码不引入依赖（`io/fs` + `testing/fstest` 是标准库）；**要写才用 afero**；不可信文件名（用户输入/压缩包/网络）走 `os.Root`——它同时拒绝 `../` 与逃逸符号链接（`path escapes from parent`）。

**版本门控**：本方案引用的版本门控特性——`os.Root` Go 1.24（`ReadFile/WriteFile` 1.25）、`sync.WaitGroup.Go` 1.25、`errors.AsType[T]` 1.26、`goroutineleak` profile 1.27——落地前先核对项目 go.mod 的 Go 下限，低于下限用对应旧形态（`root.Open/Create`、`Add/Done`、`errors.As(&target)`）；未核对的引用一律标**待验证**。afero 行为清单已由源技能对 afero v1.15.0 / Go 1.27 实证（MIT 出处声明）。

## 二、注入模式：Fs 注入，main 选实现

```go
type Store struct {
    fs  afero.Fs
    dir string
}

func NewStore(fs afero.Fs, dir string) *Store { return &Store{fs: fs, dir: dir} }

func (s *Store) Save(name string, data []byte) error {
    if err := s.fs.MkdirAll(s.dir, 0o755); err != nil {
        return err
    }
    return afero.WriteFile(s.fs, filepath.Join(s.dir, name), data, 0o644)
}
```

- `main` 传 `afero.NewOsFs()`；测试传 `afero.NewMemMapFs()`——构造函数即接缝（与《Wails3框架版本判别与服务纪律方案》「依赖注入走普通构造函数」同一条纪律）；
- 包级 helper 首参带 Fs（`afero.ReadFile/WriteFile/Walk/Exists/TempDir/Glob`）；用得多时 `afero.Afero{Fs: fs}` 包成方法（`a.Exists(p)`）；
- Wails3 绑定服务落位：服务结构体持有注入的 Fs，业务逻辑在零 Wails 导入的领域包（→ 服务纪律薄适配器）。

## 三、MemMapFs 不是磁盘（四差异决定绿测的含金量）

内存绿测证明的是**逻辑**，不是文件系统契约。四条差异决定哪些测试必须补磁盘版：

1. **自动补建父目录**：`WriteFile(mem, "/no/such/dir/f.txt", …)` 成功且目录出现；OsFs 上同一调用失败——**漏写的 `MkdirAll` 在内存测试里永远测不出**。证明代码会建目录的测试必须跑磁盘：`afero.NewBasePathFs(afero.NewOsFs(), t.TempDir())`；
2. **不校验权限位**：`0o444` 写出的文件照样可写。权限处理在磁盘或 `ReadOnlyFs` 上测；
3. **根路径与相对路径是两个条目**：`data/x.txt` ≠ `/data/x.txt`。所有路径从同一个绝对根用 `filepath.Join` 拼，禁止混用两种键；
4. **是弱 `fs.FS`**：桥接 `MemMapFs`（`afero.NewIOFS`）过不了 `fstest.TestFS`——测 `fs.FS` 消费者用 `fstest.MapFS`，不用桥接的 MemMapFs。

并发安全：MemMapFs 并发写不同文件过 race 检测器，`t.Parallel` 子测试可共享一个实例。

## 四、分层文件系统与 dry-run 的边界

| 构造器 | 行为 |
|---|---|
| `NewReadOnlyFs(fs)` | 一切写失败 EPERM（`errors.Is(err, os.ErrPermission)`） |
| `NewCopyOnWriteFs(base, layer)` | 读落到 base、写进 layer，base 永不修改 |
| `NewBasePathFs(fs, dir)` | 路径映射进 dir，`../` 越界不解析 |
| `FromIOFS{FS}` / `NewIOFS(fs)` | `fs.FS` ↔ `afero.Fs` 互转（后者见第三节第 4 条） |

**CopyOnWriteFs 的 dry-run 只覆盖创建/覆盖**：对 base 独有的文件，`Remove` 报「不存在」、`Rename` 报 EPERM、`RemoveAll` 返回 nil 但文件仍在——**删除/移动类操作必须显式 plan-then-apply**（已吸收进《默认安装目录规划方案》dry-run 两步模式，本条是它的机制依据）。

## 五、BasePathFs 不是安全边界

BasePathFs 只限**词法**路径；基目录内的符号链接指向外面，就读到外面的文件。不可信名字一律 `os.Root`（Go 1.24+，符号链接与 `../` 双拒）；文件名消毒在进文件系统之前做（→《默认安装目录规划方案》可信根 + 不可信部分）。**沙箱需求用 `os.Root`，不用 BasePathFs**。

## 六、单测模式纪律

1. **表驱动是绝对标准**：用例切片（name / 输入 / 期望）+ `t.Run` 子测试；前端 vitest 同构映射（`it.each`）；
2. **helper 必须 `t.Helper()`**：失败定位到用例行，而非 helper 行；前端同构（自定义 expect 包装报错指回用例）；
3. **手写 fake 优先于 mock 框架**：Go 隐式接口让 fake 十行以内——测试依赖轻、逻辑透明；接口在消费者侧定义（→《公共函数与Helper方案》接口三条）才 fake 得动；
4. **golden files + `testdata/`**：复杂输入/大输出的用例（解析器、生成器）把期望产物存 `testdata/`，测试比对；Go 工具链忽略该目录不打包；前端对应=快照纪律（快照变更必须显式审查，不 `-u` 一把梭）；
5. **结构比较用 `cmp`**（go-cmp）不用 `reflect.DeepEqual`——diff 可读；
6. **包布局决定可测性**：`package main` 不可导入——测不了就要拆领域包（→《后端目录结构方案》《公共函数与Helper方案》判定流程）。

## 七、错误处理源头纪律（码的上游）

1. **错误是值不是异常**：显式检查每一个返回的 error，不吞不忽略；
2. **wrap 加上下文，不加栈**：`fmt.Errorf("loading config %s: %w", path, err)`——%w 保住链式判定（`errors.Is/As`），上下文写「正在做什么」，人话文案留给文案层（→《状态码方案》码稳定检索、人话随语言走）；
3. **批量失败用 `errors.Join`**：多子任务失败聚合成一个错误返回（对应任务队列部分失败的失败清单）；
4. **类型判定**：Go 1.26+ 用 `errors.AsType[T](err)`，以下用 `errors.As(&target)`。

## 测试矩阵

| 维度 | 覆盖 |
|---|---|
| 注入纯度 | grep 断言：领域包/服务包无 `os.` 文件调用（白名单仅 main 与测试） |
| 内存/磁盘分档 | 逻辑测试全内存；建目录、权限、符号链接三类契约测试跑 `BasePathFs(OsFs, t.TempDir())` 或真实盘 |
| 路径键 | 路径全部由绝对根 `filepath.Join` 拼出；无裸字符串拼接、无混用根/相对键 |
| 不可信名字 | `../`、绝对路径、逃逸符号链接被 `os.Root` 拒绝（负面用例表驱动） |
| fake 纪律 | 无 mock 生成器依赖；fake 实现手写且 ≤ 消费接口 |
| 错误链 | wrap 全部 `%w`（`errors.Is/As` 可穿透）；批量失败 `errors.Join` 可拆 |

## AI 约束模板

> 业务逻辑零 `os` 文件调用——文件系统一律构造函数注入：只读收 `fs.FS`（测试 `fstest.MapFS`）、读写收 `afero.Fs`（测试 MemMapFs）、不可信名字收 `*os.Root`；`os` 只出现在 main 与测试。内存绿测不算文件系统契约：建目录、权限、符号链接行为单列磁盘测试；路径一律从绝对根 filepath.Join 拼出。测试是普通 Go：表驱动 + t.Helper + 手写 fake，禁 mock 生成器与 BDD 框架；golden 期望存 testdata/。错误显式检查、wrap 用 %w 加上下文、批量失败 errors.Join；码与文案分层（→ 状态码方案）。删除/移动类 dry-run 必须 plan-then-apply，不信 CopyOnWriteFs。实现后跑本方案测试矩阵并报告注入纯度 grep 结果。

## 五档定位

- **一档**：接口选型表 + 注入模式写进服务/领域包骨架；错误 wrap 纪律生效；
- **二档**：注入纯度 grep 进 CI；三类磁盘契约测试就位；vitest 表驱动/快照纪律对齐；
- **持续**：新文件 I/O 代码按选型表自查；MemMapFs 差异清单随 afero 版本复核。
