package app

// 助理条会话：发送前置校验、交付（成功/失败/失效）、建议卡渲染、应用
// （插入/替换/新建标签）、撤销与 Esc 语义。Ask 的网络行为在 internal/ai
// 用假服务器覆盖；这里注入 fakeAsker 或直接调 aiDeliver 驱动状态机。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"zakki/internal/ai"
	"zakki/internal/markdown"
)

const aiTestDoc = `# 周会纪要
导出功能延到下周，先修同步冲突。
然后大家要记得各自的事情，下周二前弄完。

## 下次
- 看备份还原的结果 @2026-06-05 17:00 ~30m
- 决定导出格式`

// fakeAsker 注入固定结果的 Ask；请求经 channel 交给测试（无竞争）。
type fakeAsker struct {
	res ai.AskResult
	err error
	got chan ai.AskRequest
}

func (f *fakeAsker) Ask(_ context.Context, req ai.AskRequest) (ai.AskResult, error) {
	f.got <- req
	return f.res, f.err
}

func aiTestApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.tabs[0].Text = aiTestDoc
	a.invalidateMdCache()
	_ = a.updatePreferences(func(p *preferences) {
		p.AI.Enabled = true
		p.AI.Provider = "openai"
		p.AI.BaseURL = "https://api.openai.com/v1"
		p.AI.Model = "gpt-4o-mini"
	})
	return a
}

// deliver 模拟一次请求完成：把状态摆到在途，再同步交付。
func (a *app) deliverForTest(seq int, instruction string, res ai.AskResult, err error) {
	a.ai.bar.reqSeq = seq
	a.ai.bar.tabID = a.activeTab().ID
	a.ai.bar.docFP = a.activeTab().Text
	a.aiDeliver(seq, instruction, res, err)
}

func TestAIBarOpenCloseAndEsc(t *testing.T) {
	a := aiTestApp(t)
	a.openAIBar()
	if !a.ai.bar.open || a.ai.bar.focus != 1 {
		t.Fatalf("打开后应聚焦输入框: %+v", a.ai.bar)
	}
	tt := ui.NewTester(a.view, 400, 560)
	if !tt.HasText("发送") {
		t.Fatalf("助理条应渲染输入行: %v", tt.Texts())
	}
	// Esc 直接关闭。
	a.aiEsc()
	if a.ai.bar.open {
		t.Fatal("Esc 应关闭助理条")
	}
	// 编辑态优先吃掉 Esc。
	a.openAIBar()
	a.ai.bar.editing, a.ai.bar.editDraft = 0, "- x"
	a.aiEsc()
	if !a.ai.bar.open || a.ai.bar.editing != -1 {
		t.Fatalf("编辑态的 Esc 只退出编辑: %+v", a.ai.bar)
	}
}

func TestAISendRequiresConfig(t *testing.T) {
	a := aiTestApp(t)
	_ = a.updatePreferences(func(p *preferences) { p.AI.Enabled = false })
	a.openAIBar()
	a.ai.bar.input = "帮我整理"
	a.aiSend(a.ai.bar.input)
	if a.ai.bar.phase == aiBarThinking {
		t.Fatal("未配置时不应发起请求")
	}
	if a.ai.bar.errText == "" {
		t.Fatal("未配置时应给行内错误")
	}
}

func TestAIDeliverProposalsAndRender(t *testing.T) {
	a := aiTestApp(t)
	res := ai.AskResult{Proposals: []ai.Proposal{
		{Kind: ai.KindInsert, At: ai.AtListEnd, Summary: "添加提醒",
			Lines: []string{"- 和阿哲过还原测试 @2026-06-06 15:00 ~10m"}},
		{Kind: ai.KindReplace, Summary: "整理成待办", Anchor: "然后大家要记得各自的事情，下周二前弄完。",
			Lines: []string{"- [ ] 阿哲：补还原测试", "- [ ] 小雯：修同步冲突"}},
	}}
	a.openAIBar()
	a.deliverForTest(1, "整理一下", res, nil)
	if a.ai.bar.phase != aiBarReview || len(a.ai.bar.proposals) != 2 {
		t.Fatalf("交付后应进入待审: phase=%d n=%d", a.ai.bar.phase, len(a.ai.bar.proposals))
	}
	if a.ai.bar.followUp == nil || a.ai.bar.followUp.Instruction != "整理一下" {
		t.Fatal("应记录追问上下文")
	}
	tt := ui.NewTester(a.view, 400, 640)
	for _, want := range []string{"插入 · 添加提醒", "替换 · 整理成待办", "应用", "放弃", "编辑原文", "全部应用"} {
		if !tt.HasText(want) {
			t.Fatalf("建议卡缺少 %q: %v", want, tt.Texts())
		}
	}
	if !tt.HasText("插入到最后一个列表项之后") || !tt.HasText("替换所选原文") {
		t.Fatalf("应显示落点说明: %v", tt.Texts())
	}
	// 提醒说明行：解析到令牌。
	if !tt.HasText("2026-06-06 15:00") {
		t.Fatalf("提醒卡应显示提醒时间: %v", tt.Texts())
	}
}

