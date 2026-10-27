package app

import (
	"errors"
	"strings"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
)

// 稳定的对外错误码；前端按码判断失败原因，不解析中文文案。
const (
	CodeEmptyToken         = "empty_token"
	CodeCatalogInvalid     = "catalog_invalid"
	CodeInternal           = "internal_error"
	CodeInvalidSettings    = "invalid_settings"
	CodeRevisionConflict   = "revision_conflict"
	CodeSettingsSaveFailed = "settings_save_failed"
	CodeConfigSchemaNewer  = "config_schema_newer"
)

// Error 带稳定错误码。cause 是不导出的字段：它只给日志和 errors.Is 用，
// 不会进 JSON，也不会出现在 Wails 生成的绑定类型里，所以前端拿不到内部细节。
type Error struct {
	Code    string
	Message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.Code + ": " + e.Message + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

// Unwrap 让上层能用 errors.Is/errors.As 追到原始错误。
func (e *Error) Unwrap() error { return e.cause }

// NewError 构造带稳定错误码的公开错误；cause 可为空。
func NewError(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

// ToPublicError 把内部错误整理成稳定错误码和可展示消息，底层原因保留在 cause 里供日志使用。
// 未知错误一律归为 internal_error，且不把内部文本透给前端。
func ToPublicError(err error) *Error {
	if err == nil {
		return nil
	}

	var publicErr *Error
	if errors.As(err, &publicErr) {
		return publicErr
	}
	if errors.Is(err, catalog.ErrInvalid) {
		return NewError(CodeCatalogInvalid, "产品目录数据不合法，请重新构建应用", err)
	}
	if errors.Is(err, config.ErrRevisionConflict) {
		return NewError(CodeRevisionConflict, "设置已被别处改过，请刷新后重做这次修改", err)
	}
	if errors.Is(err, config.ErrSchemaTooNew) {
		return NewError(CodeConfigSchemaNewer, "配置文件来自更新的程序版本，请先升级本应用再改设置", err)
	}
	if errors.Is(err, config.ErrSaveFailed) {
		// 底层原因（磁盘、权限、路径）只留在 cause 里，不进可展示消息。
		return NewError(CodeSettingsSaveFailed, "设置没能写入，界面上的值仍是上一次成功保存的内容", err)
	}
	if errors.Is(err, config.ErrEmptyMutationID) {
		return NewError(CodeInvalidSettings, "保存请求缺少幂等键，已拒绝", err)
	}
	if errors.Is(err, config.ErrInvalid) {
		return NewError(CodeInvalidSettings, settingDetail(err), err)
	}
	return NewError(CodeInternal, "操作失败，请稍后重试", err)
}

// settingDetail 取出校验错误里「哪一条不合格」的可展示部分；
// 哨兵前缀是给日志看的，前端只需要具体那一句。
func settingDetail(err error) string {
	message := err.Error()
	prefix := config.ErrInvalid.Error() + ": "
	if strings.HasPrefix(message, prefix) {
		return strings.TrimPrefix(message, prefix)
	}
	return message
}

// 保证 Error 同时满足 error 与可解包接口，签名漂移在编译期就能发现。
var (
	_ error                       = (*Error)(nil)
	_ interface{ Unwrap() error } = (*Error)(nil)
)
