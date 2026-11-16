## English

### github-net 0.1.22 Stable (0.1.21 → 0.1.22)

**Added**

None

**Fixed**

None

**Updated**

1. "Apply usable IPs" upgraded to redundant writes: every usable IP found for a hostname is written to the hosts file as separate lines, so browsers fall back to alternatives when one is blocked
2. Auto-replacement now rewrites all live IPs for the affected project, not just the single best one

## 中文

### 0.1.22 稳定版（0.1.21 → 0.1.22）

**新增**

无

**修复**

无

**更新**

1. 「补充可用IP」升级为冗余写入：每个主机名把检测出的全部可用 IP 都写进系统 Hosts（同主机名多行冗余），单 IP 被掐时浏览器自动落到备选行
2. 自动补位同步升级：IP 失效后自动改写该项目区段的全部活 IP（不只最优单 IP）
