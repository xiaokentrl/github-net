package main

import (
	"context"
	"embed"
	"log"
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
	linuxName  = "github-net"
)

func main() {
	emitter := &event.StatusEmitter{}
	service := appsvc.NewService(appName, appVersion, emitter.Sequence)
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
			ProgramName: linuxName,
		},
		Services: []application.Service{
			application.NewService(service),
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

	// Go 侧也订阅一次并保存取消函数，用来证明订阅不会随窗口关闭或重启泄漏。
	unsubscribeStatus := platformApp.Event.On(event.RuntimeStatusName, func(e *application.CustomEvent) {
		platformApp.Logger.Info("已收到运行时状态事件", "name", e.Name)
	})
	cleanup = func() {
		cancel()
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
		log.Fatal(err)
	}
}
