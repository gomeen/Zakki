package app

import (
	"bytes"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"zakki/internal/notes"
)

// TestMain 兜底声明测试进程的界面语言。注意 MyGo 在包初始化阶段就解析并
// 缓存界面语言，这里的环境变量为时已晚——真正生效的是 CI 在 make test /
// make race 步骤注入的 MYGO_LOCALE=zh-Hans（见 .github/workflows/ci.yml）；
// 本地系统语言非中文的机器请以 MYGO_LOCALE=zh-Hans go test 运行。
func TestMain(m *testing.M) {
	os.Setenv("MYGO_LOCALE", "zh-Hans")
	os.Exit(m.Run())
}

// newTestApp 测试用 app：持久化写进 t.TempDir()，时钟固定，不碰真实数据。
// 路径与时钟都经接缝注入（见 app.notes / app.now），没有全局开关。
func newTestApp(tb testing.TB) *app {
	tb.Helper()
	// 手账字体栈先本地注册，文本布局永不触达系统按需字体下载。
	registerHandFont()
	dir := tb.TempDir()
	a := newAppAt(dir)
	// 同步凭据的「系统安全存储」替换为每应用独立的内存实现：测试永不
	// 接触真实钥匙串（工厂在 Cleanup 还原，包内测试串行执行）。
	oldKeyringFactory := systemKeyringFactory
	systemKeyringFactory = func(string) (notes.Keyring, error) { return &fakeTestKeyring{}, nil }
	tb.Cleanup(func() { systemKeyringFactory = oldKeyringFactory })
	tb.Cleanup(a.notes.Close)
	a.prefs.Storage.SaveIntervalMS = 100 // 等待落盘的测试不必等默认 2 秒；校验下限 100
	a.mdActive = 0                       // 编辑机制测试默认从首个焦点块开始（生产环境启动是预览态 -1）
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	a.setTestNow(func() time.Time { return base })
	a.invalidateMdCache()
	return a
}

// setTestNow 测试注入时钟：app 与 notes.Store 共用同一函数。
func (a *app) setTestNow(now func() time.Time) {
	a.now = now
	a.notes.SetClock(now)
}

// TestView 以无窗口方式跑视图：点切换、字数统计、预览与待办勾选。
func TestView(t *testing.T) {
	a := newTestApp(t)
	a.tabs[0].Text = "hello world"
	tt := ui.NewTester(a.view, 400, 540)

	if !tt.HasText("标签 1") {
		t.Errorf("初始应显示标签 1，texts %q", tt.Texts())
	}
	if !tt.HasText("1 行 · 2 词 · 11 字符") {
		t.Errorf("应显示行/词/字符统计，texts %q", tt.Texts())
	}
	if !tt.HasText("尚未保存") {
		t.Errorf("未保存时应显示尚未保存，texts %q", tt.Texts())
	}

	// 标签 1 的圆点按钮（Label 为「标签 1」）与状态栏文案不冲突，
	// 点击用 Label 精确命中的是按钮。
	if err := tt.Click("标签 2"); err != nil {
		t.Fatal(err)
	}
	if a.active != 1 {
		t.Errorf("点击标签 2 后 active=%d，应为 1", a.active)
	}
	if !tt.HasText("0 行 · 0 词 · 0 字符") {
		t.Errorf("标签 2 为空应显示 0 行，texts %q", tt.Texts())
	}

	// 实时预览：渲染块可点击切换焦点块，待办可勾选回写源文本。
	a.tabs[1].Text = "- [ ] 买牛奶\n\n再点一段"
	a.resyncLive(0)
	tt.Frame() // 直接改状态后手动触发一帧（应用内由 Window.Update 触发）
	if err := tt.Click("再点一段"); err != nil {
		t.Fatal(err)
	}
	if a.mdActive != 2 || a.mdDraft != "再点一段" {
		t.Errorf("点击渲染块后应切换焦点块：active=%d draft=%q", a.mdActive, a.mdDraft)
	}
	if err := tt.Click("完成：买牛奶"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.tabs[1].Text, "[x]") {
		t.Errorf("勾选后源文本应变为 [x]，得到 %q", a.tabs[1].Text)
	}

	// 源码模式切换（底栏眼睛图标，⌘⇧P 同）；提示显示点击后进入的状态。
	if err := tt.Click("源码"); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.Source {
		t.Error("点击源码后应进入源码模式")
	}
	if err := tt.Click("实时预览"); err != nil {
		t.Fatal(err)
	}
	if a.cfg.Source {
		t.Error("再次点击应回到实时预览")
	}

	// 搜索条开合（⌘F，底栏不再有搜索图标）。
	tt.Key(ui.Cmd, ui.KeyF)
	if !a.findOpen {
		t.Error("点击搜索后应打开查找条")
	}
}