func TestAIDeliverErrorAndStale(t *testing.T) {
	a := aiTestApp(t)
	a.openAIBar()
	a.deliverForTest(1, "x", ai.AskResult{}, ai.ErrAuth)
	if a.ai.bar.phase != aiBarInput || a.ai.bar.errText == "" {
		t.Fatalf("失败应回到输入态并显示错误: %+v", a.ai.bar)
	}
	// 指纹失效：等待期间正文变了（指纹停留在旧全文）。
	a.ai.bar.errText = ""
	a.tabs[0].Text += "\n新行"
	a.ai.bar.reqSeq = 2
	a.ai.bar.tabID = a.activeTab().ID
	a.ai.bar.docFP = aiTestDoc // 发起时的旧全文
	a.aiDeliver(2, "x", ai.AskResult{Proposals: []ai.Proposal{{Kind: ai.KindInsert, At: ai.AtDocEnd, Lines: []string{"- x"}}}}, nil)
	if a.ai.bar.phase == aiBarReview || len(a.ai.bar.proposals) != 0 {
		t.Fatalf("指纹失效应丢弃建议: %+v", a.ai.bar)
	}
	if !strings.Contains(a.ai.bar.errText, "失效") {
		t.Fatalf("应提示失效: %q", a.ai.bar.errText)
	}
}

func TestAIApplyInsertIsUndoableAndFeedsReminders(t *testing.T) {
	a := aiTestApp(t)
	a.openAIBar()
	a.ai.bar.proposals = []ai.Proposal{{
		Kind: ai.KindInsert, At: ai.AtListEnd, Summary: "添加提醒",
		Lines: []string{"- 和阿哲过还原测试 @2026-06-06 15:00 ~10m"},
	}}
	a.ai.bar.phase = aiBarReview
	if !a.aiApplyIndex(0) {
		t.Fatal("应用应成功")
	}
	want := "- 和阿哲过还原测试 @2026-06-06 15:00 ~10m"
	if !strings.Contains(a.activeTab().Text, want) {
		t.Fatalf("应用后应插入提醒行: %q", a.activeTab().Text)
	}
	// 提醒联动：新行能被时间令牌解析（调度器同源）。
	tok, ok := markdown.FindTimeToken(want)
	if !ok || tok.Value.Time.Format("2006-01-02 15:04") != "2026-06-06 15:00" {
		t.Fatalf("提醒行应带可解析令牌: %+v", tok)
	}
	// ⌘Z 可撤销。
	a.undoLive(false)
	if strings.Contains(a.activeTab().Text, want) {
		t.Fatal("撤销应移除插入行")
	}
	// 应用后建议清空、回到输入态。
	if len(a.ai.bar.proposals) != 0 || a.ai.bar.phase != aiBarInput {
		t.Fatalf("应用后应清空待审: %+v", a.ai.bar)
	}
}

func TestAIApplyReplace(t *testing.T) {
	a := aiTestApp(t)
	a.openAIBar()
	anchor := "然后大家要记得各自的事情，下周二前弄完。"
	a.ai.bar.proposals = []ai.Proposal{{
		Kind: ai.KindReplace, Summary: "整理成待办", Anchor: anchor,
		Lines: []string{"- [ ] 阿哲：补还原测试", "- [ ] 我：导出排进下周"},
	}}
	if !a.aiApplyIndex(0) {
		t.Fatal("应用应成功")
	}
	if strings.Contains(a.activeTab().Text, anchor) {
		t.Fatal("原文应被替换")
	}
	if !strings.Contains(a.activeTab().Text, "- [ ] 阿哲：补还原测试") {
		t.Fatalf("应写入待办行: %q", a.activeTab().Text)
	}
	a.undoLive(false)
	if !strings.Contains(a.activeTab().Text, anchor) {
		t.Fatal("撤销应恢复原文")
	}
}

func TestAIApplyAnchorLost(t *testing.T) {
	a := aiTestApp(t)
	a.openAIBar()
	a.ai.bar.proposals = []ai.Proposal{{
		Kind: ai.KindReplace, Anchor: "不存在的原文", Lines: []string{"- x"},
	}}
	if a.aiApplyIndex(0) {
		t.Fatal("锚点丢失时应用应失败")
	}
	if len(a.ai.bar.proposals) != 1 {
		t.Fatal("失败的建议应保留待审")
	}
	if len(a.toasts.pending) == 0 {
		t.Fatal("应给出 toast 提示")
	}
}

func TestAIApplyNewTab(t *testing.T) {
	a := aiTestApp(t)
	before := len(a.tabs)
	a.openAIBar()
	a.ai.bar.proposals = []ai.Proposal{{
		Kind: ai.KindNewTab, Summary: "拆到新标签",
		NewTab: ai.TabSpec{Name: "待办", Content: "# 待办\n- [ ] 项一"},
	}}
	if !a.aiApplyIndex(0) {
		t.Fatal("应用应成功")
	}
	if len(a.tabs) != before+1 {
		t.Fatalf("应新增标签: %d", len(a.tabs))
	}
	nt := a.tabs[len(a.tabs)-1]
	if nt.Name != "待办" || nt.Text != "# 待办\n- [ ] 项一" {
		t.Fatalf("新标签内容错误: %+v", nt)
	}
	if a.active != 0 {
		t.Fatal("不应自动切换标签")
	}
	if len(a.liveUndo) != 0 {
		t.Fatal("新建标签不进当前标签的撤销栈")
	}
}

