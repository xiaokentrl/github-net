## English

### github-net 0.1.9 Stable (0.1.8 → 0.1.9)

**Added**

None

**Fixed**

1. Fixed the 0.1.8 installer reporting version 0.1.7 internally: the release workflow packages from the repository's package config, and that tag pointed to the commit before the version sync, so the file was named 0.1.8 while dpkg recorded 0.1.7. This release republishes with matching versions
2. The release workflow now cross-checks the tag, application version and package version and refuses to publish if they disagree, preventing another mismatched installer

**Updated**

None

## 中文

### 0.1.9 稳定版（0.1.8 → 0.1.9）

**新增**

无

**修复**

1. 修正 0.1.8 发布包内版本号误标为 0.1.7：发版工作流打包时直接读仓库里的包配置，而那次标签打在版本同步提交之前，导致安装包文件名是 0.1.8、系统里登记的版本却是 0.1.7；本版重新出包，安装后版本号与发布版本一致
2. 发版工作流增加包版本核对：标签、应用版本、安装包版本三者必须一致，不一致直接拦截，避免再次放出版本号对不上的安装包

**更新**

无
