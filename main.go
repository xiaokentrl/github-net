package main

import (
	"context"
	"embed"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	appsvc "github.com/xiaokentrl/github-net/internal/app"
	"github.com/xiaokentrl/github-net/internal/event"
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

func main() {
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

	window := platformApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            appName,
		Width:            1280,
		Height:           800,
		BackgroundColour: application.NewRGB(247, 248, 250),
		URL:              "/",
		// 启动后最小化：窗口直接以隐藏方式创建，托盘里照样能唤回。
		Hidden: startMinimized,
	})

	// 关窗到托盘：Linux 上 GTK 的 delete-event 先到这个钩子，Cancel 掉默认销毁、改把窗口藏起来，
	// 应用继续跑（托盘还在）。开关只信配置现读——运行中改设置立即生效，不维护第二份状态。
	// 读不到配置按「不拦」处理：关窗是用户明说的动作，别因为读不出偏好把它卡住。
	window.RegisterHook(events.Linux.WindowDeleteEvent, func(e *application.WindowEvent) {
		cfg, err := service.GetSettings()
		if err != nil || !cfg.Startup.CloseToTray {
			return
		}
		e.Cancel()
		window.Hide()
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
				window.Restore()
				window.Show()
				window.Focus()
			}
		}
	}()

	tray := platformApp.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip(appName)

	menu := application.NewMenu()
	menu.Add("显示主窗口").OnClick(func(*application.Context) { requestWindowFocus() })
	menu.Add("隐藏主窗口").OnClick(func(*application.Context) { window.Hide() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { platformApp.Quit() })
	tray.SetMenu(menu)

	tray.OnClick(func() {
		if window.IsVisible() {
			window.Hide()
			return
		}
		requestWindowFocus()
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

	// Go 侧也订阅一次并保存取消函数，用来证明订阅不会随窗口关闭或重启泄漏。
	unsubscribeStatus := platformApp.Event.On(event.RuntimeStatusName, func(e *application.CustomEvent) {
		platformApp.Logger.Info("已收到运行时状态事件", "name", e.Name)
	})
	cleanup = func() {
		cancel()
		appsvc.ShutdownTaskQueue(service)
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
