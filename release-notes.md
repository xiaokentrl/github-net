## English

### github-net 0.1.14 Stable (0.1.13 → 0.1.14)

**Added**

None

**Fixed**

1. Dark-theme users no longer see a white flash on cold start: the first frame renders in the configured theme
2. Traditional-Chinese users no longer see Simplified text in backend error notices: all 25 error codes are localized
3. The tray left-click no longer hides a visible window; it restores/focuses as originally decided
4. The tray theme menu now includes all seven themes (the missing warm-yellow added)
5. Queue shutdown no longer waits forever if an executor ignores cancellation; it caps the wait at 5 seconds
6. A single invalid key in a hand-edited config no longer resets every setting; per-group salvage keeps the rest
7. Fast repeated checkbox toggles can no longer postpone the save indefinitely; it flushes within 2s and on exit
8. Error codes referenced inside error messages are no longer mis-detected; matching prefers line starts

**Updated**

1. Hardened the update download chain: per-hop origin whitelist, rejected packages deleted, download mutex, package dir moved inside the app directory
2. New automation gates: bindings regeneration diff, clean-tree push check, post-install assertions, app relaunch after upgrade
3. Detection progress events are batched and coalesced per candidate, flushed at round end
4. Re-probing a single IP updates the detail view immediately; the applied mark converges regardless of ordering

## 中文

### 0.1.14 稳定版（0.1.13 → 0.1.14）

**新增**

无

**修复**

1. 修复深色主题用户冷启动白闪的问题：窗口首帧即按配置主题渲染，不再先白后跳
2. 修复繁体用户在后端错误提示上看到简体的问题：25 类错误按界面语言出文案
3. 修复托盘左键会把已显示的窗口藏起来、与「幂等唤回」拍板相反的行为
4. 修复托盘主题菜单缺「暖阳黄」第七套配色的问题
5. 修复队列关停时执行器不响应取消会拖死整个退出的风险：关停等待加 5 秒上限
6. 修复批量写 Hosts 时手改配置单个键会让全部设置归零的问题：逐组抢救，坏组退默认其余照常
7. 修复「记住检测档位」在快速连续勾选后可能丢失的问题：防抖加 2 秒上限并在退出前冲刷
8. 修复后端错误码在嵌套引用时可能认错码的问题：按行首优先匹配

**更新**

1. 更新安装包下载与交接整链加固：来源逐跳白名单、验签失败即删、下载互斥、升级包收回应用目录内
2. 新增绑定生成对账门、推送前验树、装后三验与运行中拉回等自动化门
3. 检测进度事件批量节流（同候选合并＋单批上限＋轮尾冲刷），界面不再被事件洪峰冲刷
4. 单条 IP 重新检测后详情页结论立即更新；检测中应用「当前生效」标记两种时序都收敛
