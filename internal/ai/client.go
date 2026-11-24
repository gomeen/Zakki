package ai

// OpenAI 兼容的 AI 助理客户端：把一句自然语言指令与当前笔记快照发给
// 聊天补全接口，解析回一组结构化建议（Proposal）。本包是纯逻辑，不依赖
// MyGo 与其他 internal 包；HTTP 客户端与密钥供给均可注入，测试用
// httptest 假服务器。应用层（internal/app）负责把建议渲染成卡片并在
// 用户确认后应用；锚点校验在本包完成，应用层不再重复判断。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultTimeout 非流式请求的默认超时；可经 Config.HTTP 覆盖。
	// 推理型模型（如 MiniMax M3）思考阶段可能超过 60 秒。
	defaultTimeout = 120 * time.Second
	// maxResponseBytes 响应体读取上限，防异常服务端耗尽内存。
	maxResponseBytes = 4 << 20
)

// Config 是构造 Client 的全部输入。BaseURL 已含版本路径（如
// https://api.openai.com/v1），末尾不带斜杠；APIKey 为 nil 表示无鉴权
// （本地 Ollama 场景）。
type Config struct {
	BaseURL string
	Model   string
	APIKey  func(ctx context.Context) (string, error)
	HTTP    *http.Client
}

// Client 是一次配置多次使用的助理客户端；Ask 可并发调用。
type Client struct {
	cfg Config
}

// New 构造客户端；缺省注入 60 秒超时的 HTTP 客户端。
func New(cfg Config) *Client {
	if cfg.HTTP == nil {
		c := *http.DefaultClient
		c.Timeout = defaultTimeout
		cfg.HTTP = &c
	}
	return &Client{cfg: cfg}
}

// chatMessage 是 chat/completions 的消息载荷。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest 是非流式聊天补全请求体。
type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

// chatResponse 只解码用到的字段。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Ask 发送指令与笔记快照，返回经锚点校验的建议组。追问时把上一轮
// 指令与建议一并附上，要求返回整组替换。
func (c *Client) Ask(ctx context.Context, req AskRequest) (AskResult, error) {
	if strings.TrimSpace(c.cfg.BaseURL) == "" || strings.TrimSpace(c.cfg.Model) == "" {
		return AskResult{}, ErrNotConfigured
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	content, err := c.complete(ctx, systemPrompt(req.Now), userPrompt(req), 0, true)
	if err != nil {
		return AskResult{}, err
	}
	res, err := parseProposals(content)
	if err != nil {
		return AskResult{}, err
	}
	if err := res.Validate(req.NoteMarkdown); err != nil {
		return AskResult{}, err
	}
	return res, nil
}

// TestConnection 发一条最小补全请求验证 Base URL、模型与密钥可用。
func (c *Client) TestConnection(ctx context.Context) error {
	if strings.TrimSpace(c.cfg.BaseURL) == "" || strings.TrimSpace(c.cfg.Model) == "" {
		return ErrNotConfigured
	}
	_, err := c.complete(ctx, "You are a connection test. Reply with the single word: pong.", "ping", 8, false)
	return err
}

// ListModels 拉取端点的模型列表（GET /models，OpenAI 兼容）；
// 返回按字典序排序的模型 ID。端点或密钥无效时如实报错。
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	if strings.TrimSpace(c.cfg.BaseURL) == "" {
		return nil, ErrNotConfigured
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	if c.cfg.APIKey != nil {
		key, err := c.cfg.APIKey(ctx)
		if err != nil {
			return nil, err
		}
		if key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := c.cfg.HTTP.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrCancelled
		}
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w（HTTP %d）", ErrAuth, resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("%w: HTTP %d：%s", ErrNetwork, resp.StatusCode, truncateForError(string(data)))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParse, err)
	}
	var ids []string
	for _, m := range out.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: 端点没有返回任何模型", ErrParse)
	}
	sort.Strings(ids)
	return ids, nil
}

// complete 发起一次补全并返回首个 choice 的文本。jsonMode 要求
// response_format json_object（服务端不支持时按 ErrParse 如实上报，
// 由用户换模型或自定义端点）。
func (c *Client) complete(ctx context.Context, sys, user string, maxTokens int, jsonMode bool) (string, error) {
	body := chatRequest{
		Model:       c.cfg.Model,
		Messages:    []chatMessage{{Role: "system", Content: sys}, {Role: "user", Content: user}},
		Temperature: 0.3,
		MaxTokens:   maxTokens,
	}
	if jsonMode {
		body.ResponseFormat = &struct {
			Type string `json:"type"`
		}{Type: "json_object"}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != nil {
		key, err := c.cfg.APIKey(ctx)
		if err != nil {
			return "", err
		}
		if key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := c.cfg.HTTP.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrCancelled
		}
		return "", fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrCancelled
		}
		return "", fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("%w（HTTP %d）", ErrAuth, resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", &RateLimitError{Wait: retryAfter(resp.Header.Get("Retry-After"))}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return "", fmt.Errorf("%w: HTTP %d：%s", ErrNetwork, resp.StatusCode, truncateForError(string(data)))
	}
	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("%w: %v", ErrParse, err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("%w: %s", ErrParse, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%w: 响应没有 choices", ErrParse)
	}
	return out.Choices[0].Message.Content, nil
}

// retryAfter 解析 Retry-After 秒数；缺失或非法返回 0。
func retryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

// truncateForError 截短错误里的响应体，避免状态行被巨量 HTML 撑爆。
func truncateForError(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
