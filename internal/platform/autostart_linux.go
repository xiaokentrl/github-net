//go:build linux

package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xiaokentrl/github-net/internal/identity"
)

// autostartDesktopName 是自启动入口的文件名，和正式安装的桌面入口同名同身份，
// 用户在「启动应用程序」列表里看到的是同一个名字。
const autostartDesktopName = identity.AppID + ".desktop"

// autostartBinary 是装好的主程序绝对路径（deb 安装位）。自启动需要绝对路径：
// XDG autostart 不按 PATH 找。
const autostartBinary = "/usr/local/bin/" + identity.Slug

// autostartDir 从 XDG 规范取自启动目录：$XDG_CONFIG_HOME/autostart，缺省 ~/.config/autostart。
// 单独拎出来是为了测试能把根指到临时目录，不碰真实的自启动配置。
func autostartDir() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("拿不到用户主目录：%w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "autostart"), nil
}

// LinuxAutostart 是 Autostart 在 Linux 上的实现：写一个 XDG 自启动 desktop 文件。
type LinuxAutostart struct{}

func NewAutostart() Autostart {
	return LinuxAutostart{}
}

func (LinuxAutostart) Enable() error {
	dir, err := autostartDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("建自启动目录失败：%w", err)
	}
	entry := fmt.Sprintf("[Desktop Entry]\n"+
		"Type=Application\n"+
		"Name=%s\n"+
		"Exec=%s\n"+
		"Icon=%s\n"+
		"Terminal=false\n"+
		"X-GNOME-Autostart-enabled=true\n",
		identity.DisplayName, autostartBinary, identity.Slug)
	path := filepath.Join(dir, autostartDesktopName)
	tmp := path + ".tmp"
	// 先写临时再改名：半截的 desktop 文件会被桌面环境当坏项忽略，用户还以为自启动开了。
	if err := os.WriteFile(tmp, []byte(entry), 0o644); err != nil {
		return fmt.Errorf("自启动文件写不进去：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("自启动文件替换失败：%w", err)
	}
	return nil
}

func (LinuxAutostart) Disable() error {
	dir, err := autostartDir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, autostartDesktopName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("移除自启动文件失败：%w", err)
	}
	return nil
}

func (LinuxAutostart) Enabled() (bool, error) {
	dir, err := autostartDir()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(dir, autostartDesktopName))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("自启动状态读不出来：%w", err)
	}
	return true, nil
}
