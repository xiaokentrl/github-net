# GitHub Net

GitHub Net 是一个用 Go、Wails、React 和 TypeScript 构建的桌面应用。当前仓库已建立工程骨架，功能按规划逐步接入。

## 开发环境

- Go 1.27.3 或更高版本
- Wails v3.0.0
- Node.js 和 npm

## 开发和构建

```sh
wails3 dev
```

```sh
wails3 build
```

Linux 只有一条发布线：默认 GTK4/WebKitGTK 6.0，构建宿主为 Debian 13 稳定版（或等效容器），即上面的 `wails3 build`。Debian 12 不在发布目标内——它的仓库没有 `webkitgtk-6.0`，默认后端编不过。`-tags gtk3` 只留作开发者本机备选，官方不为它出包、不承诺修复。

运行 Go 测试：

```sh
go test ./...
```

Linux 构建需要 `libgtk-4-dev libwebkitgtk-6.0-dev`。产物能不能装某台机器，看实测的两道门：用 `objdump -T` 查产物要求的最高 `GLIBC_x.y` 是否不高于 `GLIBC_2.39`（最低目标 Ubuntu 24.04 的 glibc），用 `ldd` 查目标发行版是否有 `libwebkitgtk-6.0.so.4` 和 `libgtk-4.so.1`。这两项与编译在哪台机器无关，本机 Ubuntu 26.04 直接构建的包只用于调试。Windows 和 macOS 构建需在各自原生 runner 上验证。
