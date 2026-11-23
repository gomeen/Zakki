package ai

// 错误分类：应用层按类别映射本地化文案，不透传服务端原文给界面以外的
// 地方。哨兵错误经 fmt.Errorf("%w: …") 包装，errors.Is 可判。

import (
	"errors"
	"time"
)

var (
	// ErrNotConfigured Base URL 或模型为空。
	ErrNotConfigured = errors.New("AI 助理未配置")
	// ErrAuth 密钥无效或无权限（401/403）。
	ErrAuth = errors.New("密钥无效或无权限")
	// ErrNetwork 网络失败或服务端非分类错误（含 5xx）。
	ErrNetwork = errors.New("网络请求失败")
	// ErrParse 模型输出无法解析为建议组。
	ErrParse = errors.New("无法解析模型返回的建议")

	// ErrNoProposal 模型成功应答但没有任何可执行的修改建议
	//（proposals 为空，或类型全部未知被滤掉）。
	ErrNoProposal = errors.New("模型没有给出可执行的修改建议")
	// ErrAnchorNotFound 建议的锚点在笔记中找不到或不唯一。
	ErrAnchorNotFound = errors.New("建议引用的原文在笔记中找不到")
	// ErrCancelled 请求在完成前被取消。
	ErrCancelled = errors.New("请求已取消")
)

// RateLimitError 服务端限流（429）；Wait 是 Retry-After 建议的等待时长，
// 0 表示服务端未给出。
type RateLimitError struct {
	Wait time.Duration
}

func (e *RateLimitError) Error() string {
	if e.Wait > 0 {
		return "请求过于频繁，建议稍后重试"
	}
	return "请求过于频繁"
}
