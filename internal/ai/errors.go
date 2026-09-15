package ai

import (
	"context"
	"errors"
)

type ProviderError struct {
	Code      string
	Retryable bool
	Cause     error
}

func (e *ProviderError) Error() string { return e.Code }
func (e *ProviderError) Unwrap() error { return e.Cause }

func NormalizeError(err error) (string, string, bool) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "provider_timeout", "模型响应超时，请稍后重试。", true
	}
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		messages := map[string]string{
			"provider_unauthorized":   "模型服务配置无效，请联系管理员。",
			"provider_rate_limited":   "模型服务繁忙，请稍后重试。",
			"provider_output_invalid": "模型返回内容不符合生成协议，请重试。",
			"provider_error":          "模型服务暂时不可用，请稍后重试。",
		}
		return providerError.Code, messages[providerError.Code], providerError.Retryable
	}
	return "provider_error", "模型服务暂时不可用，请稍后重试。", true
}
