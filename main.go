package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	appsvc "github.com/xiaokentrl/github-net/internal/app"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/task"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var trayIcon []byte

const (
	appName    = "GitHub Net"
	appVersion = "0.1.0"
	statusTick = 2 * time.Second
	appID      = "io.github.xiaokentrl.githubnet"
)

// windowGeometryPath 给窗口几何落盘文件的路径：与用户配置同目录的 window.json。
// 单独一个文件不走 config 的 revision 语义——拖窗口不该和设置保存抢版本号。
func windowGeometryPath(store *config.Store) (string, error) {
	dir := filepath.Dir(store.Path())
	return filepath.Join(dir, "window.json"), nil
}

// trayThemes 是托盘「主题」子菜单的六套配色（id 与前端 themes.ts、config 校验同一份口径；
// 显示名跟着前端 themes.ts 的中文名，改名字两边一起改）。
var trayThemes = []struct{ id, name string }{
	{"light", "晨光白"},
	{"sakura", "樱花粉"},
	{"forest", "森林绿"},
	{"ocean", "深海蓝"},
	{"midnight", "午夜蓝"},
	{"oled", "极夜黑"},
}

func main() {
	// 统一走 XWayland（拍板 2026-11-09）：Wayland 原生会话禁止应用自摆 toplevel 位置，
	// 收托盘、召回、重启后全靠合成器摆位，窗口位置永远保不住；X11 后端下
	// position/move 全部生效，几何落盘与恢复才真正工作。Ubuntu 桌面必带 XWayland。
	// 用户显式设了 GDK_BACKEND 就尊重用户的选择，不覆盖。
	if os.Getenv("GDK_BACKEND") == "" {
		_ = os.Setenv("GDK_BACKEND", "x11")
	}
	emitter := &event.StatusEmitter{}
	ctx, cancel := context.WithCancel(context.Background())
	windowFocusRequests := make(chan struct{}, 1)
	windowReady := make(chan struct{})
	var unsubscribeDockReopen func()
	var unsubscribeStartup func()

	requestWindowFocus := func() {
		select {
		case windowFocusRequests <- struct{}{}:
		default:
		}
	}

	// cleanup 在窗口、托盘和事件订阅建好之后才被赋值，OnShutdown 只能提前捕获这个变量。
	var cleanup func()

	platformApp := application.New(application.Options{
		Name:        appName,
		Description: "GitHub 和 Steam 网络加速工具",
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: appID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				requestWindowFocus()
			},
		},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyRegular,
		},
		Linux: application.LinuxOptions{
			ApplicationID: appID,
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		OnShutdown: func() {
			if cleanup != nil {
				cleanup()
			}
		},
	})

	// 服务在应用建好之后才装配：任务队列的进度事件要投递到已经存在的事件总线上。
	// RegisterService 只在 Run 之前有效，这里仍在 Run 之前，注册顺序与原来写在 Options 里一致。
	emitProgress := func(name string, payload any) { platformApp.Event.Emit(name, payload) }
	service := appsvc.NewService(appName, appVersion, emitter.Sequence, emitProgress)
	// 启动时清一次本机落盘缓存（超过保留期限的一律删掉，拍板 2026-11-05）。放后台跑是因为它可能碰磁盘上
	// 一个卡住的目录，而清缓存这件事不该拦住窗口起来；清失败不吭声也不影响任何功能。
	go appsvc.PruneCaches(service)
	platformApp.RegisterService(application.NewService(service))

	// 启动行为来自用户配置：启动后最小化 → 窗口直接以隐藏方式创建；关窗行为由下面的钩子现读配置。
	// 配置读不到时按「不最小化」走，和应用其他地方「读不到就用默认值并说明」的口径一致。
	startMinimized := false
	if cfg, err := service.GetSettings(); err == nil {
		startMinimized = cfg.Startup.StartMinimized
	}

	unsubscribeDockReopen = platformApp.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		requestWindowFocus()
	})
	unsubscribeStartup = platformApp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		close(windowReady)
	})

	// 窗口几何落盘与恢复（拍板 2026-11-09）：收托盘/退出前把位置、尺寸、最大化记进
	// window.json（与配置同目录），下次建窗时恢复。尺寸和最大化全平台有效；
	// 位置只有 X11 能恢复——Wayland 协议禁止应用自摆 toplevel 位置，合成器说了算。
	type windowGeometry struct {
		X, Y      int
		Width     int
		Height    int
		Maximised bool
	}
	geo := windowGeometry{Width: 1280, Height: 800}
	if geoPath, err := windowGeometryPath(service.SettingsStore()); err == nil {
		if data, err := os.ReadFile(geoPath); err == nil {
			_ = json.Unmarshal(data, &geo)
		}
	}
	// 兜底：记到的是坏值（如窗口还没显示过、多屏切换后的怪数）就回默认尺寸。
	if geo.Width < 400 || geo.Height < 300 {
		geo.Width, geo.Height = 1280, 800
		geo.X, geo.Y, geo.Maximised = 0, 0, false
	}

	window := platformApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            appName,
		Width:            geo.Width,
		Height:           geo.Height,
		BackgroundColour: application.NewRGB(247, 248, 250),
		URL:              "/",
		// 启动后最小化：窗口直接以隐藏方式创建，托盘里照样能唤回。
		Hidden: startMinimized,
		// 标题栏改成界面自己画（原型顶栏那颗「— □ ×」），系统标题栏不再出现；
		// 拖动和四边缩放由 Wails 运行时接：顶栏标 --wails-draggable:drag，缩放条自动加在窗口边缘。
		Frameless: true,
	})
	// 恢复最大化状态：Wayland 上尺寸由 default size 生效，最大化状态这里补。
	if geo.Maximised {
		window.Maximise()
	}
	// 位置恢复只对 X11 有效（Wayland 上 SetPosition 是空操作）；0,0 视为「没记到位置」。
	if geo.X != 0 || geo.Y != 0 {
		window.SetPosition(geo.X, geo.Y)
	}

	// 关窗到托盘：两条关窗路都要走同一套判断——
	// 系统发来的 delete-event（窗口管理器快捷键）走 Linux.WindowDeleteEvent；
	// 自绘标题栏的「×」走 Window.Close()，再到 WindowClosing。
	// 开关只信配置现读——运行中改设置立即生效，不维护第二份状态。
	// 读不到配置按「不拦」处理：关窗是用户明说的动作，别因为读不出偏好把它卡住。
	shouldHideOnClose := func() bool {
		cfg, err := service.GetSettings()
		return err == nil && cfg.Startup.CloseToTray
	}
	// saveWindowGeometry 在窗口即将离开眼前（收托盘、退出）时记下几何。
	// 放这几个时机而不是拖动时逐次写：拖动高频写盘没必要，这几个点必然是「关闭前的样子」。
	saveWindowGeometry := func() {
		w, h := window.Size()
		x, y := window.Position()
		data, err := json.Marshal(windowGeometry{X: x, Y: y, Width: w, Height: h, Maximised: window.IsMaximised()})
		if err != nil {
			return
		}
		if path, err := windowGeometryPath(service.SettingsStore()); err == nil {
			_ = os.WriteFile(path, data, 0o600)
		}
	}
	// 收进托盘用 Minimise 不用 Hide（拍板 2026-11-09）：Wayland 禁止应用自摆 toplevel 位置，
	// Hide 再 Show 会由合成器重新摆位（窗口跳回中间）；最小化不销毁窗口映射，
	// 合成器记得原位置，唤回时原位恢复。X11 上两种方式都保位置，统一走最小化。
	window.RegisterHook(events.Linux.WindowDeleteEvent, func(e *application.WindowEvent) {
		if !shouldHideOnClose() {
			return
		}
		e.Cancel()
		saveWindowGeometry()
		window.Minimise()
	})
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !shouldHideOnClose() {
			return
		}
		e.Cancel()
		saveWindowGeometry()
		window.Minimise()
	})

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-windowReady:
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-windowFocusRequests:
				// 最小化收进托盘的窗口：Show 里的 gtk_window_present 会反最小化并原位恢复；
				// Restore 在 Linux 上是空实现，真正干活的是这条。
				window.Restore()
				window.Show()
				window.Focus()
			}
		}
	}()

	themeCtl := appsvc.NewThemeController(service.SettingsStore())

	tray := platformApp.SystemTray.New()
	tray.SetIcon(trayIcon)
	// 左键单击 = 幂等唤回主窗口（拍板 2026-11-09，对照托盘方案第 8 问）：
	// 不用默认的 ToggleWindow——它会藏窗口还会按托盘位置重新定位；
	// 只 Restore+Show+Focus，点多少次窗口都不动位置。右键菜单照旧。
	tray.OnClick(requestWindowFocus)
	// 双击显式走同一条唤回：有些宿主把双击拆成两次单击，幂等操作点几遍都一样；
	// 也防宿主给了独立双击回调却落到默认行为上。
	tray.OnDoubleClick(requestWindowFocus)

	// 托盘菜单：显示/隐藏/退出 + 「主题」子菜单（六套，当前项勾选）。
	// 切主题走配置服务保存并向前端发 settings:changed；菜单整体重建让勾选跟到新主题上。
	var buildTrayMenu func() *application.Menu
	buildTrayMenu = func() *application.Menu {
		menu := application.NewMenu()
		menu.Add("显示主窗口").OnClick(func(*application.Context) { requestWindowFocus() })
		menu.Add("最小化到托盘").OnClick(func(*application.Context) {
			saveWindowGeometry()
			window.Minimise()
		})
		menu.AddSeparator()
		themeMenu := menu.AddSubmenu("主题")
		current := ""
		if cfg, err := service.GetSettings(); err == nil {
			current = cfg.Appearance.Theme
		}
		for _, th := range trayThemes {
			id, name := th.id, th.name
			item := themeMenu.AddCheckbox(name, id == current)
			item.OnClick(func(*application.Context) {
				cfg, err := themeCtl.Set(id)
				if err != nil {
					platformApp.Logger.Error("托盘切主题失败", "theme", id, "err", err)
					return
				}
				platformApp.Event.Emit(event.SettingsChangedName, event.SettingsChanged{Config: cfg})
				tray.SetMenu(buildTrayMenu())
			})
		}
		menu.AddSeparator()
		menu.Add("退出").OnClick(func(*application.Context) {
			saveWindowGeometry()
			platformApp.Quit()
		})
		return menu
	}
	tray.SetMenu(buildTrayMenu())

	tray.OnClick(func() {
		if window.IsVisible() {
			window.Hide()
			return
		}
		requestWindowFocus()
	})

	// 托盘提示跟着队列走（原型胶囊那颗「运行中」小圆点的等价物：系统托盘图标是静态图，
	// 颜色改不了，只能把「有没有在跑」写进提示文字）。只在句子变了才下发，免得每出一条
	// 进度就打扰一次桌面环境。
	tooltip := trayTooltipText(appName, 0)
	tray.SetTooltip(tooltip)
	unsubscribeTaskProgress := platformApp.Event.On(event.TaskProgressName, func(*application.CustomEvent) {
		next := trayTooltipText(appName, runningTasks(service.GetTasks()))
		if next == tooltip {
			return
		}
		tooltip = next
		tray.SetTooltip(next)
	})

	// 启动后自动检查一次更新（技术规划 §7）：只比较和提示，不下载、不安装。
	// 结论经 update:status 事件推给界面；失败也说清楚原因，不把失败装成「已是最新」。
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-windowReady:
		}
		info, err := service.CheckForUpdates()
		notice := event.UpdateNotice{}
		if err != nil {
			if appErr, ok := err.(*appsvc.Error); ok {
				notice.Error = appErr.Message
			} else {
				notice.Error = "更新检查失败"
			}
		} else {
			notice.Info = &info
		}
		platformApp.Event.Emit(event.UpdateStatusName, notice)
	}()

	// 启动后自动检测 → 自动应用 → 自动退出（设置中心那三个开关）。
	// 同样等窗口起来再跑：链上的每一步都往事件总线推状态，总线得先在。
	// 一个主菜单都没勾时这条链整个不开；结论另存一份在 GetAutomationStatus 里，
	// 界面挂载晚了也能补读到「刚才跑过什么、为什么没退出」。
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-windowReady:
		}
		appsvc.StartStartupAutomation(ctx, service, platformApp.Quit, emitProgress)
	}()

	// Go 侧也订阅一次并保存取消函数，用来证明订阅不会随窗口关闭或重启泄漏。
	unsubscribeStatus := platformApp.Event.On(event.RuntimeStatusName, func(e *application.CustomEvent) {
		platformApp.Logger.Info("已收到运行时状态事件", "name", e.Name)
	})
	cleanup = func() {
		// 退出前记一次窗口几何：不是从托盘收起而是真退出的路（关窗退出/退出菜单之外的
		// 信号、崩溃前 由 OnShutdown 触发的），也要拿到「关闭前的样子」。窗口此刻未销毁。
		saveWindowGeometry()
		cancel()
		appsvc.ShutdownTaskQueue(service)
		unsubscribeTaskProgress()
		unsubscribeStatus()
		unsubscribeDockReopen()
		unsubscribeStartup()
		tray.Destroy()
	}

	go func() {
		emit := func() {
			platformApp.Event.Emit(event.RuntimeStatusName, emitter.Next(window.IsVisible()))
		}
		ticker := time.NewTicker(statusTick)
		defer ticker.Stop()
		emit()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				emit()
			}
		}
	}()

	if err := platformApp.Run(); err != nil {
		log.Fatal(startupFailureText(err))
	}
}

