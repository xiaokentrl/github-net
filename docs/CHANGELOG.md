# 更新日志

> 遵循 Keep a Changelog 精神，记录 github-net 每个版本改了什么。当前应用版本 `0.1.0`（单一真实来源：`main.go` 的 `appVersion`；以后建了 `wails.json` 就换成它的 `info.productVersion`，两个地方不能同时算数）。发布正文用仓库根的 `release-notes.md`，由 `scripts/gen-release-notes.sh` 从 `docs/release-notes/<版本号>.entries` 那份条目清单生成（`release-notes.md` 是生成物，不手改），格式契约见脚本头注释。
>
> 版本标签用 `v主版本.次版本.修订号`（如 `v0.1.0`），预览版带后缀（如 `v0.2.0-beta.1`）。出正式版时把「未发布」段的内容整理成该版本小节。两边分类不完全一样，是有意这么定的：仓库日志继续按 新增/修改/修复/删除 四类记；Release 正文按固定三组出——新增、修复、更新，其中「修改」和「删除」都并进「更新」这一组（条目动词写清，例如「删除了…」），没有内容的组只写一个「无」（英文段写 `None`），有条目按 `1.`、`2.`、`3.` 编号；版本类型不单独成行，并进标题（如「0.1.0 稳定版」）；中英各一份，形状由 `scripts/gen-release-notes.sh` 保证，条目内容要精简、准确、只写本版净变化。

## [未发布]

- **工程骨架**：Wails v3 + React/TS/Vite 骨架、`Taskfile.yml`、Linux CI 门禁（前端构建、后端格式化/静态检查/测试、Linux x64 出包）。
- **产品目录**：`internal/catalog/catalog.json` 收敛服务、项目、域名和稳定 ID；`GetItems` 接口让分类树和详情页读后端真实数据，不再用前端写死的列表。
- **设置服务**：`internal/config` 接入，配置经后端读写并带 revision 与幂等键，不走前端 `localStorage`。
- **页面迁移**：第一屏把分类树、搜索、详情页、顶栏与状态栏改读后端真实数据（Hosts 状态当时给「未读取」，不编造结果；真值见下一条「Hosts 只读」）；第二屏接上设置中心对话框（侧栏底部入口，含布局与网络与流量两页），每控件即时保存、失败回读权威快照，队列高度预设先禁用并说明原因，Esc 与初始焦点已处理。真 WebView 逐项实测：侧栏 240→480px、revision 0→1→2→3、越界输入被前端挡住且不动 revision。
- **Hosts 只读**：`internal/hosts` 落地管理区段判读（标记固定为 v1 那一对，缺端、重复、嵌套、顺序错、版本未知、区段内非法行全部识别并给出带行号的原因）与只读读取；`GetItems` 每次调用现读一次系统 Hosts，54 个项目的状态从「未读取」换成真值。现在后端只产出「未替换」「冲突」「读取失败」三档，「已生效」「部分生效」要等 Hosts 写入接上才有可比对的摘要。详情页加了这次读取的时间、第几次读取，冲突和读取失败会显示具体原因。本工具仍然不写 Hosts，测试只用临时文件，没碰系统 Hosts。
- **推送前自动重装**：`.githooks/pre-push` + `scripts/pre-push-reinstall.sh`，每次推送前打包、卸载旧包、装新包并删配置。root 侧优先交给只干「卸载+安装」的 root 助手（`scripts/github-net-root-install.sh` + `scripts/setup-passwordless-reinstall.sh` 一次性开免密，之后推送不再打断）；没配助手时按「免密 → 终端 → 图形密码框」依次试。授权或安装没过就中断推送。`GITHUB_NET_SKIP_REINSTALL=1` 可跳过一次。
- **单实例与图标关联**：`main.go` 启用 Wails 单实例锁（UniqueID `io.github.xiaokentrl.githubnet`）——再点启动器或应用图标时新进程直接退出，已有实例收到「还原＋显示＋聚焦主窗口」的请求；Linux 设 `Linux.ProgramName=github-net`，打包任务给 `.desktop` 追加 `StartupWMClass=github-net`，让运行窗口归进应用图标那一组；macOS 用普通应用激活策略并监听 Dock 重开事件。托盘「显示主窗口」和托盘单击走的是同一个聚焦请求。真二进制实测（跑构建产物、配置目录指临时）：第一实例确实持有会话总线上的单例名，第二次启动退出码 0、没有第二个窗口也没有第二个进程，窗口 `WM_CLASS` 实测是 `github-net`、与 `StartupWMClass` 对得上。两条没验：聚焦是否真把窗口拉到前台、拓展坞上的分组与运行小点要人眼；连不上会话总线时程序静默退出（退出码 1、一个字都不打印），要不要加提示待裁决。
