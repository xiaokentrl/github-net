## English

### github-net 0.1.7 Stable (0.1.6 → 0.1.7)

**Added**

1. Apply now raises one clear success toast per round, with a warning variant reporting successful and failed counts for partial failures, instead of one notice per task

**Fixed**

1. Fixed auto-restart of running browsers after apply not working: recognized the real Linux Edge process name msedge and excluded unrelated tools sharing a browser name prefix from being terminated
2. Every skipped browser restart now reports its real reason (disabled, nothing written, restarted within 30 seconds, did not exit gracefully, relaunch failed) and writes step-by-step logs
3. The 30-second debounce now only holds the window after a real restart, so a run that found no browser no longer blocks retries for the next 30 seconds

**Updated**

1. Introduced a frontend sync hub: commands only declare which facts are stale, refreshers are dispatched from a registry, with delayed fallback and focus-resume revalidation

## 中文

### 0.1.7 稳定版（0.1.6 → 0.1.7）

**新增**

1. 写入 Hosts 成功后按轮弹出一条明确的成功提示，部分失败用警告档并报出成功与失败条数，不再逐条刷屏

**修复**

1. 修复「应用完成后自动重启正在运行的浏览器」无效果：补上 Linux 版 Edge 的真实进程名 msedge，并排除名字撞浏览器前缀的独立工具进程，避免误杀
2. 浏览器重启的每一次跳过现在如实报出原因（开关未开、本轮没写成功、30 秒内刚重启过、没温和退出、拉起失败），并补分步日志供排查
3. 30 秒去抖只在真正重启成功时占窗口，没扫到浏览器不再挡住后续 30 秒内的重试

**更新**

1. 前端引入统一同步层：命令落定只声明失效哪些事实，刷新动作由登记表转发，并补延迟兜底与焦点恢复重刷
