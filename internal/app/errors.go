package app

// Error 带稳定错误码，前端按码判断失败原因，不解析中文文案。
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}
