package main

import (
	"context"
	"embed"
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

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
)

func main() {
	emitter := &event.StatusEmitter{}
	service := appsvc.NewService(appName, appVersion, emitter.Sequence)

	// cleanup 在窗口、托盘和事件订阅建好之后才被赋值，OnShutdown 只能提前捕获这个变量。
	var cleanup func()

	platformApp := application.New(application.Options{
		Name:        appName,
		Description: "GitHub 和 Steam 网络加速工具",
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

	window := platformApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            appName,
		Width:            1280,
		Height:           800,
		BackgroundColour: application.NewRGB(247, 248, 250),
		URL:              "/",
	})

	tray := platformApp.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip(appName)

	menu := application.NewMenu()
	menu.Add("显示主窗口").OnClick(func(*application.Context) { window.Show() })
	menu.Add("隐藏主窗口").OnClick(func(*application.Context) { window.Hide() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { platformApp.Quit() })
	tray.SetMenu(menu)

	tray.OnClick(func() {
		if window.IsVisible() {
			window.Hide()
			return
		}
		window.Show()
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Go 侧也订阅一次并保存取消函数，用来证明订阅不会随窗口关闭或重启泄漏。
	unsubscribeStatus := platformApp.Event.On(event.RuntimeStatusName, func(e *application.CustomEvent) {
		platformApp.Logger.Info("已收到运行时状态事件", "name", e.Name)
	})

	cleanup = func() {
		cancel()
		unsubscribeStatus()
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
