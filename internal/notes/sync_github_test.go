package notes

// 后端 A（GitHub Contents API）与 Device Flow 的契约测试：全部走
// httptest 假服务器，绝不触网；token 断言只核对 Authorization 头形状，
// 不让真实凭据进入测试。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newGitHubServer 起一个假 GitHub API：记录请求头与最近 PUT 载荷。
func newGitHubServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func newTestBackend(repo string, token func(context.Context) (string, error), srv *httptest.Server) *GitHubBackend {
	b := NewGitHubBackend(repo, "", token)
	b.APIBase = srv.URL
	b.Client = srv.Client()
	return b
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestGitHubPullBase64(t *testing.T) {
	content := docBytes("云端内容")
	srv, _ := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/contents/zakki.json") {
			t.Errorf("请求路径错误: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("Authorization = %q", got)
		}
		writeJSON(w, 200, map[string]any{
			"sha":      "abc123",
			"content":  base64.StdEncoding.EncodeToString(content) + "\n",
			"encoding": "base64",
		})
	})
	be := newTestBackend("gomeen/notes", func(context.Context) (string, error) { return "tok-1", nil }, srv)
	got, version, existed, err := be.Pull(context.Background())
	if err != nil || !existed || version != "abc123" || string(got) != string(content) {
		t.Fatalf("Pull = %q %q %v %v", got, version, existed, err)
	}
}

func TestGitHubPullMissing(t *testing.T) {
	for _, tc := range []struct {
		name        string
		repoStatus  int
		wantExisted bool
		wantErr     string
	}{
		{"文件缺失仓库存在", 200, false, ""},
		{"仓库不存在", 404, false, "仓库"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/contents/") {
					w.WriteHeader(404)
					return
				}
				writeJSON(w, tc.repoStatus, map[string]string{"full_name": "gomeen/notes"})
			})
			be := newTestBackend("gomeen/notes", func(context.Context) (string, error) { return "t", nil }, srv)
			_, _, existed, err := be.Pull(context.Background())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("应报 %s: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil || existed != tc.wantExisted {
				t.Fatalf("Pull = %v %v", existed, err)
			}
		})
	}
}

func TestGitHubPullLargeFileRawFallback(t *testing.T) {
	content := docBytes("大文件内容")
	srv, _ := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "application/vnd.github.raw+json" {
			_, _ = w.Write(content)
			return
		}
		// >1MB 的文件 JSON 响应只有 sha，content 为空。
		writeJSON(w, 200, map[string]any{"sha": "big", "content": "", "encoding": "none"})
	})
	be := newTestBackend("gomeen/notes", func(context.Context) (string, error) { return "t", nil }, srv)
	got, version, existed, err := be.Pull(context.Background())
	if err != nil || !existed || version != "big" || string(got) != string(content) {
		t.Fatalf("大文件回退失败: %q %q %v %v", got, version, existed, err)
	}
}

func TestGitHubPushAndCAS(t *testing.T) {
	var putBody map[string]any
	srv, _ := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Push 应用 PUT: %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&putBody)
		writeJSON(w, 200, map[string]any{"content": map[string]string{"sha": "new-sha"}})
	})
	be := newTestBackend("gomeen/notes", func(context.Context) (string, error) { return "t", nil }, srv)
	version, err := be.Push(context.Background(), docBytes("新内容"), "old-sha")
	if err != nil || version != "new-sha" {
		t.Fatalf("Push = %q %v", version, err)
	}
	if putBody["sha"] != "old-sha" || putBody["content"] != base64.StdEncoding.EncodeToString(docBytes("新内容")) {
		t.Fatalf("PUT 载荷错误: %v", putBody)
	}
}

func TestGitHubPushConflictAndErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
		wantMsg string
	}{
		{"409 冲突", 409, `{"message":"conflict"}`, ErrSyncConflict, ""},
		{"422 sha 不匹配", 422, `{"message":"... does not match ..."}`, ErrSyncConflict, ""},
		{"401 授权失败", 401, `{"message":"Bad credentials"}`, nil, "授权失败"},
		{"422 其他错误", 422, `{"message":"invalid content"}`, nil, "invalid content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, tc.status, json.RawMessage(tc.body))
			})
			be := newTestBackend("gomeen/notes", func(context.Context) (string, error) { return "t", nil }, srv)
			_, err := be.Push(context.Background(), docBytes("x"), "sha")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("应返回 %v: %v", tc.wantErr, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("应包含 %q: %v", tc.wantMsg, err)
			}
		})
	}
}