func TestHeaderClosePreservesNotes(t *testing.T) {
	for _, source := range []bool{false, true} {
		t.Run(map[bool]string{false: "实时预览", true: "源码"}[source], func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			a.cfg.Source = source
			a.active = 2
			a.tabs[2].Text = "关闭窗口后要保留的内容"
			a.resyncLive(0)
			before := append([]tab(nil), a.tabs...)
			tt := ui.NewTester(a.view, 400, 540)

			tt.ClickAt(22, tabTrackHeight/2)
			tt.Frame()
			if !reflect.DeepEqual(a.tabs, before) || a.active != 2 {
				t.Fatalf("点击左上角关闭按钮修改了笔记：tabs=%+v active=%d", a.tabs, a.active)
			}
			if !tt.HasText("关闭窗口") || tt.HasText("清空当前标签") {
				t.Fatalf("顶栏应提供关闭窗口按钮：%q", tt.Texts())
			}
		})
	}
}

// TestStatusBarActions 底栏只保留源码开关；搜索入口走 ⌘F 与菜单。
func TestStatusBarActions(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.tabs[0].Text = ""
	tt := ui.NewTester(func(c *ui.Context) {
		a.statusBar(c, c.Theme(), themeIsDark(c.Theme()))
	}, 400, 36)
	want := []string{"0 行 · 0 词 · 0 字符", "尚未保存", "源码"}
	if got := tt.Texts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("底栏内容=%q，要 %q", got, want)
	}
	if err := tt.Click("源码"); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.Source {
		t.Error("源码切换应继续可用")
	}
}

