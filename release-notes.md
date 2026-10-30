## English

### github-net 0.1.1 Stable

**Added**

1. First public release: probe available IPs for GitHub domains (DNS candidates + TLS verification) and write them into the system hosts file in one click
2. Task queue: pause/resume, cancel, retry, clear and restore-all; batch detection shares a per-round budget and runs in parallel
3. Safe hosts writes: backup, atomic replace, post-write verification and automatic rollback; restore to the pre-apply state
4. A restricted privileged helper (Linux systemd) is authorized once; hosts writes never ask for admin again
5. Six themes, bilingual UI, tray with launch-at-login, and a two-pane task queue with per-item details
6. In-app updates: checks the latest GitHub stable release and verifies digest and signature before install

**Fixed**

None

**Updated**

None

## 中文

### 0.1.1 稳定版

**新增**

1. 首个公开发布：检测 GitHub 域名的可用 IP（DNS 候选 + TLS 验证），一键写入系统 Hosts 加速访问
2. 任务队列：暂停/继续、取消、重试、清理与全部还原；批量检测共享每轮预算并按档位并行
3. Hosts 安全写入：备份、原子替换、写后校验、失败自动回滚；恢复到应用前状态
4. 受限特权辅助服务（Linux systemd）一次授权，之后读写 Hosts 不再要管理员密码
5. 六套主题、双语界面、托盘与开机自启动、任务队列与子菜单明细
6. 应用内更新：自动检查 GitHub 最新稳定版，下载前后做摘要与签名双校验

**修复**

无

**更新**

无
