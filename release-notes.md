## English

### github-net 0.1.19 Stable (0.1.18 → 0.1.19)

**Added**

None

**Fixed**

1. Fixed autostart pointing at the legacy install path so the app failed to launch at boot
2. Fixed the missing menu icon and failed launches: desktop entry now uses absolute Exec/Icon paths
3. Fixed all Hosts writes failing under the service sandbox: the allowlist now covers the /etc directory so temp-file-and-rename works again
4. "Apply usable IPs" renamed to "Supplement usable IPs" with the 10-minute re-verification and mapping-update semantics spelled out

**Updated**

1. Directory restructure: program files consolidated under /opt/github-net, privileged state under /var/opt/github-net, user config unchanged; the upgrade cleans legacy paths automatically
2. Privileged socket model upgraded to 0660 with an access group enforced by the kernel, removing the old any-local-process residual risk; log out and back in once after upgrading
3. The managed /etc/hosts section now matches markers regardless of the product id, keeping restores working across renames
4. The passwordless setup script now runs identity resolution as the invoking user and passes values explicitly, no longer depending on Go inside the restricted sudo environment

## 中文

### 0.1.19 稳定版（0.1.18 → 0.1.19）

**新增**

无

**修复**

1. 修复开机自启动指向旧安装路径、开机拉不起应用的问题
2. 修复应用菜单图标丢失与菜单启动失败的问题：桌面项 Exec/Icon 改绝对路径
3. 修复写 Hosts 全部失败的沙箱误伤：白名单放行目录而非单个文件，临时文件与替换流程恢复正常
4. 「写入可用IP」更名「补充可用IP」，说明写明 10 分钟复验与映射更新语义

**更新**

1. 目录重构：程序本体集中 /opt/github-net/，特权状态集中 /var/opt/github-net/，配置维持 ~/.config/github-net/；升级自动清理旧路径，配置零丢失
2. 特权服务 socket 访问模型升级：0660＋访问组制（内核把关），消除「本机任意进程可连」的历史残余风险；升级后需重新登录一次使组资格生效
3. /etc/hosts 管理区段标记匹配泛化：产品改名后旧区段照常识别与还原
4. 免密配置脚本改两段式：普通用户态取身份，sudo 只做特权动作，不再依赖受限环境里的 Go
