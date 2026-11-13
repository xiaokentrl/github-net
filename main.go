package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	appVersion = "0.1.14"
	statusTick = 2 * time.Second
	appID      = "io.github.xiaokentrl.githubnet"
)

// injectThemeAndLang 把首页 HTML 的 <html> 标签补上配置里的 data-theme 与 lang，
// 返回（正文, 应发状态码；Content-Length 已按新正文设好）。主题读不到（配置服务
// 没就绪）就用默认浅色——白闪一次总好过打不开。正文捕获不完整时返回原样字节——
// 绝不回头重跑 next，那会叠写正文。
func injectThemeAndLang(theme, lang string, next http.Handler, w http.ResponseWriter, r *http.Request) ([]byte, int) {
	cap := &captureResponseWriter{ResponseWriter: w, buf: &bytes.Buffer{}}
	next.ServeHTTP(cap, r)
	html := cap.buf.String()
	at := strings.Index(html, "<html")
	if at < 0 {
		// 找不到 <html（比如回了 404 页）：把捕获到的原样发出去，绝不再跑一遍 next——
		// 重跑会叠写正文。
		return cap.buf.Bytes(), statusOrDefault(cap.status)
	}
	end := strings.Index(html[at:], ">")
	if end < 0 {
		return cap.buf.Bytes(), statusOrDefault(cap.status)
	}
	tag := html[at : at+end+1]
	// 原地替换模板写死的 data-theme/lang（没有才追加）：不依赖重复属性的解析语义，
	// 替换完的一定生效。HTML 同名属性先到先得，追加在尾部会被模板旧值压住。
	themeAttr := regexp.MustCompile(`data-theme="[^"]*"`)
	langAttr := regexp.MustCompile(`lang="[^"]*"`)
	newTag := themeAttr.ReplaceAllString(langAttr.ReplaceAllString(tag, fmt.Sprintf(`lang=%q`, lang)), fmt.Sprintf(`data-theme=%q`, theme))
	if !strings.Contains(newTag, "data-theme=") {
		newTag = strings.TrimSuffix(newTag, ">") + fmt.Sprintf(` data-theme=%q>`, theme)
	}
	if !strings.Contains(newTag, `lang="`) {
		newTag = strings.TrimSuffix(newTag, ">") + fmt.Sprintf(` lang=%q>`, lang)
	}
	out := []byte(html[:at] + newTag + html[at+end+1:])
	// 长度按改写后的正文重设，再按记录的状态码发——顺序不能反。
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	return out, statusOrDefault(cap.status)
}

// statusOrDefault 把「next 没显式写状态码」的 0 归位成 200：HTTP 隐式 200 的等价物。
func statusOrDefault(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}

// captureResponseWriter 拦下正文与状态码供改写；其余头透传给真 ResponseWriter。
// 状态码不能透传：注入会改变正文长度，必须先改写、自设 Content-Length，
// 再按记录的状态码发——WebKit 按先到的 Content-Length 框定正文，长度不改会把
// 注入后的首页截断（对照复核 2026-11-14 高危）。
type captureResponseWriter struct {
	http.ResponseWriter
	buf    *bytes.Buffer
	status int
}

func (n *captureResponseWriter) WriteHeader(status int) {
	if n.status == 0 {
		n.status = status
	}
}

func (n *captureResponseWriter) Write(b []byte) (int, error) { return n.buf.Write(b) }

// windowGeometryPath 给窗口几何落盘文件的路径：与用户配置同目录的 window.json。
// 单独一个文件不走 config 的 revision 语义——拖窗口不该和设置保存抢版本号。
func windowGeometryPath(store *config.Store) (string, error) {
	dir := filepath.Dir(store.Path())
	return filepath.Join(dir, "window.json"), nil
}

// trayThemes 是托盘「主题」子菜单的七套配色（id 与前端 themes.ts、config 校验同一份口径；
// 显示名跟着前端 themes.ts 的中文名，改名字两边一起改）。
// 齐性由 main_test 的 TestTrayThemesCoverAllAllowedThemes 钉住：config 的 allowedThemes 加了新主题而这里忘了，测试红。
var trayThemes = []struct{ id, name string }{
	{"light", "晨光白"},
	{"sakura", "樱花粉"},
	{"forest", "森林绿"},
	{"ocean", "深海蓝"},
	{"midnight", "午夜蓝"},
	{"oled", "极夜黑"},
	{"warm-yellow", "暖阳黄"},
}

