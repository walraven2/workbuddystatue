package core

import "fmt"

// 错误种类。UI 层据此决定展示文案与图标状态。
const (
	KindUnauthorized     = "unauthorized"       // 登录态失效，需在 WorkBuddy 中重新登录
	KindEndpoint         = "endpoint"           // 接口地址不合法
	KindHTTP             = "http"               // HTTP 状态码异常
	KindAPI              = "api"                // 业务 code 非 0
	KindDecoding         = "decoding"           // 响应解析失败
	KindNetwork          = "network"            // 网络层失败
	KindCredentialMiss   = "credential_missing" // 找不到登录信息文件
	KindCredentialBroken = "credential_broken"  // 登录信息文件存在但读不出令牌
	KindTokenExpired     = "token_expired"      // 令牌已过期
)

// Error 统一错误类型，带中文用户提示
type Error struct {
	Kind   string
	Detail string
}

func (e *Error) Error() string { return e.Detail }

// UserMessage 返回可直接展示给用户的中文说明
func (e *Error) UserMessage() string {
	switch e.Kind {
	case KindUnauthorized:
		return "登录已失效，请在 WorkBuddy 中重新登录"
	case KindEndpoint:
		return "接口地址无效（请检查 config.json 里的 endpoint）"
	case KindCredentialMiss:
		return "未找到 WorkBuddy 登录信息，请先打开并登录 WorkBuddy 桌面端"
	case KindCredentialBroken:
		return "登录信息中没有访问令牌，请在 WorkBuddy 中重新登录"
	case KindTokenExpired:
		return "登录令牌已过期（" + e.Detail + "），请在 WorkBuddy 中重新登录"
	}
	return e.Detail
}

func newError(kind, detail string) *Error {
	return &Error{Kind: kind, Detail: detail}
}

// apiErrorFromCode 把业务错误码翻译成统一错误
func apiErrorFromCode(code int, msg string) *Error {
	if code == 401 || code == 40301 {
		return newError(KindUnauthorized, msg)
	}
	if msg == "" {
		msg = "未知错误"
	}
	return newError(KindAPI, fmt.Sprintf("接口错误 %d：%s", code, msg))
}

// AsError 把任意 error 归一化成 *Error，便于取 UserMessage
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return newError(KindNetwork, err.Error())
}
