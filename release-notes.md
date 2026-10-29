## English

### github-net 0.1.0 Stable

**Added**

1. Updates install in one click: the "download and install" action fetches the package, verifies digest and signature, then hands it to the system installer
2. Verification is a hard gate: bad signature, tampered content, missing signature file or missing public key all refuse and void the package
3. The release script produces Ed25519 signatures with the public key shipped in the app; nothing downloads or installs silently

**Fixed**

None

**Updated**

None

## 中文

### 0.1.0 稳定版

**新增**

1. 更新可以一键装了：检测升级屏里「下载并安装」会下载安装包、校验摘要与签名，通过后交给系统安装器完成升级
2. 校验是硬闸：签名不对、内容被改、没有签名文件或没有公钥一律拒绝，安装包当场作废
3. 发布脚本出 Ed25519 签名，公钥随应用分发；应用内绝不自动下载或静默安装

**修复**

无

**更新**

无
