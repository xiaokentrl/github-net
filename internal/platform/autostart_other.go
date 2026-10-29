//go:build !linux

package platform

// 其他平台的自启动还没实现。按包注释的约定，这里给明确的「不支持」，
// 不做半吊子的假实现；界面拿到 ErrUnsupported 要如实告诉用户。
type unsupportedAutostart struct{}

func (unsupportedAutostart) Enable() error          { return ErrUnsupported }
func (unsupportedAutostart) Disable() error         { return ErrUnsupported }
func (unsupportedAutostart) Enabled() (bool, error) { return false, ErrUnsupported }

func NewAutostart() Autostart {
	return unsupportedAutostart{}
}
