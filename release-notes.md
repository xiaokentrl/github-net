## English

### github-net 0.1.11 Stable (0.1.10 → 0.1.11)

**Added**

1. The method dropdown now defaults to "stored IPs": no network needed, instant results, and the method shown in the dialog always matches the one actually executed
2. The item dialog is now a one-stop detection panel: pick method, pick tier and run in one place
3. "Detect with method" now covers every item under the current menu (one-shot, defaults untouched), opening the tier dialog first
4. New "detect availability" header button (blue outline, detection semantic color) covering the whole menu in one click
5. Adopt 13 planning guides into the repo (sync layer, network resilience, diagnostics, etc.) plus an NFR baseline table and an operation-feedback matrix

**Fixed**

1. The item list now refreshes immediately after apply, per-IP re-probe and batch detect, instead of showing stale results
2. Source fetches gain idempotent retries (backoff with jitter); round-ledger and finished-task records get size caps
3. File logging with rotation and abnormal-exit detection; several interaction gaps fixed in the tier dialog and result refresh

**Updated**

1. The header selector is back to pure default-setting with an "acceleration method" tag; "detect all" renamed "detect with defaults"; a Content-Security-Policy was added

## 中文

### 0.1.11 稳定版（0.1.10 → 0.1.11）

**新增**

1. 检测方法下拉默认选中「使用已有储存」：不联网、秒出结论，弹框显示与实际执行的方法保持一致
2. 详情弹框升级为一站式检测台：选方法、选档位、执行检测在一个界面完成
3. 「只测此方法」改为检测本主菜单下全部子项（一次性，不改默认设置），并先弹档位弹框选档
4. 头部新增「检测可用性」按钮（蓝描边，检测类语义色），一键检测本主菜单全部项目
5. guides 指南库引入 13 篇立项方案（统一同步层、网络弹性、日志可诊断性等），新增 NFR 基线表与操作反馈矩阵

**修复**

1. 详情列表在写入/重检测/整批检测后立即刷新，不再停留在旧结论
2. 源拉取增加幂等重试（退避＋抖动）；轮预算账本与队列终态记录设上限，挂机不再无界增长
3. 日志落盘（1MB 轮转）与异常退出检测；检测档位弹框与结果刷新的多处交互缺口修复

**更新**

1. 头部选择器回归纯默认设置并加「加速方法」标签；「检测全部」改名「按默认方法检测」；新增内容安全策略（CSP）
