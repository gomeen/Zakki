package notes

// 后端 A：GitHub Contents API（blob sha CAS）。每轮先 GET 最新内容与
// sha，提交时 PUT 带 sha；远端已变化（409/422 不匹配）返回 ErrSyncConflict
// 由同步轮重新 pull 重判。token 经注入的 provider 获取（系统安全存储），
// 绝不写入偏好、笔记文件、日志或 Git（R7）。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultGitHubAPI 是 GitHub Contents API 的根地址；测试注入覆盖。
	DefaultGitHubAPI = "https://api.github.com"
	// GitHubSyncPath 是仓库内笔记文件的默认路径。
	GitHubSyncPath = "zakki.json"
	githubAPIVer   = "2022-11-28"
	githubUA       = "Zakki"
	// githubBodyLimit 限制响应体大小，防止把大文件读进内存。
	githubBodyLimit = 64 << 20
)

// ErrGitHubUnconnected 表示尚未完成 GitHub 授权（无 token）。
var ErrGitHubUnconnected = errors.New("尚未连接 GitHub，请先完成授权")

// GitHubBackend 通过 GitHub Contents API 同步单一文件（后端 A）。
type GitHubBackend struct {
	// Repo 是 owner/name；Path 是仓库内文件路径（默认 zakki.json）；
	// Branch 为空表示默认分支。
	Repo   string
	Path   string
	Branch string
	// Token 返回当前访问令牌；失败即本轮失败（token 只存在于内存与
	// 安全存储，绝不落盘）。
	Token func(ctx context.Context) (string, error)
	// APIBase 与 Client 仅供测试注入；零值使用官方 API 与默认客户端。
	APIBase string
	Client  *http.Client
}

// NewGitHubBackend 构造后端 A；token 为 nil 时每轮报未连接。
func NewGitHubBackend(repo, path string, token func(ctx context.Context) (string, error)) *GitHubBackend {
	if path == "" {
		path = GitHubSyncPath
	}
	return &GitHubBackend{Repo: repo, Path: path, Token: token, APIBase: DefaultGitHubAPI}
}

// BackendID 返回后端 A 的偏好取值。
func (g *GitHubBackend) BackendID() string { return "github" }

func (g *GitHubBackend) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (g *GitHubBackend) apiBase() string {
	if g.APIBase != "" {
		return g.APIBase
	}
	return DefaultGitHubAPI
}

// fileURL 组装 Contents API 的文件地址（路径段逐段转义，支持子目录）。
func (g *GitHubBackend) fileURL() string {
	parts := strings.Split(strings.Trim(g.Path, "/"), "/")
	for i, p := range parts {
		parts[i] = escapePathSeg(p)
	}
	return fmt.Sprintf("%s/repos/%s/contents/%s", g.apiBase(), g.Repo, strings.Join(parts, "/"))
}

func escapePathSeg(seg string) string {
	var b strings.Builder
	for _, r := range seg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		default:
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", by)
			}
		}
	}
	return b.String()
}

// do 执行一次 API 调用并返回状态码与响应体；token 只进 Authorization 头。
func (g *GitHubBackend) do(ctx context.Context, method, url, accept string, body []byte) (int, []byte, error) {
	token, err := g.token(ctx)
	if err != nil {
		return 0, nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVer)
	req.Header.Set("User-Agent", githubUA)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("GitHub 网络请求失败：%w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, githubBodyLimit))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("读取 GitHub 响应失败：%w", err)
	}
	return resp.StatusCode, data, nil
}

