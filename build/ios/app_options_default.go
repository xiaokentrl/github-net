//go:build ignore

// 这份模板文件不参与任何构建：iOS 不是发布目标，而它一旦在本机生效，
// build/ios 就成了没有 func main 的 main 包，go build ./... 直接失败。
package main

import "github.com/wailsapp/wails/v3/pkg/application"

// modifyOptionsForIOS is a no-op on non-iOS platforms
func modifyOptionsForIOS(opts *application.Options) {
	// No modifications needed for non-iOS platforms
}
