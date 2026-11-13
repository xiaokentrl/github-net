## English

### github-net 0.1.18 Stable (0.1.17 → 0.1.18)

**Added**

None

**Fixed**

1. Fixed resume attempts stuck forever when a fragment was already complete: full-size fragments are now promoted to the package instead of being rejected by the server (HTTP 416)
2. Fixed resume assembling a corrupt package when the server misaligns the response: the start offset is now validated and downloads restart from scratch on mismatch
3. Repeated clicks no longer re-download an already fetched package; the existing file is reused
4. The check-updates button is disabled during a download so the progress display can no longer be displaced
5. Automatic update-check failures are now localized for Traditional-Chinese users

**Updated**

1. Resume now validates fragment identity via HTTP If-Range: same-version republish or content changes trigger a clean restart instead of blind appending
2. Duplicate-download rejections now use a dedicated code and clear copy, no longer conflated with "package discarded"
3. The main window title bar now shows the version next to the app name

## 中文

### 0.1.18 稳定版（0.1.17 → 0.1.18）

**新增**

无

**修复**

1. 修复升级包残片恰好收满后重试永远失败的问题：收满的残片直接转正，不再被服务器 416 卡死
2. 修复弱网续传在服务器响应错位时可能拼出坏包的问题：校验响应起点，错位就整段重来
3. 修复重复点击「下载并安装」会把已收的安装包整包重下的问题：成品在位直接复用
4. 修复下载中点「立即检查更新」会把下载进度显示顶掉的问题：下载期间该按钮禁用
5. 修复繁体用户在启动自动检查失败时看到简体文案的问题：失败提示按界面语言出

**更新**

1. 断点续传加身份核对：同版本重发布或内容变更时自动整段重来，不盲拼
2. 重复点击下载的拒绝提示改用独立错误码与准确文案，不再与「安装包已作废」混淆
3. 主界面标题栏在应用名后显示版本号