// trayTooltipText 决定托盘提示文字：有活在跑就写清几项，空闲只写应用名。
// 原型胶囊用一颗会闪的小圆点表示运行中，原生托盘图标是静态图，等价物只有这句提示。
func trayTooltipText(appName string, running int) string {
	if running <= 0 {
		return appName
	}
	return fmt.Sprintf("%s · 进行中 %d 项", appName, running)
}

// runningTasks 数快照里正在执行的任务：检测、应用、恢复都算，排队和已完成的不算。
func runningTasks(snap task.Snapshot) int {
	count := 0
	for _, t := range snap.Tasks {
		if t.Status == task.StatusRunning || t.Status == task.StatusRestoring {
			count++
		}
	}
	return count
}

// startupFailureText 把启动失败说成一句能照着查的话。
// 原来只印一句底层英文错误：从桌面图标点进来的人看不到 stderr，只会以为双击没反应。
// 最常见的一种原因是这台会话没有 DBus 会话总线——单实例锁要靠它把二次启动转给已运行的实例，
// 拿不到就当场报错退出。这里只在能确认时补这句原因；
// 变量在但总线已经死了的情况判不出来，那时照原样带出底层错误，不编原因。
// 提示只落到 stderr：图形对话框要等界面真的起来才谈得上，这一层还没有能力弹框。
func startupFailureText(err error) string {
	message := "界面没能启动：" + err.Error()
	if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		message += "（这个会话没有 DBus 会话总线，单实例锁拿不到它；从桌面环境里启动，或先在终端设好 DBUS_SESSION_BUS_ADDRESS）"
	}
	return message
}