func TestGitHubTokenErrors(t *testing.T) {
	srv, _ := newGitHubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("无 token 时不应发起请求")
	})
	be := newTestBackend("gomeen/notes", nil, srv)
	if _, _, _, err := be.Pull(context.Background()); !errors.Is(err, ErrGitHubUnconnected) {
		t.Fatalf("无 token 应报未连接: %v", err)
	}
	empty := newTestBackend("gomeen/notes", func(context.Context) (string, error) { return "  ", nil }, srv)
	if _, _, _, err := empty.Pull(context.Background()); !errors.Is(err, ErrGitHubUnconnected) {
		t.Fatalf("空 token 应报未连接: %v", err)
	}
}

func TestEscapePathSeg(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"zakki.json", "zakki.json"},
		{"a b.json", "a%20b.json"},
		{"目录", "%E7%9B%AE%E5%BD%95"},
	} {
		if got := escapePathSeg(tc.in); got != tc.want {
			t.Fatalf("escapePathSeg(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// 子目录路径逐段转义后再以 / 连接。
	be := NewGitHubBackend("o/r", "笔记/zakki.json", nil)
	if got, want := be.fileURL(), DefaultGitHubAPI+"/repos/o/r/contents/%E7%AC%94%E8%AE%B0/zakki.json"; got != want {
		t.Fatalf("fileURL = %q, want %q", got, want)
	}
}

func TestDeviceFlowStartAndPoll(t *testing.T) {
	var pollCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			if r.Header.Get("Accept") != "application/json" {
				t.Errorf("Accept 头错误: %q", r.Header.Get("Accept"))
			}
			writeJSON(w, 200, map[string]any{
				"device_code":      "dev-1",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://github.com/login/device",
				"expires_in":       900,
				"interval":         5,
			})
		case "/login/oauth/access_token":
			pollCount++
			switch pollCount {
			case 1:
				writeJSON(w, 200, map[string]string{"error": "authorization_pending"})
			case 2:
				writeJSON(w, 200, map[string]string{"error": "slow_down"})
			default:
				writeJSON(w, 200, map[string]any{
					"access_token":  "gho_ok",
					"refresh_token": "ghr_ok",
					"expires_in":    600,
				})
			}
		default:
			t.Errorf("未知端点: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	flow := &DeviceFlow{Client: srv.Client(), DeviceURL: srv.URL + "/login/device/code", TokenURL: srv.URL + "/login/oauth/access_token"}
	start, err := flow.Start(context.Background(), "client-1")
	if err != nil || start.DeviceCode != "dev-1" || start.UserCode != "ABCD-1234" || start.Interval != 5*time.Second {
		t.Fatalf("Start = %+v %v", start, err)
	}
	if _, state, err := flow.Poll(context.Background(), "client-1", start.DeviceCode); err != nil || state != FlowPending {
		t.Fatalf("首轮应 Pending: %d %v", state, err)
	}
	if _, state, _ := flow.Poll(context.Background(), "client-1", start.DeviceCode); state != FlowSlowDown {
		t.Fatalf("次轮应 SlowDown: %d", state)
	}
	tok, state, err := flow.Poll(context.Background(), "client-1", start.DeviceCode)
	if err != nil || state != FlowDone || tok.AccessToken != "gho_ok" || tok.RefreshToken != "ghr_ok" {
		t.Fatalf("终态应 Done: %+v %d %v", tok, state, err)
	}
	if tok.ExpiresAt.IsZero() {
		t.Fatal("expires_in 应换算成绝对时间")
	}
}

func TestDeviceFlowFailures(t *testing.T) {
	t.Run("缺 client_id", func(t *testing.T) {
		if _, err := NewDeviceFlow().Start(context.Background(), " "); err == nil {
			t.Fatal("缺 client_id 应报错")
		}
	})
	t.Run("用户拒绝", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]string{"error": "access_denied", "error_description": "denied"})
		}))
		defer srv.Close()
		flow := &DeviceFlow{Client: srv.Client(), DeviceURL: srv.URL, TokenURL: srv.URL}
		_, state, err := flow.Poll(context.Background(), "c", "d")
		if state != FlowDenied || err == nil {
			t.Fatalf("应 Denied 且报错: %d %v", state, err)
		}
	})
	t.Run("设备码过期", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]string{"error": "expired_token"})
		}))
		defer srv.Close()
		flow := &DeviceFlow{Client: srv.Client(), DeviceURL: srv.URL, TokenURL: srv.URL}
		_, state, err := flow.Poll(context.Background(), "c", "d")
		if state != FlowExpired || err == nil {
			t.Fatalf("应 Expired: %d %v", state, err)
		}
	})
	t.Run("刷新轮换", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, map[string]any{"access_token": "gho_new", "refresh_token": "ghr_new", "expires_in": 300})
		}))
		defer srv.Close()
		flow := &DeviceFlow{Client: srv.Client(), DeviceURL: srv.URL, TokenURL: srv.URL}
		tok, err := flow.Refresh(context.Background(), "c", "ghr_old")
		if err != nil || tok.AccessToken != "gho_new" || tok.RefreshToken != "ghr_new" {
			t.Fatalf("刷新 = %+v %v", tok, err)
		}
	})
	t.Run("刷新缺 refresh_token", func(t *testing.T) {
		if _, err := NewDeviceFlow().Refresh(context.Background(), "c", ""); err == nil {
			t.Fatal("缺 refresh_token 应报错")
		}
	})
}

