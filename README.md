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

Linux 只有一条发布线：默认 GTK4/WebKitGTK 6.0，构建宿主为 Ubuntu 26.04（本机即该版本，或等效容器 `ubuntu:26.04`），也就是上面的 `wails3 build`。发布目标是 Debian 13 与 Ubuntu 26.04。Debian 12 不在目标内——它的仓库没有 `webkitgtk-6.0`，默认后端编不过；Ubuntu 24.04 也已于 2026-10-27 经裁决移出，改由 26.04 LTS 取代。`-tags gtk3` 只留作开发者本机备选，官方不为它出包、不承诺修复。

运行 Go 测试：

```sh
go test ./...
```

Linux 构建需要 `libgtk-4-dev libwebkitgtk-6.0-dev`。产物能不能装某台机器，看实测的两道门：用 `objdump -T` 查产物要求的最高 `GLIBC_x.y` 是否不高于 `GLIBC_2.41`（最低目标 Debian 13 的 glibc），用 `ldd` 查目标发行版是否有 `libwebkitgtk-6.0.so.4` 和 `libgtk-4.so.1`。同一份代码在 Ubuntu 26.04 编出的产物要求 `GLIBC_2.38`，放进 Debian 13 容器 `ldd -r` 无未解析符号，所以一台 26.04 宿主可以同时供 Debian 13 与 Ubuntu 26.04 两条线；Debian 13 那边只验过链接，实机运行还没验。Windows 和 macOS 构建需在各自原生 runner 上验证。

## CI

`.github/workflows/ci.yml` 在 push 和 PR 上跑四步：前端 `npm ci && npm run build`（含 tsc）；后端在 `ubuntu:26.04` 容器里查 `gofmt`、`go vet ./...`、`go test -count=1 ./...`；同在 Ubuntu 26.04 宿主用默认后端出 x64 产物并跑门一（≤`GLIBC_2.41`）与本发行版门二；最后把产物放进 `debian:13` 容器复核门二。这四步的命令已在 `ubuntu:26.04` 与 `debian:13` 两个真容器里按 CI 的写法逐条本地跑过（含把门一门槛压到 2.34 的负向对照，确认能正确拦住）；没本地复现的只剩 Actions 自己的调度——容器内 `setup-go` 在线装 Go 与拉模块、跨作业的 artifact 传递，这几步要等首次真跑确认。当前只覆盖 Linux x64，arm64、Windows、macOS 还没有对应任务，所以本地提交前请自行跑 `go build ./... && go vet ./... && go test ./...`、`gofmt -l .`（应为空）和 `frontend` 里的 `npm run build`。