// TestLiveTypingFlow 端到端：真实视图 + 键盘事件流模拟Typora 输入
// （打字 → Enter 分段 → 点击渲染块切换焦点 → 继续输入 → 退格合并）。
func TestLiveTypingFlow(t *testing.T) {
	a := newTestApp(t)
	a.tabs[0].Text = "# 标题\n\n第一段"
	tt := ui.NewTester(a.view, 400, 540)

	// 光标在首块（标题源码），按 End 到块尾再 Enter → 硬分段出第二块。
	if !tt.HasText("第一段") {
		t.Fatalf("应渲染第一段，texts %q", tt.Texts())
	}
	// 点击渲染的"第一段"块 → 切换为焦点块（所见即所得、可输入）
	if err := tt.Click("第一段"); err != nil {
		t.Fatal(err)
	}
	if a.mdActive != 2 || a.mdDraft != "第一段" {
		t.Fatalf("点击后焦点块 = %d draft=%q", a.mdActive, a.mdDraft)
	}
	// 键盘输入 + Enter 分段
	tt.Type("X")
	tt.Key(0, ui.KeyEnter)
	tt.Type("Y")
	if want := "# 标题\n\n第一段X\n\nY"; a.tabs[0].Text != want {
		t.Fatalf("输入流文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
	// 此时光标在最后一块 "Y"：先删字符，再一次退格合并整个段落边界。
	tt.Key(0, ui.KeyBackspace) // 删 Y，块变空
	if want := "# 标题\n\n第一段X\n\n"; a.tabs[0].Text != want {
		t.Fatalf("退格删 Y 文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
	tt.Key(0, ui.KeyBackspace) // 块首：一次合并进 "第一段X"，不经过源码分隔空行
	if want := "# 标题\n\n第一段X"; a.tabs[0].Text != want {
		t.Fatalf("退格合并文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
}

// TestEnterMovesOneLine 段末回车只下一行：源文本是 "\n\n" 硬分段（Typora 语义，
// 保证后续输入不会并回同段），但段落分隔空行块不占可见高度 ——
// 光标紧贴文本下一行，而不是隔着一个 20pt 空行“多跳一行”。
func TestEnterMovesOneLine(t *testing.T) {
	a := newTestApp(t)
	a.tabs[0].Text = "甲乙丙丁"
	tt := ui.NewTester(a.view, 400, 540)

	// 光标置段尾，回车一次。
	a.selSyncStart, a.selSyncEnd = 4, 4
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if want := "甲乙丙丁\n\n"; a.tabs[0].Text != want {
		t.Fatalf("回车后文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdActive != 2 || a.mdDraft != "" {
		t.Fatalf("回车后焦点块 = %d draft=%q, 要 2/空", a.mdActive, a.mdDraft)
	}
	// 视觉契约：光标在文本紧邻下一行（间隙 ≤ 一个行距），不是隔一个空行。
	text, ok := tt.Find("甲乙丙丁")
	if !ok {
		t.Fatal("找不到渲染的段落文本")
	}
	caret, ok := tt.TextCaret()
	if !ok {
		t.Fatal("找不到光标")
	}
	if gap := caret.Y - (text.Y + text.H); gap < -2 || gap > 26 {
		t.Errorf("回车后光标距文本 %.1fpt，应约 20pt（一个行距）", gap)
	}
}

// TestSpliceBlocksConsistent 随机编辑序列下，增量拼接结果必须与全量解析一致。
func TestSpliceBlocksConsistent(t *testing.T) {
	// 拼接逻辑极其易错（新旧行号坐标、接缝合并、坐标错位），历史上一系列缺陷
	// 都由本测试抓出；多种子大量随机编辑是它唯一的正确性保障，勿缩减规模。
	for _, seed := range []int64{42, 1, 2026} {
		t.Run("", func(t *testing.T) { runSpliceFuzz(t, seed, 6000) })
	}
}

func TestSpliceBlocksTreatsOverIndentedFenceAsParagraphContinuation(t *testing.T) {
	oldDoc := "first\n\nparagraph\n\nlast"
	newDoc := "first\n\nparagraph\n    ```\n\nlast"
	oldLines := strings.Split(oldDoc, "\n")
	got, _, _ := spliceBlocks(oldDoc, oldLines, liveBlocks(oldDoc), newDoc)
	want := liveBlocks(newDoc)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("过度缩进围栏在接缝处必须仍是段落续行:\n增量=%+v\n全量=%+v", got, want)
	}
}

func runSpliceFuzz(t *testing.T, seed int64, steps int) {
	rng := rand.New(rand.NewSource(seed))
	tokens := []string{
		"# 标题", "正文一行", "", "- 列表", "- [ ] 待办", "> 引用", "```", "code", "---",
		"**粗体** 与 `code`", "普通文字 ok", "1. 有序", "- ", "x", "中文内容测试",
	}
	oldDoc, oldBlocks, valid := "", []liveBlock(nil), false
	prevDoc, prevLines := "", []string(nil)
	for step := 0; step < steps; step++ {
		switch rng.Intn(4) {
		case 0: // 末尾追加
			doc := oldDoc + tokens[rng.Intn(len(tokens))]
			if rng.Intn(2) == 0 {
				doc += "\n"
			}
			oldDoc = doc
		case 1: // 随机位置插入一行
			lines := strings.Split(oldDoc, "\n")
			i := rng.Intn(len(lines))
			lines = append(lines[:i], append([]string{tokens[rng.Intn(len(tokens))]}, lines[i:]...)...)
			oldDoc = strings.Join(lines, "\n")
		case 2: // 随机行改写
			lines := strings.Split(oldDoc, "\n")
			lines[rng.Intn(len(lines))] = tokens[rng.Intn(len(tokens))]
			oldDoc = strings.Join(lines, "\n")
		case 3: // 随机删一行
			lines := strings.Split(oldDoc, "\n")
			if len(lines) > 1 {
				i := rng.Intn(len(lines))
				lines = append(lines[:i], lines[i+1:]...)
				oldDoc = strings.Join(lines, "\n")
			}
		}
		argOld, argBlocks, argLines := prevDoc, oldBlocks, prevLines
		if !valid {
			argOld, argBlocks, argLines = "", nil, nil
		}
		got, _, _ := spliceBlocks(argOld, argLines, argBlocks, oldDoc)
		want := liveBlocks(oldDoc)
		if !reflect.DeepEqual(got, want) {
			for i := 0; i < len(got) || i < len(want); i++ {
				if i >= len(got) {
					t.Fatalf("step %d prev=%q doc=%q\n块 %d 增量缺失，全量=%+v\n增量末尾=%+v", i, prevDoc, oldDoc, i, want[i], got[max(0, len(got)-3):])
				}
				if i >= len(want) {
					t.Fatalf("step %d prev=%q doc=%q\n块 %d 增量多出=%+v", i, prevDoc, oldDoc, i, got[i:])
				}
				if !sameBlock(got[i], want[i]) {
					t.Fatalf("step %d prev=%q doc=%q\n块 %d 不一致:\n 增量=%+v\n 全量=%+v\n 增量前后=%+v", i, prevDoc, oldDoc, i, got[i], want[i], got[max(0, i-2):min(i+3, len(got))])
				}
			}
			t.Fatalf("step %d 不一致但未定位", step)
		}
		oldBlocks, valid = got, true
		prevDoc, prevLines = oldDoc, strings.Split(oldDoc, "\n")
	}
}

// TestMarkdown 覆盖行内解析、块级解析与待办勾选。
func TestMarkdown(t *testing.T) {
	spans := inlineSpans("a **bold** `code` [link](https://x.y) *i*")
	kinds := map[spanKind]bool{}
	for _, sp := range spans {
		kinds[sp.kind] = true
	}
	if !kinds[spanBold] || !kinds[spanCode] || !kinds[spanLink] || !kinds[spanItalic] {
		t.Errorf("行内标记解析不全: %+v", spans)
	}

	blocks := parseBlocks("# 标题\n\n正文一段\n\n- 列表\n- [ ] 待办\n> 引用\n```\ncode\n```\n---\n")
	var got []blockKind
	for _, b := range blocks {
		got = append(got, b.kind)
	}
	want := []blockKind{bkHeading, bkPara, bkList, bkTask, bkQuote, bkCode, bkRule}
	if len(got) != len(want) {
		t.Fatalf("块数 %d != %d: %+v", len(got), len(want), blocks)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("块 %d = %v, 要 %v", i, got[i], want[i])
		}
	}

	// 待办翻转。
	if got := toggleTaskLine("- [ ] a\n- [x] b", 0); !strings.Contains(got, "[x] a") {
		t.Errorf("翻转第 0 行: %q", got)
	}
}

// TestLiveBlocks 检查实时预览的块切分：围栏、空行、列表、段落。
func TestLiveBlocks(t *testing.T) {
	doc := "# 标题\n\n段落一\n段落二\n\n- 甲\n- [x] 乙\n> 引用\n```go\ncode line\n```\n---\n\n"
	blocks := liveBlocks(doc)
	var got []blockKind
	var raws []string
	for _, b := range blocks {
		got = append(got, b.kind)
		raws = append(raws, b.raw)
	}
	wantKinds := []blockKind{bkHeading, bkBlank, bkPara, bkBlank, bkList, bkTask, bkQuote, bkCode, bkRule, bkBlank, bkBlank}
	if len(got) != len(wantKinds) {
		t.Fatalf("块数 %d != %d：%+v raws=%q", len(got), len(wantKinds), got, raws)
	}
	for i := range wantKinds {
		if got[i] != wantKinds[i] {
			t.Errorf("块 %d = %v, 要 %v (raws=%q)", i, got[i], wantKinds[i], raws)
		}
	}
	if blocks[2].text != "段落一 段落二" {
		t.Errorf("段落软换行应拼成一段: %q", blocks[2].text)
	}
	if len(blocks[7].lines) != 1 || blocks[7].lines[0] != "code line" {
		t.Errorf("代码块内容行: %q", blocks[7].lines)
	}

	// 未闭合围栏到文档尾。
	open := liveBlocks("```\nx\ny")
	if len(open) != 1 || open[0].kind != bkCode || open[0].end != 2 {
		t.Errorf("未闭合围栏: %+v", open)
	}

	// 空文档至少有一个空行块。
	if bs := liveBlocks(""); len(bs) != 1 || bs[0].kind != bkBlank {
		t.Errorf("空文档应有单个空行块: %+v", bs)
	}

	// rune 偏移定位块。
	bs := liveBlocks("甲\n\n乙")
	if idx, off := blockOfRuneOffset(bs, 3); idx != 2 || off != 3 {
		t.Errorf("blockOfRuneOffset(3) = (%d,%d), 要 (2,3)", idx, off)
	}
	if idx, _ := blockOfRuneOffset(bs, 0); idx != 0 {
		t.Errorf("blockOfRuneOffset(0) = %d, 要 0", idx)
	}
}

// TestLiveEditing 覆盖提交草稿、分段、列表续行与跨块（live_edit.go 逻辑层）。
func TestLiveEditing(t *testing.T) {
	a := newTestApp(t)

	// Enter 分段：段中回车 → "\n\n" 硬分段，光标落到新段首。
	a.tabs[0].Text = "第一段第二段"
	a.mdActive, a.mdDraft = 0, "第一段第二段"
	a.afterEnter(utf8.RuneCountInString("第一段"), 0)
	if want := "第一段\n\n第二段"; a.tabs[0].Text != want {
		t.Errorf("分段后文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdActive != 2 || a.mdDraft != "第二段" || a.selSyncStart != 0 {
		t.Errorf("分段后焦点块 = %d draft=%q sel=%d, 要 2/第二段/0", a.mdActive, a.mdDraft, a.selSyncStart)
	}

	// Shift+Enter 的软换行（单 \n）：段内换行不分段，光标保持原生。
	a.mdActive, a.mdDraft = 2, "第二段\n续行"
	a.selSyncStart, a.selSyncEnd = -1, -1
	a.commitDraft(utf8.RuneCountInString("第二段"))
	if want := "第一段\n\n第二段\n续行"; a.tabs[0].Text != want {
		t.Errorf("软换行 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdDraft != "第二段\n续行" || a.selSyncStart != -1 {
		t.Errorf("软换行应留在同块内：draft=%q sel=%d", a.mdDraft, a.selSyncStart)
	}

	// 纯块内打字：不强制光标（保持原生光标）。
	a.mdDraft = "第二段续行呀"
	a.selSyncStart, a.selSyncEnd = -1, -1
	a.commitDraft(5)
	if want := "第一段\n\n第二段续行呀"; a.tabs[0].Text != want {
		t.Errorf("块内打字 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.selSyncStart != -1 {
		t.Error("块内打字不应重设光标")
	}

	// 列表续行：Enter 在列表项末尾 → 下一项自动补 "- "。
	a.tabs[0].Text = "- 甲"
	a.mdActive, a.mdDraft = 0, "- 甲"
	a.afterEnter(3, 0)
	if want := "- 甲\n- "; a.tabs[0].Text != want {
		t.Errorf("列表续行 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdDraft != "- " || a.selSyncStart != 2 {
		t.Errorf("续行草稿 = %q sel=%d, 要 - / 2", a.mdDraft, a.selSyncStart)
	}

	// 有序列表续行递增序号。
	a.tabs[0].Text = "1. 甲"
	a.mdActive, a.mdDraft = 0, "1. 甲"
	a.afterEnter(4, 0)
	if want := "1. 甲\n2. "; a.tabs[0].Text != want {
		t.Errorf("有序续行 = %q, 要 %q", a.tabs[0].Text, want)
	}

	// 空列表项上 Enter：去掉标记退出列表，光标留在空行上。
	a.tabs[0].Text = "- \n- 乙"
	a.mdActive, a.mdDraft = 0, "- "
	a.afterEnter(2, 0)
	if want := "\n- 乙"; a.tabs[0].Text != want {
		t.Errorf("退出列表 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdActive != 0 || a.mdDraft != "" || a.selSyncStart != 0 {
		t.Errorf("退出列表后 = %d draft=%q sel=%d, 要 0/空/0", a.mdActive, a.mdDraft, a.selSyncStart)
	}

	// 代码块内 Enter 插入单换行（不硬分段）。
	a.tabs[0].Text = "```go\nx\n```"
	a.mdActive, a.mdDraft = 0, "```go\nx\n```"
	a.afterEnter(7, 7)
	if want := "```go\nx\n\n```"; a.tabs[0].Text != want || a.mdDraft != want {
		t.Errorf("代码块回车 = doc %q draft %q, 要 %q", a.tabs[0].Text, a.mdDraft, want)
	}

	// 块首退格：与上一段合并（一次删除整个段落边界），光标停在接缝。
	a.tabs[0].Text = "甲\n\n乙"
	a.mdActive, a.mdDraft = 2, "乙"
	a.applyKeyHint(ui.KeyBackspace, 0, 0)
	if want := "甲乙"; a.tabs[0].Text != want || a.mdDraft != want || a.selSyncStart != 1 {
		t.Errorf("合块 = %q draft=%q sel=%d, 要 %v/同/1", a.tabs[0].Text, a.mdDraft, a.selSyncStart, want)
	}

	// 空列表项退格：去掉标记变空行（首块也可以）。
	a.tabs[0].Text = "- \n- 乙"
	a.mdActive, a.mdDraft = 0, "- "
	a.applyKeyHint(ui.KeyBackspace, 0, 0)
	if want := "\n- 乙"; a.tabs[0].Text != want || a.mdDraft != "" {
		t.Errorf("去标记 = %q draft=%q, 要 %q/空", a.tabs[0].Text, a.mdDraft, want)
	}

	// 选区非空时：退格不记录块级提示（原生只删选区，不合并块）。
	a.tabs[0].Text = "ABCDEF"
	a.mdActive, a.mdDraft = 0, "ABCDEF"
	a.mdCaret, a.mdSelEnd = 1, 5 // 选中 BCDE
	a.recordKeyHint(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyBackspace})
	if a.keyHint != 0 {
		t.Error("选区退格不应记录块级提示")
	}
	a.keyHint = 0
	a.mdCaret, a.mdSelEnd = 0, 0

	// 选区回车：选中内容被替换（"BCDE" 删除）后硬分段。
	a.tabs[0].Text = "ABCDEF"
	a.mdActive, a.mdDraft = 0, "ABCDEF"
	a.afterEnter(1, 5)
	if want := "A\n\nF"; a.tabs[0].Text != want {
		t.Errorf("选区回车文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdActive != 2 || a.mdDraft != "F" {
		t.Errorf("选区回车焦点 = %d draft=%q, 要 2/F", a.mdActive, a.mdDraft)
	}

	// 段首回车：上方生成空段落，焦点落原文本块、光标在其段首。
	a.tabs[0].Text = "ABC"
	a.mdActive, a.mdDraft = 0, "ABC"
	a.afterEnter(0, 0)
	if want := "\n\nABC"; a.tabs[0].Text != want {
		t.Errorf("段首回车文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdActive != 2 || a.mdDraft != "ABC" {
		t.Errorf("段首回车焦点 = %d draft=%q, 要 2/ABC", a.mdActive, a.mdDraft)
	}

	// 列表段首回车：上方生成空续行项，光标落其标记后。
	a.tabs[0].Text = "- 甲乙"
	a.mdActive, a.mdDraft = 0, "- 甲乙"
	a.afterEnter(0, 0)
	if want := "- \n- 甲乙"; a.tabs[0].Text != want {
		t.Errorf("列表段首回车文档 = %q, 要 %q", a.tabs[0].Text, want)
	}
	if a.mdActive != 0 || a.mdDraft != "- " || a.selSyncStart != 2 {
		t.Errorf("列表段首回车焦点 = %d draft=%q sel=%d, 要 0/- /2", a.mdActive, a.mdDraft, a.selSyncStart)
	}
	a.keyHint = 0

	// 点击空白区域的兜底：focusBlockEnd 聚焦最后一块块尾。
	a.tabs[0].Text = "# 标题\n\n正文"
	a.mdActive, a.mdDraft = 0, "# 标题"
	a.focusBlockEnd(99) // 越界收敛到最后一块
	if a.mdActive != 2 || a.mdDraft != "正文" {
		t.Errorf("focusBlockEnd = %d draft=%q, 要 2/正文", a.mdActive, a.mdDraft)
	}
	if a.mdCaret != 0 && a.selSyncStart != utf8.RuneCountInString("正文") {
		t.Errorf("focusBlockEnd 光标应到块尾 sel=%d", a.selSyncStart)
	}

	// 方向键跨块：Down 在块尾 → 下一块块首；Up 在块首 → 上一块块尾。
	a.tabs[0].Text = "甲\n\n乙"
	a.mdActive, a.mdDraft = 0, "甲"
	a.applyKeyHint(ui.KeyDown, 1, 1)
	if a.mdActive != 2 || a.mdDraft != "乙" || a.selSyncStart != 0 {
		t.Errorf("跨到下段 = %d draft=%q sel=%d", a.mdActive, a.mdDraft, a.selSyncStart)
	}
	a.applyKeyHint(ui.KeyUp, 0, 0)
	if a.mdActive != 0 || a.mdDraft != "甲" || a.selSyncStart != 1 {
		t.Errorf("跨回上段 = %d draft=%q sel=%d", a.mdActive, a.mdDraft, a.selSyncStart)
	}

	// 光标不在边界时提示不生效。
	a.tabs[0].Text = "甲\n\n乙"
	a.mdActive, a.mdDraft = 2, "乙"
	a.applyKeyHint(ui.KeyBackspace, 1, 1)
	if a.tabs[0].Text != "甲\n\n乙" {
		t.Errorf("非边界退格不应改文档: %q", a.tabs[0].Text)
	}

	// 代码块不可并入。
	a.tabs[0].Text = "```\ncode\n```\n尾"
	a.mdActive, a.mdDraft = 1, "尾"
	a.applyKeyHint(ui.KeyBackspace, 0, 0)
	if a.tabs[0].Text != "```\ncode\n```\n尾" {
		t.Errorf("退格不应并入代码块: %q", a.tabs[0].Text)
	}
}

// TestEditorRanges 检查高亮区间互不重叠且有序。
func TestEditorRanges(t *testing.T) {
	text := "# 标题\n\n**bold** 和 [link](https://x.y)\n- [ ] 待办 title\n```go\ncode here\n```\n"
	ranges := editorRanges(text, "待办", &ui.Theme{}, ui.Hex("#d4253a"))
	for i := 1; i < len(ranges); i++ {
		if ranges[i].Start < ranges[i-1].End {
			t.Fatalf("区间重叠: %+v", ranges)
		}
	}
	if len(ranges) == 0 {
		t.Fatal("应至少有搜索命中的区间")
	}
}

// TestFindMatches 验证大小写不敏感搜索。
func TestFindMatches(t *testing.T) {
	ms := findMatches("Go go GOlang", "go")
	if len(ms) != 3 {
		t.Fatalf("应命中 3 处，得到 %d: %+v", len(ms), ms)
	}
	if findMatches("abc", "") != nil {
		t.Error("空查询应无命中")
	}
}

// TestNoDirectProductionAppInTests 测试不得直接用 newApp()：它指向真实用户数据
// 目录，跑一次就会把用户的偏好与笔记覆盖成测试内容。新增测试请用 newTestApp(t)。
func TestNoDirectProductionAppInTests(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, "_test.go") || name == "main_test.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(src, []byte("newApp()")) {
			t.Errorf("%s 使用了 newApp()，会写真实用户数据目录；请改用 newTestApp(t)", name)
		}
	}
}

// TestSettingsTabMotionAndShapeRoundtrip 顶栏动效开关与标签形状随数据文件保存/载入。
func TestSettingsTabMotionAndShapeRoundtrip(t *testing.T) {
	a := newTestApp(t)
	a.cfg.TabMotionOff = true
	a.cfg.TabDotShape = "ring"
	if err := a.notes.SaveNow(a.snapshotFile()); err != nil {
		t.Fatal(err)
	}
	b := newTestApp(t)
	b.notes.Close()
	b.notes = newNotesStore(a.notes.DataPath(), b.now) // 读同一个临时目录的数据文件
	raw, err := os.ReadFile(b.notes.DataPath())
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := b.notes.ParseRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	b.applySavedFile(f)
	if !b.cfg.TabMotionOff || b.cfg.TabDotShape != "ring" {
		t.Fatalf("顶栏动效/形状未回读: %+v", b.cfg)
	}
	// 已去掉的形状按未知值回退圆点。
	f.Settings.TabDotShape = "hexagon"
	b.applySavedFile(f)
	if b.cfg.TabDotShape != "" || isValidTabDotShape("hexagon") || isValidTabDotShape("bogus") {
		t.Fatalf("旧形状应回退: %q", b.cfg.TabDotShape)
	}
}
