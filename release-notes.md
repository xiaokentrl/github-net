## English

### github-net 0.1.10 Stable (0.1.9 → 0.1.10)

**Added**

1. Add a performance/resource baseline table (docs/NFR.md) and an operation-feedback matrix (docs/操作反馈矩阵.md)

**Fixed**

1. The CI-published installer was built without the production build tag and shipped a debugger: the build command now carries the tag and a "production build tag" gate step was added — packages published from 0.1.10 on no longer include it
2. Add a Content-Security-Policy: the page can no longer load external scripts; all sources are restricted to self
3. Per-IP re-probe now validates hostname ownership against the catalog, preventing probes against arbitrary domains
4. Result refresh gains single-flight merging, watermark-based out-of-order rejection and clear-safe refetching; the round-budget ledger and finished task records get size caps, so multi-day sessions no longer grow without bounds

**Updated**

1. Logs no longer record full user paths; proxy policies for the updater and source fetcher are declared explicitly; the error-code registry is documented

## 中文

### 0.1.10 稳定版（0.1.9 → 0.1.10）

**新增**

1. 新增性能与资源基线表（docs/NFR.md）与操作-反馈矩阵（docs/操作反馈矩阵.md）

**修复**

1. CI 发布的安装包此前未带 production 构建标签、携带了调试器：出包命令补标签并新增「生产构建标签」门禁步骤，0.1.10 起发布的包不再携带调试器
2. 新增内容安全策略（CSP）：页面不得加载任意外部脚本，全部资源收紧到 self
3. 单条 IP 重新检测补域名目录归属校验，防止对目录外的任意域名发起探测
4. 结果刷新补齐单飞合并、水位乱序拒绝与清空防回灌；轮预算账本与队列终态记录各设上限，挂机数天不再无界增长

**更新**

1. 日志不再记录含用户名的完整路径；updater 与源拉取的代理策略显式声明；错误码补登记表
