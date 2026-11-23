package ai

// 客户端与解析的行为测试：httptest 假服务器覆盖成功、追问、错误分类、
// 锚点校验与截断。全部不触网。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testNote = `# 周会纪要
导出功能延到下周，先修同步冲突。
然后大家要记得各自的事情，下周二前弄完。

## 下次
- 看备份还原的结果 @2026-06-05 17:00 ~30m
- 决定导出格式`

// newTestClient 返回指向假服务器的客户端与请求记录器。
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *[]chatRequest) {
	t.Helper()
	var bodies []chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body chatRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, Model: "test-model", HTTP: srv.Client()}), &bodies
}

func replyJSON(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%s}}]}`, mustJSON(content))
}

func mustJSON(v string) string { b, _ := json.Marshal(v); return string(b) }

func TestAskInsertReplaceNewTab(t *testing.T) {
	payload := `{"proposals":[
		{"kind":"insert","summary":"添加提醒","at":"listEnd","lines":["- 和阿哲过还原测试 @2026-06-06 15:00 ~10m"]},
		{"kind":"replace","summary":"整理成待办","anchor":"然后大家要记得各自的事情，下周二前弄完。","lines":["- [ ] 阿哲：补还原测试","- [ ] 小雯：修同步冲突"]},
		{"kind":"newTab","summary":"拆到新标签","tab":"待办","content":"# 待办\n- [ ] 项一"}]}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { replyJSON(w, payload) })
	res, err := c.Ask(context.Background(), AskRequest{Instruction: "整理一下", TabName: "周会", NoteMarkdown: testNote, Now: time.Date(2026, 6, 5, 10, 0, 0, 0, time.Local)})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(res.Proposals) != 3 {
		t.Fatalf("建议数 got %d want 3", len(res.Proposals))
	}
	if res.Proposals[0].Kind != KindInsert || res.Proposals[0].At != AtListEnd {
		t.Errorf("第一条应为 listEnd 插入，got %s/%s", res.Proposals[0].Kind, res.Proposals[0].At)
	}
	if res.Proposals[2].NewTab.Name != "待办" || res.Proposals[2].NewTab.Content == "" {
		t.Errorf("newTab 字段解析错误: %+v", res.Proposals[2].NewTab)
	}
}

func TestAskSendsContext(t *testing.T) {
	c, bodies := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		replyJSON(w, `{"proposals":[{"kind":"insert","at":"docEnd","summary":"x","lines":["- a"]}]}`)
	})
	_, err := c.Ask(context.Background(), AskRequest{
		Instruction: "帮我加提醒", TabName: "周会", NoteMarkdown: testNote,
		Now: time.Date(2026, 6, 5, 10, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("请求数 got %d", len(*bodies))
	}
	req := (*bodies)[0]
	if req.Model != "test-model" || len(req.Messages) != 2 {
		t.Fatalf("请求形状错误: %+v", req)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
		t.Errorf("应请求 json_object 模式")
	}
	if !strings.Contains(req.Messages[0].Content, "Today is 2026-06-05") {
		t.Errorf("系统提示缺少今天日期")
	}
	if !strings.Contains(req.Messages[1].Content, testNote) || !strings.Contains(req.Messages[1].Content, "帮我加提醒") {
		t.Errorf("用户消息应含笔记与指令")
	}
}

func TestAskFollowUpCarriesPrevious(t *testing.T) {
	c, bodies := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		replyJSON(w, `{"proposals":[{"kind":"insert","at":"docEnd","summary":"x","lines":["- a"]}]}`)
	})
	prev := AskResult{Proposals: []Proposal{{Kind: KindInsert, At: AtDocEnd, Summary: "添加提醒", Lines: []string{"- 旧"}}}}
	_, err := c.Ask(context.Background(), AskRequest{
		Instruction: "提前到30分钟", TabName: "周会", NoteMarkdown: testNote,
		FollowUp: &FollowUp{Instruction: "帮我加提醒", Result: prev},
		Now:      time.Date(2026, 6, 5, 10, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	user := (*bodies)[0].Messages[1].Content
	for _, want := range []string{"Previous instruction: 帮我加提醒", "- 旧", "Adjustment: 提前到30分钟"} {
		if !strings.Contains(user, want) {
			t.Errorf("追问上下文缺少 %q", want)
		}
	}
}

func TestParseFencedAndProseWrappedJSON(t *testing.T) {
	fenced := "```json\n{\"proposals\":[{\"kind\":\"insert\",\"at\":\"docEnd\",\"summary\":\"s\",\"lines\":[\"- x\"]}]}\n```"
	if _, err := parseProposals(fenced); err != nil {
		t.Errorf("围栏 JSON 解析失败: %v", err)
	}
	prose := `好的，我的建议：{"proposals":[{"kind":"newTab","summary":"s","tab":"T","content":"c"}]} 以上。`
	if _, err := parseProposals(prose); err != nil {
		t.Errorf("散文包裹 JSON 解析失败: %v", err)
	}
	if _, err := parseProposals("完全不是 JSON"); !errors.Is(err, ErrParse) {
		t.Errorf("纯文本应报 ErrParse，got %v", err)
	}
	if _, err := parseProposals(`{"proposals":[{"kind":"teleport","summary":"s"}]}`); !errors.Is(err, ErrNoProposal) {
		t.Errorf("全部未知类型应报 ErrNoProposal，got %v", err)
	}
	if _, err := parseProposals(`{"proposals":[]}`); !errors.Is(err, ErrNoProposal) {
		t.Errorf("空 proposals 应报 ErrNoProposal，got %v", err)
	}
	if _, err := parseProposals(`{"proposals":[{"kind":"teleport"},{"kind":"insert","at":"docEnd","lines":["x"]}]}`); err != nil {
		t.Errorf("未知类型与有效建议混合时应保留有效项，got %v", err)
	}
}

func TestAskErrorTaxonomy(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		body   string
		want   error
	}{
		{"401", http.StatusUnauthorized, nil, `{"error":{"message":"bad key"}}`, ErrAuth},
		{"403", http.StatusForbidden, nil, "", ErrAuth},
		{"429", http.StatusTooManyRequests, map[string]string{"Retry-After": "17"}, "", nil},
		{"500", http.StatusInternalServerError, nil, "boom", ErrNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.Ask(context.Background(), AskRequest{Instruction: "x", NoteMarkdown: testNote, Now: time.Now()})
			if tc.want == nil {
				var rl *RateLimitError
				if !errors.As(err, &rl) || rl.Wait != 17*time.Second {
					t.Fatalf("应报 RateLimitError(17s)，got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestAskCancelled(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // 挂住直到客户端取消
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := c.Ask(ctx, AskRequest{Instruction: "x", NoteMarkdown: testNote, Now: time.Now()})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("got %v want ErrCancelled", err)
	}
}

func TestValidateAnchors(t *testing.T) {
	dup := "line\nline"
	cases := []struct {
		name string
		res  AskResult
		note string
		want error
	}{
		{"锚点缺失", AskResult{Proposals: []Proposal{{Kind: KindReplace, Anchor: "不存在", Lines: []string{"x"}}}}, testNote, ErrAnchorNotFound},
		{"锚点多次", AskResult{Proposals: []Proposal{{Kind: KindReplace, Anchor: "line", Lines: []string{"x"}}}}, dup, ErrAnchorNotFound},
		{"锚点空白", AskResult{Proposals: []Proposal{{Kind: KindReplace, Anchor: "  ", Lines: []string{"x"}}}}, testNote, ErrAnchorNotFound},
		{"位置非法", AskResult{Proposals: []Proposal{{Kind: KindInsert, At: "middle", Lines: []string{"x"}}}}, testNote, ErrParse},
		{"插入无行", AskResult{Proposals: []Proposal{{Kind: KindInsert, At: AtDocEnd}}}, testNote, ErrParse},
		{"afterAnchor 无锚点", AskResult{Proposals: []Proposal{{Kind: KindInsert, At: AtAfterAnchor, Lines: []string{"x"}}}}, testNote, ErrAnchorNotFound},
		{"newTab 无名", AskResult{Proposals: []Proposal{{Kind: KindNewTab}}}, testNote, ErrParse},
		{"replace 删除合法", AskResult{Proposals: []Proposal{{Kind: KindReplace, Anchor: "- 决定导出格式"}}}, testNote, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.res.Validate(tc.note)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("应通过，got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestAskNotConfigured(t *testing.T) {
	c := New(Config{})
	if _, err := c.Ask(context.Background(), AskRequest{Instruction: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v want ErrNotConfigured", err)
	}
}

func TestAPIKeyHeader(t *testing.T) {
	var gotAuth string
	ok := `{"proposals":[{"kind":"insert","at":"docEnd","summary":"s","lines":["- x"]}]}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		replyJSON(w, ok)
	})
	ask := func(cl *Client) {
		t.Helper()
		if _, err := cl.Ask(context.Background(), AskRequest{Instruction: "x", NoteMarkdown: testNote, Now: time.Now()}); err != nil {
			t.Fatalf("Ask: %v", err)
		}
	}
	// 有密钥：Bearer 头。
	kc := New(Config{BaseURL: c.cfg.BaseURL, Model: c.cfg.Model, HTTP: c.cfg.HTTP,
		APIKey: func(ctx context.Context) (string, error) { return "sk-test", nil }})
	ask(kc)
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization got %q want Bearer sk-test", gotAuth)
	}
	// 无密钥供给（本地 Ollama）：不带 Authorization。
	gotAuth = "sentinel"
	ask(c)
	if gotAuth != "" {
		t.Errorf("无密钥时不应带 Authorization，got %q", gotAuth)
	}
}

func TestTestConnection(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		replyJSON(w, "pong")
	})
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	c2, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if err := c2.TestConnection(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatalf("got %v want ErrAuth", err)
	}
}

func TestTruncateMiddle(t *testing.T) {
	if got := truncateMiddle("short", 100); got != "short" {
		t.Errorf("短文不应截断 got %q", got)
	}
	long := strings.Repeat("字", 101)
	got := truncateMiddle(long, 11)
	if !strings.Contains(got, "…（中间省略") {
		t.Errorf("缺少省略标记: %q", got)
	}
	if n := len([]rune(got)); n > 40 { // 5+5+标记
		t.Errorf("截断后仍过长: %d runes", n)
	}
	if !strings.HasPrefix(got, "字字字字字") || !strings.HasSuffix(got, "字字字字字") {
		t.Errorf("应保留首尾: %q", got)
	}
}

func TestProviderPresets(t *testing.T) {
	if _, ok := ProviderByID("openai"); !ok {
		t.Error("openai 预设缺失")
	}
	for _, p := range Providers {
		if p.BaseURL != "" && strings.HasSuffix(p.BaseURL, "/") {
			t.Errorf("%s BaseURL 不应带尾斜杠", p.ID)
		}
	}
	if Providers[len(Providers)-1].ID != "custom" {
		t.Error("custom 应在最后")
	}
}
