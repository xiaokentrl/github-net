package app

// RuntimeInfo 是后端交给前端的真实运行环境信息，不含任何演示数据。
type RuntimeInfo struct {
	AppName        string `json:"appName"`
	Version        string `json:"version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	StartedAt      string `json:"startedAt"`
	StatusSequence int64  `json:"statusSequence"`
}
