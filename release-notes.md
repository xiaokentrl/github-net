## English

### github-net 0.1.0 Stable

**Added**

1. Batch detection now matches the prototype: one shared per-round probe budget, items run in parallel, and total in-flight network requests stay capped at 8
2. When the budget cannot cover everything you confirm up front; short items are marked "budget exhausted, not probed" instead of failing silently
3. Queue panel catches up: cancel waiting, retry failed (with per-row retry), per-row restore, restore-all, resizable queue height and a spinner for running tasks
4. Detail pages gain "detect all", probing the whole service with your chosen method in one go

**Fixed**

None

**Updated**

None

## 中文

### 0.1.0 稳定版

**新增**

1. 批量检测按原型对齐：一次检测全部共享每轮候选预算，项目按档位并行跑，全进程网络并发收口在 8
2. 预算盖不全时启动前先确认，没排上的项目明确标「预算不足，未检测」
3. 队列面板补齐：取消等待、重试失败、行内恢复、全部还原、队列高度可调、执行中显示转轮与阶段
4. 详情页新增「检测全部」，按当前加速方法一次检测整个服务

**修复**

无

**更新**

无