func TestTokenProviderRefresh(t *testing.T) {
	const oldAccess = "gho_old"
	const newAccess = "gho_new"
	flow := &DeviceFlow{}
	// 借 httptest 构造刷新端点；TokenProvider 只用 Refresh。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"access_token": newAccess, "refresh_token": "ghr_new", "expires_in": 600})
	}))
	defer srv.Close()
	flow.TokenURL = srv.URL
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	k := &memKeyring{}
	if err := SaveSyncToken(k, TokenSet{AccessToken: oldAccess, RefreshToken: "ghr_old", ExpiresAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	provider := TokenProvider(k, "client", flow, func() time.Time { return now })
	got, err := provider(context.Background())
	if err != nil || got != newAccess {
		t.Fatalf("过期应刷新: %q %v", got, err)
	}
	tok, err := LoadSyncToken(k)
	if err != nil || tok.AccessToken != newAccess || tok.RefreshToken != "ghr_new" {
		t.Fatalf("刷新后应回存新令牌: %+v %v", tok, err)
	}
	// 未过期直接返回访问令牌，不触网。
	if err := SaveSyncToken(k, TokenSet{AccessToken: oldAccess, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	provider = TokenProvider(k, "client", flow, func() time.Time { return now })
	if got, err := provider(context.Background()); err != nil || got != oldAccess {
		t.Fatalf("未过期应直接返回: %q %v", got, err)
	}
	// 过期且无 refresh token：如实报错。
	if err := SaveSyncToken(k, TokenSet{AccessToken: oldAccess, ExpiresAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider(context.Background()); err == nil || !strings.Contains(err.Error(), "重新连接") {
		t.Fatalf("过期且无刷新令牌应报错: %v", err)
	}
}

func TestSyncTokenEnvelope(t *testing.T) {
	k := &memKeyring{}
	if _, err := LoadSyncToken(k); !errors.Is(err, ErrKeyringEmpty) {
		t.Fatalf("空存储应报 ErrKeyringEmpty: %v", err)
	}
	if err := SaveSyncToken(k, TokenSet{}); err == nil {
		t.Fatal("空访问令牌应拒绝保存")
	}
	want := TokenSet{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}
	if err := SaveSyncToken(k, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSyncToken(k)
	if err != nil || got.AccessToken != want.AccessToken || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("往返不一致: %+v %v", got, err)
	}
	if err := ClearSyncToken(k); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSyncToken(k); !errors.Is(err, ErrKeyringEmpty) {
		t.Fatalf("删除后应为空: %v", err)
	}
	if err := ClearSyncToken(k); err != nil {
		t.Fatalf("重复删除应成功: %v", err)
	}
}
