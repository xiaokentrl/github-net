## English

### github-net 0.1.2 Stable (0.1.1 → 0.1.2)

**Added**

1. Add local SQLite persistence for detection results: each round is recorded as a ledger, with a per-hostname+IP cache pool keeping the latest usable addresses
2. Add three public hosts-source methods (GitCDN/HelloGitHub/GitLab) plus an aggregate "All sources" method, which is the default for the GitHub scope
3. Add a "Use stored IPs" method that re-verifies cached usable addresses without re-fetching sources
4. Group the method dropdown with numbered sources and add a Detect button that runs only the selected method
5. Add a title-bar tray pill for quick theme switching, minimize-to-tray and quit, with a pulsing dot while tasks run
6. Replace the language dropdown with two buttons (Simplified/Traditional) in appearance settings

**Fixed**

1. Fix the settings dialog stealing focus from text inputs on background events
2. Fix invisible option text in native dropdowns on the light theme caused by system-following popups
3. Fix segmented-button hover overriding the selected state and hiding its label
4. Fix the "applied IP" metric not refreshing after apply or restore
5. Move the config file to ~/.config/github-net/config.json with automatic migration from the old location

**Updated**

1. Trim UI copy: remove placeholder hints, show only data-backed metrics and relative check time
2. Update defaults: collapsed menu, no selection, compact queue height, focus-follow on, and reordered categories
3. Update hosts writing: separate the managed block with a blank line that is removed again on restore
4. Remove the Pages and Gist submenus (their domains are not covered by public sources)

## 中文

### 0.1.2 稳定版（0.1.1 → 0.1.2）

**新增**

1. 新增候选结论落盘：每轮检测整笔记进本机 SQLite 底账，候选池按「域名+IP」缓存最近一轮的可用地址
2. 新增三个公共 hosts 源加速方法（GitCDN/HelloGitHub/GitLab）和汇总方法「全部公共源」，GitHub 范围默认使用后者
3. 新增「使用已有储存」方法：直接用缓存里的可用地址重新验证，不重新拉源
4. 方法下拉分组显示，公共源按优先序编号；下拉旁新增「检测」按钮，只测选中的方法
5. 标题栏新增托盘胶囊：主题色点快切、最小化到托盘、退出，圆点随任务运行脉冲
6. 设置中心外观页语言改为简体/繁体两颗按钮

**修复**

1. 修复设置对话框抢焦点：后台事件一进来输入框就失去焦点
2. 修复浅色主题下原生下拉弹层跟系统走深色导致悬停文字看不见
3. 修复分段按钮悬停覆盖选中态样式导致白字不可见
4. 修复应用/还原后详情页「当前写入 IP」不刷新
5. 配置文件迁移到 ~/.config/github-net/config.json，旧位置自动搬家

**更新**

1. 更新界面文案与布局：删除占位提示与说明小字，指标格改为有数据来源的真指标，检测完成时间改相对时间
2. 更新默认值：菜单折叠、默认无勾选、队列高度小档、焦点跟随打开；分类排序调整
3. 更新启动行为：写入 Hosts 前先空一行与原有内容分隔，还原时一并收走
4. 删除 Pages 和 Gist 两个子菜单（公共源不覆盖其域名）
