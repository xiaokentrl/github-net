package app

import (
	"errors"
	"strings"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 稳定的对外错误码；前端按码判断失败原因，不解析中文文案。
// 码表登记（状态码方案的最小登记表；parity 门 error_codes_parity_test.go 钉住双端）：
// 每码一条「码 | 含义 | 引入版本」。码发布即冻结——改义走新码，永不复用；
// 文案目前随调用点带细节（同一码多种细节），接 {code,params} 协议时再语言包化。
// 登记清单（17 码）：
//
//	empty_token            | 令牌为空 | 0.1.0
//	catalog_invalid        | 内嵌目录数据不合法 | 0.1.0
//	internal_error         | 未知内部错误的兜底码 | 0.1.0
//	invalid_settings       | 设置保存请求不合法 | 0.1.0
//	revision_conflict      | 设置乐观锁冲突 | 0.1.0
//	settings_save_failed   | 设置写盘失败 | 0.1.0
//	config_schema_newer    | 配置 schema 比当前程序新 | 0.1.0
//	unknown_scope          | 加速范围不认识 | 0.1.0
//	strategy_unavailable   | 加速方法不可用/不适用 | 0.1.0
//	invalid_task_id        | 任务请求缺必要信息 | 0.1.0
//	queue_running          | 应用退出中不接受新任务 | 0.1.0
//	invalid_item_id        | 项目 ID 不合法/不在目录 | 0.1.0
//	no_strategy_selected   | 未选择加速方法 | 0.1.0
//	no_detect_result       | 没有检测结果 | 0.1.0
//	helper_unavailable     | 特权辅助服务不可用 | 0.1.0
//	hosts_failed           | 操作系统 Hosts 失败 | 0.1.0
//	autostart_failed       | 开机自启动设置失败 | 0.1.0
//	platform_unsupported   | 平台不支持该功能 | 0.1.0
//
// （update.go 另有 update_* 六码，同登记于该文件头部）
const (
	CodeEmptyToken         = "empty_token"
	CodeCatalogInvalid     = "catalog_invalid"
	CodeInternal           = "internal_error"
	CodeInvalidSettings    = "invalid_settings"
	CodeRevisionConflict   = "revision_conflict"
	CodeSettingsSaveFailed = "settings_save_failed"
	CodeConfigSchemaNewer  = "config_schema_newer"

	// 方法选择这一组的码：前端按码分支，不解析中文。
	// unknown_scope 一种分支：这一屏没有可显示的选择区（没给范围、或范围不在目录里）。
	// strategy_unavailable 一种分支：这次点的方法存不了（没给方法 ID、方法不在注册表、方法不适用本范围）。
	// 同一分支内的几种原因界面动作完全一样，所以共用一个码，具体原因只写进可展示消息。
	CodeUnknownScope        = "unknown_scope"
	CodeStrategyUnavailable = "strategy_unavailable"

	// 队列这一组的码：前端按码分支，不解析中文。
	// invalid_task_id：排队请求缺必要信息；queue_running：队列已经不再收任务。
	// queue_busy：队列里有在途任务挡住这笔直接命令（还原全部、清缓存）——等它跑完再试。
	CodeInvalidTaskID = "invalid_task_id"
	CodeQueueRunning  = "queue_running"
	CodeQueueBusy     = "queue_busy"

	// 检测入口这一组的码：前端按码分支，不解析中文。
	// invalid_item_id：这次要检测的项目 ID 在目录里找不到（多半是过期界面发来的）。
	// no_strategy_selected：这个项目所在的加速范围还没选默认方法，检测被入口挡下，
	//   具体原因（没选、方法已不在注册表、不适用本范围）只写进可展示消息。
	CodeInvalidItemID      = "invalid_item_id"
	CodeNoStrategySelected = "no_strategy_selected"

	// 应用与还原这一组的码：前端按码分支，不解析中文。
	// no_detect_result：已退役（2026-11-13 审计）——应用入口的挡下项改为逐项 skipped 进队列，
	// 不再整批报错。码表发布即冻结，保留登记防后人误当活路径。
	// helper_unavailable：特权辅助服务连不上（直接命令场景；队列任务里同类失败进任务文案）。
	// hosts_failed：服务拒绝或写失败，可展示原因在消息里。
	CodeNoDetectResult    = "no_detect_result"
	CodeHelperUnavailable = "helper_unavailable"
	CodeHostsFailed       = "hosts_failed"

	// 启动与平台这一组的码：前端按码分支，不解析中文。
	// autostart_failed：配置已保存但自启动入口没改成，消息里说清这个区别。
	// platform_unsupported：当前平台没有这个能力，不假装成功。
	CodeAutostartFailed     = "autostart_failed"
	CodePlatformUnsupported = "platform_unsupported"
)

// Error 带稳定错误码。cause 是不导出的字段：它只给日志和 errors.Is 用，
// 不会进 JSON，也不会出现在 Wails 生成的绑定类型里。
// 注意（2026-11-13 审计如实化）：cause 会进 Error() 字符串（「码: 消息: 内部原因」），
// Wails 按这个字符串把错误传给界面——界面上会出现带本地路径/errno 的尾巴。
// 前端按码前缀分支不受影响；内部细节当排障噪音看，别当安全保证。
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
	if errors.Is(err, task.ErrQueueStopped) {
		// 应用正在退出，队列不再收新任务；这不是用户能重试的状态，如实说。
		return NewError(CodeQueueRunning, "应用正在退出，这条检测没有排进队列", err)
	}
	if errors.Is(err, task.ErrInvalidRequest) {
		return NewError(CodeInvalidTaskID, "这条任务请求缺少必要信息，已拒绝排队", err)
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
