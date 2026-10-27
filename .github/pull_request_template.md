# 说明

- 这次改了什么、为什么改：

## 必查项

- [ ] 改了 `internal/identity/identity.go` 里「户口」层的值（产品 ID、包名与二进制名、数据目录名、Hosts 区段标记，或由它们拼出的安装路径）——这等于改用户机器上已经存在的东西，必须在本 PR 里写清：
  - ① 新旧同时存在的那个时刻会发生什么（旧图标、旧包、已写的 Hosts 区段、已存的配置目录各会怎样）
  - ② 迁移路径：谁在什么时候把旧的变成新的
  - ③ 迁移失败怎么回来
  没改就勾掉这条：[ ] 本 PR 没动户口层的值
- [ ] **第二道金标准**：免密 root 助手 `scripts/github-net-root-install.sh` 是「唯一手写源」的安全例外——它不 source 仓库的库、也不调 `go`（免密 sudo 会在没人看内容的情况下执行它，而仓库目录用户可写），包名按字面写死在它的 `PKG="github-net"` 这一行，别处只引用这个变量。所以改户口值时必须**三处一起改**：`identity.go` 的常量、`identity_test.go` 的金标准、root 助手的 `PKG` 那一行。`TestNotDuplicatedOutsideIdentity` 逐字核对这三处，实得值不等于就红——两个方向都实测过：只改常量不改脚本 ⇒ 红；只改脚本里的 `PKG` 或卸载命令、常量和金标准不动 ⇒ 也红。这是设计，不是测试坏了；本 PR 若动了户口值，请把这三处的 diff 都在描述里点名。
- [ ] 没有在 `internal/identity` 之外新抄一份产品名、路径或标记字符串（需要取值就用 `go run ./cmd/identity --key 名字`，脚本用 `--shell` 且只 eval 一次）
- [ ] 脚本里每个 `rm -rf`、`dpkg --purge`、`apt`、写 sudoers 之前都调了 `identity_guard`（`scripts/github-net-root-install.sh` 是唯一例外，它不 source 仓库里的库，理由见那个文件的开头）
- [ ] `gofmt -l .` 干净、`go vet ./...` 通过、`go test -count=1 ./...` 通过
- [ ] 涉及打包或安装的改动，已在本机实际打过一次包并装上跑过

## 没做的事（如果有，写出来）

-
