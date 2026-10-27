# 更新日志

> 遵循 Keep a Changelog 精神，记录 github-net 每个版本改了什么。当前应用版本 `0.1.0`（单一真实来源：`main.go` 的 `appVersion`；以后建了 `wails.json` 就换成它的 `info.productVersion`，两个地方不能同时算数）。发布正文用仓库根的 `release-notes.md`，由 `scripts/gen-release-notes.sh` 生成，格式契约见脚本头注释。
>
> 版本标签用 `v主版本.次版本.修订号`（如 `v0.1.0`），预览版带后缀（如 `v0.2.0-beta.1`）。出正式版时把「未发布」段的内容整理成该版本小节，分类（新增/修改/修复/删除）和 `release-notes.md` 保持一致。

## [未发布]

- **工程骨架**：Wails v3 + React/TS/Vite 骨架、`Taskfile.yml`、Linux CI 门禁（前端构建、后端格式化/静态检查/测试、Linux x64 出包）。
- **产品目录**：`internal/catalog/catalog.json` 收敛服务、项目、域名和稳定 ID；`GetItems` 接口让分类树和详情页读后端真实数据，不再用前端写死的列表。
- **设置服务**：`internal/config` 接入，配置经后端读写并带 revision 与幂等键，不走前端 `localStorage`。
- **页面迁移**：第一屏把分类树、搜索、详情页、顶栏与状态栏改读后端真实数据（Hosts 状态先给「未读取」，不编造结果）；第二屏接上设置中心对话框（侧栏底部入口，含布局与网络与流量两页），每控件即时保存、失败回读权威快照，队列高度预设先禁用并说明原因，Esc 与初始焦点已处理。真 WebView 逐项实测：侧栏 240→480px、revision 0→1→2→3、越界输入被前端挡住且不动 revision。