func main() {
	// 统一走 XWayland（拍板 2026-11-09）：Wayland 原生会话禁止应用自摆 toplevel 位置，
	// 收托盘、召回、重启后全靠合成器摆位，窗口位置永远保不住；X11 后端下
	// position/move 全部生效，几何落盘与恢复才真正工作。Ubuntu 桌面必带 XWayland。
	// 用户显式设了 GDK_BACKEND 就尊重用户的选择，不覆盖。
	if os.Getenv("GDK_BACKEND") == "" {
		_ = os.Setenv("GDK_BACKEND", "x11")
	}

	// 日志落盘与异常退出检测（日志与可诊断性方案，拍板 2026-11-10）：
	//   桌面启动的应用 stdout 没人看得见——标准 log 与 wails 系统日志全部落到
	//   配置目录旁的 app.log（1MB 轮转、保留一份旧文件）。
	//   运行标记 running.flag 启动时创建、正常退出时删除；残留＝上次异常退出，记进日志。
	logDir := ""
	if cfgPath, err := config.Path(); err == nil {
		logDir = filepath.Dir(cfgPath)
		_ = os.MkdirAll(logDir, 0o700)
	}
	// 运行标记里写 PID：再次启动（单实例握手前的新进程）会先跑到这里——
	// 标记在、且里面那个 PID 还活着＝上一个自己还在跑，不算异常退出，不写误报。
	markerPath := ""
	if logDir != "" {
		markerPath = filepath.Join(logDir, "running.flag")
		if data, err := os.ReadFile(markerPath); err == nil {
			oldPid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			stillAlive := oldPid > 0 && oldPid != os.Getpid() && (func() bool {
				if p, err := os.FindProcess(oldPid); err == nil {
					// nil＝能发信号；EPERM＝进程存在但属其他用户——两种都算「活着」，
					// 只有 ESRCH（进程没了）才算异常退出。判的是 Signal 自己的返回值，
					// 不是 FindProcess 的（那个在这一分支里恒为 nil，判它 EPERM 永远不成立）。
					sigErr := p.Signal(syscall.Signal(0))
					return sigErr == nil || errors.Is(sigErr, syscall.EPERM)
				}
				return false
			})()
			if !stillAlive {
				markerLog, _ := os.OpenFile(filepath.Join(logDir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
				if markerLog != nil {
					fmt.Fprintf(markerLog, "%s 检测到上次异常退出（pid %d 的运行标记残留）\n", time.Now().Format(time.RFC3339), oldPid)
					markerLog.Close()
				}
			} else {
				// 上一个实例还活着（二次启动，单实例握手会把本进程送走）：
				// 不写误报，也不覆写它的标记——标记归属保持第一个实例。
				markerPath = ""
			}
		}
		if markerPath != "" {
			if err := os.WriteFile(markerPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
				markerPath = "" // 标记写不了（目录不可写）就不再依赖它
			}
		}
	}
	var logWriter io.Writer = os.Stderr
	if logDir != "" {
		logWriter = newRotatingWriter(filepath.Join(logDir, "app.log"), 1<<20)
		log.SetOutput(io.MultiWriter(os.Stderr, logWriter))
	}
	// 降级路径（配置目录不可写、没有文件日志）：直接用 stderr，别把 stderr 双写两遍。
	var slogSink io.Writer = logWriter
	if logWriter == io.Writer(os.Stderr) {
		slogSink = os.Stderr
	}
	slogFile := slog.New(slog.NewTextHandler(slogSink, nil))
	// 启动第一行先报身份：之后任何一条日志都能顺着回答「这是哪个版本」
	// （对照复核 2026-11-13：版本此前只在关于页可查）。
	slogFile.Info("应用启动", "版本", appVersion, "系统", runtime.GOOS+"/"+runtime.GOARCH)

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

	// service 声明在资产中间件之前：FOUC 注入的闭包在建窗时就引用它（运行期才赋值）。
	var service *appsvc.Service
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
			// CSP（安全边界方案，拍板 2026-11-10）：前端资源是边界外输入，页面不得
			// 加载任意外部脚本。产物全部走外部样式表（无行内 style、无 data: 图标），
			// 全部收紧到 self；前端构建若引入行内样式或 data: 资源，这里会拦下并暴露。
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Security-Policy",
						"default-src 'none'; script-src 'self'; style-src 'self'; "+
							"img-src 'self'; connect-src 'self'; font-src 'self'")
					// 首帧前注入主题与语言（对照《主题与色彩方案》「冷启动无主题闪烁」）：
					// 只拦首页 HTML，把 <html> 的 data-theme/lang 换成配置里的现值——
					// CSS 按属性选择器取色，页面渲染第一帧就是对的，不再白闪后跳深色。
					// CSP 禁行内脚本，所以走服务端改写而不是前端 bootstrap。
					if r.URL.Path == "/" || r.URL.Path == "/index.html" {
						theme, lang := "light", "zh-CN"
						if service != nil {
							if cfg, err := service.GetSettings(); err == nil {
								theme, lang = cfg.Appearance.Theme, cfg.Appearance.Language
							}
						}
						b, code := injectThemeAndLang(theme, lang, next, w, r)
						w.WriteHeader(code)
						_, _ = w.Write(b)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		Logger: slogFile,
		OnShutdown: func() {
			if cleanup != nil {
				cleanup()
			}
			// 清运行标记放在 cleanup 之后：cleanup 里再出的异常也算异常退出，不漏报。
			if markerPath != "" {
				_ = os.Remove(markerPath)
			}
		},
	})

	// 服务在应用建好之后才装配：任务队列的进度事件要投递到已经存在的事件总线上。
	// RegisterService 只在 Run 之前有效，这里仍在 Run 之前，注册顺序与原来写在 Options 里一致。
	emitProgress := func(name string, payload any) { platformApp.Event.Emit(name, payload) }
	service = appsvc.NewService(appName, appVersion, emitter.Sequence, emitProgress)
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
			// 原子写（复用 config 的 WriteFileAtomic）：断电也只会留下完整旧文件或完整新文件，
			// 不会是半截 JSON——半截会让下次启动的几何恢复静默失效。
			_ = config.WriteFileAtomic(path, data, 0o600)
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

// rotatingWriter 是带轮转的追加日志写入器：单文件超 max 就改名留旧份（保留一份），
// 再开新文件继续追加。单进程单写者，互斥锁只护轮转与追加的并发。
type rotatingWriter struct {
	mu   sync.Mutex
	path string
	max  int64
}

func newRotatingWriter(path string, max int64) *rotatingWriter {
	return &rotatingWriter{path: path, max: max}
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if st, err := os.Stat(w.path); err == nil && st.Size()+int64(len(p)) > w.max {
		_ = os.Remove(w.path + ".1")
		_ = os.Rename(w.path, w.path+".1")
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Write(p)
}
