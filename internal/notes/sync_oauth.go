package notes

// GitHub Device Flow（后端 A 的授权入口）：用户在浏览器输入 user code
// 后，应用轮询拿到 user access token 与 refresh token。令牌只经返回值
// 交给系统安全存储，绝不写入偏好、zakki.json、日志或 Git（R7）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	githubDeviceCodeURL = "https://github.com/login/device/code"
	githubTokenURL      = "https://github.com/login/oauth/access_token"
	// githubMinInterval 是 slow_down 后的保底轮询间隔（规范要求 +5s）。
	githubMinInterval = 5 * time.Second
)

// DeviceFlowState 是单次轮询的结论；Pending/SlowDown 由调用方继续排程。
type DeviceFlowState int

const (
	FlowPending  DeviceFlowState = iota // 用户尚未在浏览器完成授权
	FlowSlowDown                        // 轮询过快：按返回间隔 +5s 重试
	FlowDone                            // 授权成功，TokenSet 有效
	FlowDenied                          // 用户拒绝授权
	FlowExpired                         // device_code 已过期，需重新发起
)

// DeviceFlowStart 是发起 Device Flow 的结果：引导用户打开 VerificationURI
// 并输入 UserCode，然后按 Interval 轮询直到 Done/Denied/Expired。
type DeviceFlowStart struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresAt       time.Time
}

// TokenSet 是授权或刷新得到的令牌集合。AccessToken 立即生效；
// RefreshToken 仅在启用过期令牌的 GitHub App 上返回。
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// DeviceFlow 封装 Device Flow 端点与 HTTP 客户端（测试注入 httptest）。
type DeviceFlow struct {
	Client    *http.Client
	DeviceURL string
	TokenURL  string
	// Now 供测试注入；nil 使用 time.Now。
	Now func() time.Time
}

// NewDeviceFlow 返回指向官方端点的流程实例。
func NewDeviceFlow() *DeviceFlow {
	return &DeviceFlow{DeviceURL: githubDeviceCodeURL, TokenURL: githubTokenURL}
}

func (d *DeviceFlow) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (d *DeviceFlow) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Start 请求设备码与用户码。GitHub App 只需 client_id（权限来自安装）。
func (d *DeviceFlow) Start(ctx context.Context, clientID string) (DeviceFlowStart, error) {
	if strings.TrimSpace(clientID) == "" {
		return DeviceFlowStart{}, errors.New("缺少 GitHub App Client ID（ZAKKI_GITHUB_CLIENT_ID）")
	}
	form := url.Values{"client_id": {clientID}}
	body, err := d.postForm(ctx, d.DeviceURL, form)
	if err != nil {
		return DeviceFlowStart{}, err
	}
	var resp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		ErrorDesc       string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return DeviceFlowStart{}, fmt.Errorf("解析 Device Flow 响应失败：%w", err)
	}
	if resp.Error != "" {
		return DeviceFlowStart{}, fmt.Errorf("发起 Device Flow 失败：%s（%s）", resp.Error, resp.ErrorDesc)
	}
	if resp.DeviceCode == "" || resp.UserCode == "" || resp.VerificationURI == "" {
		return DeviceFlowStart{}, errors.New("Device Flow 响应缺少必需字段")
	}
	interval := time.Duration(resp.Interval) * time.Second
	if interval <= 0 {
		interval = githubMinInterval
	}
	expires := time.Duration(resp.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	return DeviceFlowStart{
		DeviceCode:      resp.DeviceCode,
		UserCode:        resp.UserCode,
		VerificationURI: resp.VerificationURI,
		Interval:        interval,
		ExpiresAt:       d.now().Add(expires),
	}, nil
}

// Poll 轮询一次授权结果；返回的 state 指示下一步动作。
func (d *DeviceFlow) Poll(ctx context.Context, clientID, deviceCode string) (TokenSet, DeviceFlowState, error) {
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	return d.tokenRequest(ctx, form)
}

// Refresh 用 refresh_token 换新令牌（GitHub App 启用过期令牌时）；刷新
// 后 refresh_token 轮换，调用方必须回存新集合。
func (d *DeviceFlow) Refresh(ctx context.Context, clientID, refreshToken string) (TokenSet, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return TokenSet{}, errors.New("缺少 refresh token，请重新连接 GitHub")
	}
	form := url.Values{
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	tok, state, err := d.tokenRequest(ctx, form)
	if err != nil {
		return TokenSet{}, err
	}
	if state != FlowDone {
		return TokenSet{}, fmt.Errorf("刷新令牌失败（状态 %d），请重新连接 GitHub", int(state))
	}
	return tok, nil
}

func (d *DeviceFlow) tokenRequest(ctx context.Context, form url.Values) (TokenSet, DeviceFlowState, error) {
	body, err := d.postForm(ctx, d.TokenURL, form)
	if err != nil {
		return TokenSet{}, FlowPending, err
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return TokenSet{}, FlowPending, fmt.Errorf("解析令牌响应失败：%w", err)
	}
	switch resp.Error {
	case "":
	case "authorization_pending":
		return TokenSet{}, FlowPending, nil
	case "slow_down":
		return TokenSet{}, FlowSlowDown, nil
	case "access_denied":
		return TokenSet{}, FlowDenied, errors.New("用户拒绝了 GitHub 授权")
	case "expired_token":
		return TokenSet{}, FlowExpired, errors.New("设备码已过期，请重新发起连接")
	default:
		return TokenSet{}, FlowPending, fmt.Errorf("GitHub 授权失败：%s（%s）", resp.Error, resp.ErrorDesc)
	}
	if resp.AccessToken == "" {
		return TokenSet{}, FlowPending, errors.New("GitHub 令牌响应缺少 access_token")
	}
	tok := TokenSet{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken}
	if resp.ExpiresIn > 0 {
		tok.ExpiresAt = d.now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	return tok, FlowDone, nil
}

// postForm 提交表单并返回响应体（Accept JSON；token 不进入任何日志）。
func (d *DeviceFlow) postForm(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", githubUA)
	resp, err := d.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub 授权请求失败：%w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 GitHub 授权响应失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 授权请求失败（HTTP %d）：%s", resp.StatusCode, bodyExcerpt(data))
	}
	return data, nil
}
