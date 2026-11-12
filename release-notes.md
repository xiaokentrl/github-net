## English

### github-net 0.1.13 Stable (0.1.12 → 0.1.13)

**Added**

None

**Fixed**

1. Fixed the candidate table being wiped repeatedly during a detection run: it now clears once when a new round starts instead of on every progress event
2. Fixed stale detail results after re-probing a single IP: the probe outcome now writes back into the in-memory result immediately
3. Fixed a race where applying during a detection dropped the "currently applied" mark: detection now re-marks from the live Hosts file when it finishes
4. Fixed duplicate skipped rows and doubled notices when double-clicking "apply usable IPs": an existing skipped row is now reused with its reason refreshed
5. Fixed a lock-up when metadata could not be saved: the transaction now rolls back instead of leaving Hosts and its ledger out of sync
6. Reduced the risk of concurrent Hosts writes from two helper instances: startup probes for a live service and write transactions take a cross-process lock
7. The per-menu restore dialog now requires its preview to load before confirming, matching the restore-all dialog
8. Concurrent update downloads no longer delete each other's files: the backend serializes and rejects repeats explicitly
9. Detection failures no longer spam one notice per item on top of the round summary; only the summary remains

**Updated**

1. Detection progress events are now batched and coalesced per candidate, flushed at round end
2. Window geometry and config writes use atomic file replacement with directory fsync; the ledger database now refuses files from a newer build
3. Asset downloads re-validate the origin whitelist on every redirect; the tray pill now minimizes instead of hiding, keeping window position on Wayland
4. The operation-feedback matrix now covers nine previously unregistered entries and fixes two stale declarations

## 中文

### 0.1.13 稳定版（0.1.12 → 0.1.13）

**新增**

无

**修复**

1. 修复检测进行中候选表被反复清空的缺陷：任务事件只在「新一轮开跑」那一刻清一次，不再逐条抹掉刚画出来的行
2. 修复单条 IP 重新检测后详情页结论不更新的缺陷：复测结论同步回写内存结果，状态、延迟与「可用」计数立刻跟上
3. 修复检测中点「应用」会丢「当前生效」标记的竞态：检测收尾按 Hosts 现状重标一次，两种先后顺序都收敛
4. 修复双击「写入可用IP」产生重复跳过行与双份通知的问题：同项重复跳过复用已有行并刷新原因
5. 修复元数据保存失败会让工具自锁的缺陷：内容已写入而账本没存时自动回滚到写入前状态
6. 修复双开辅助服务可能并发写 Hosts 丢更新的风险：启动时探测已有服务、写事务加跨进程锁
7. 修复还原弹框在对照表没读出来时也能点确认的问题，与全量还原弹框同一套门禁
8. 修复下载更新并发会互相删包的问题：后端互斥，重复调用明确拒绝
9. 修复批量检测失败时逐项刷屏通知与轮汇总双报的问题：检测类失败只走轮聚合一条

**更新**

1. 检测进度事件批量节流（同候选合并＋单批上限），轮收尾同步投完，界面不再被事件洪峰冲刷
2. 窗口几何、配置落盘统一原子写并补目录 fsync；底账库加版本守卫，库比程序新时明确拒绝
3. 资产下载来源白名单逐跳复验（重定向也拦）；托盘胶囊收起改用最小化，Wayland 下不再丢窗口位置
4. 操作反馈矩阵补齐九个漏登记入口并修正两处与实现不符的声明