func (g *GitHubBackend) token(ctx context.Context) (string, error) {
	if g.Token == nil {
		return "", ErrGitHubUnconnected
	}
	tok, err := g.Token(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(tok) == "" {
		return "", ErrGitHubUnconnected
	}
	return tok, nil
}

// githubAPIError 把非预期状态码转成可展示错误（绝不包含 token）。
func githubAPIError(op string, code int, body []byte) error {
	switch code {
	case http.StatusUnauthorized:
		return errors.New("GitHub 授权失败（token 无效或已过期），请重新连接")
	case http.StatusForbidden:
		return fmt.Errorf("GitHub 拒绝了请求（可能达到速率限制）：%s", bodyExcerpt(body))
	}
	return fmt.Errorf("%s失败（HTTP %d）：%s", op, code, bodyExcerpt(body))
}

func bodyExcerpt(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// Pull 读取远端文件：JSON 响应给 sha 与 base64 内容；超大文件（>1MB）
// 的 content 为空，回退用 raw 媒体类型取完整内容（sha 已在第响应里）。
func (g *GitHubBackend) Pull(ctx context.Context) (content []byte, version string, existed bool, err error) {
	url := g.fileURL()
	if g.Branch != "" {
		url += "?ref=" + escapePathSeg(g.Branch)
	}
	code, body, err := g.do(ctx, http.MethodGet, url, "application/vnd.github+json", nil)
	if err != nil {
		return nil, "", false, err
	}
	if code == http.StatusNotFound {
		// 区分「文件不存在」与「仓库不存在/无权限」：仓库探测一次。
		repoCode, repoBody, repoErr := g.do(ctx, http.MethodGet, g.apiBase()+"/repos/"+g.Repo, "application/vnd.github+json", nil)
		if repoErr == nil && repoCode == http.StatusNotFound {
			return nil, "", false, fmt.Errorf("GitHub 仓库 %s 不存在或当前授权无权访问", g.Repo)
		}
		_ = repoBody
		return nil, "", false, nil
	}
	if code != http.StatusOK {
		return nil, "", false, githubAPIError("读取远端文件", code, body)
	}
	var meta struct {
		SHA      string `json:"sha"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, "", false, fmt.Errorf("解析 GitHub 响应失败：%w", err)
	}
	if meta.SHA == "" {
		return nil, "", false, errors.New("GitHub 响应缺少文件版本号")
	}
	if meta.Content != "" {
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(meta.Content, "\n", ""))
		if err != nil {
			return nil, "", false, fmt.Errorf("解码远端文件失败：%w", err)
		}
		return raw, meta.SHA, true, nil
	}
	code, body, err = g.do(ctx, http.MethodGet, url, "application/vnd.github.raw+json", nil)
	if err != nil {
		return nil, "", false, err
	}
	if code != http.StatusOK {
		return nil, "", false, githubAPIError("读取远端大文件", code, body)
	}
	return body, meta.SHA, true, nil
}

// Push 以 CAS 语义上传文件；expect 为空表示远端应当不存在。409 与
// 422「sha 不匹配」都按 ErrSyncConflict 处理，交由同步轮重新 pull。
func (g *GitHubBackend) Push(ctx context.Context, content []byte, expect string) (string, error) {
	payload := map[string]any{
		"message": fmt.Sprintf("sync: update %s from %s", GitHubSyncPath, deviceName()),
		"content": base64.StdEncoding.EncodeToString(content),
	}
	if expect != "" {
		payload["sha"] = expect
	}
	if g.Branch != "" {
		payload["branch"] = g.Branch
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	code, respBody, err := g.do(ctx, http.MethodPut, g.fileURL(), "application/vnd.github+json", body)
	if err != nil {
		return "", err
	}
	switch {
	case code == http.StatusOK || code == http.StatusCreated:
		var resp struct {
			Content struct {
				SHA string `json:"sha"`
			} `json:"content"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return "", fmt.Errorf("解析 GitHub 上传响应失败：%w", err)
		}
		if resp.Content.SHA == "" {
			return "", errors.New("GitHub 上传响应缺少新版本号")
		}
		return resp.Content.SHA, nil
	case code == http.StatusConflict,
		code == http.StatusUnprocessableEntity &&
			(strings.Contains(string(respBody), "does not match") || strings.Contains(string(respBody), "wasn't supplied")):
		// 远端已被并发修改（或我们以为不存在但远端已出现）：CAS 拒绝。
		return "", ErrSyncConflict
	}
	return "", githubAPIError("上传远端文件", code, respBody)
}