func TestAIEditProposalMarkdown(t *testing.T) {
	a := aiTestApp(t)
	a.openAIBar()
	a.ai.bar.proposals = []ai.Proposal{{
		Kind: ai.KindInsert, At: ai.AtDocEnd, Lines: []string{"- 旧"},
	}}
	a.aiToggleEdit(0)
	if a.ai.bar.editing != 0 || a.ai.bar.editDraft != "- 旧" {
		t.Fatalf("编辑应以原文播种: %+v", a.ai.bar)
	}
	a.ai.bar.editDraft = "- 新 @2026-06-07 09:00"
	a.aiCommitEdit()
	if a.ai.bar.editing != -1 || a.ai.bar.proposals[0].Lines[0] != "- 新 @2026-06-07 09:00" {
		t.Fatalf("编辑应写回建议: %+v", a.ai.bar.proposals[0])
	}
}

func TestAISwitchTabResetsBar(t *testing.T) {
	a := aiTestApp(t)
	a.openAIBar()
	a.ai.bar.proposals = []ai.Proposal{{Kind: ai.KindInsert, At: ai.AtDocEnd, Lines: []string{"- x"}}}
	a.ai.bar.phase = aiBarReview
	a.switchTab(1)
	if a.ai.bar.open || len(a.ai.bar.proposals) != 0 {
		t.Fatalf("切标签应关闭助理条并丢弃建议: %+v", a.ai.bar)
	}
}

func TestAISendUsesInjectedAsker(t *testing.T) {
	a := aiTestApp(t)
	fake := &fakeAsker{res: ai.AskResult{Proposals: []ai.Proposal{
		{Kind: ai.KindInsert, At: ai.AtDocEnd, Lines: []string{"- ok"}},
	}}, got: make(chan ai.AskRequest, 1)}
	a.ai.asker = fake
	a.openAIBar()
	a.ai.bar.input = "加一条"
	a.aiSend("加一条")
	if a.ai.bar.phase != aiBarThinking {
		t.Fatalf("发送后应进入思考态: %d", a.ai.bar.phase)
	}
	// 后台 goroutine 异步返回：等快照送达（无竞争）。
	var got ai.AskRequest
	select {
	case got = <-fake.got:
	case <-time.After(2 * time.Second):
		t.Fatal("Ask 未在时限内送达")
	}
	if got.Instruction != "加一条" || got.NoteMarkdown != aiTestDoc {
		t.Fatalf("快照应含指令与笔记: %+v", got)
	}
	if got.Now.IsZero() {
		t.Fatal("应传入时钟")
	}
}

func TestAIDefaultOpenAndFocusShortcut(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	if a.ai.bar.open {
		t.Fatal("默认关闭时不应显示助理条")
	}
	// 开启 → 默认显示，但不抢焦点。
	_ = a.updatePreferences(func(p *preferences) {
		p.AI.Enabled, p.AI.BaseURL, p.AI.Model = true, "https://api.openai.com/v1", "m"
	})
	if !a.ai.bar.open || a.ai.bar.focus != 0 {
		t.Fatalf("开启后应默认显示且不抢焦点: %+v", a.ai.bar)
	}
	// 关闭后改无关偏好不重复打开。
	a.closeAIBar()
	_ = a.updatePreferences(func(p *preferences) { p.Window.AlwaysOnTop = true })
	if a.ai.bar.open {
		t.Fatal("无关偏好应用不应重开助理条")
	}
	// 再关再开 → 重新默认显示。
	_ = a.updatePreferences(func(p *preferences) { p.AI.Enabled = false })
	_ = a.updatePreferences(func(p *preferences) { p.AI.Enabled = true })
	if !a.ai.bar.open {
		t.Fatal("从关到开应重新默认显示")
	}
	// ⌘J 聚焦：面板已开时只聚焦输入框。
	a.ai.bar.focus = 0
	a.focusAIBarInput()
	if !a.ai.bar.open || a.ai.bar.focus != 1 {
		t.Fatalf("聚焦应落在输入框: %+v", a.ai.bar)
	}
	// 面板隐藏时 ⌘J 先打开并聚焦。
	a.closeAIBar()
	a.focusAIBarInput()
	if !a.ai.bar.open || a.ai.bar.focus != 1 {
		t.Fatalf("隐藏时应先打开再聚焦: %+v", a.ai.bar)
	}
	// 从开到关：停用 AI 时收起助理条。
	_ = a.updatePreferences(func(p *preferences) { p.AI.Enabled = false })
	if a.ai.bar.open {
		t.Fatal("停用 AI 应收起助理条")
	}
}

func TestAIErrTextRateLimit(t *testing.T) {
	if got := aiErrText(&ai.RateLimitError{Wait: 5e9}); got == "" {
		t.Fatalf("限流应有文案: %q", got)
	}
}
