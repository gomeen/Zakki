package app

// 首启向导：全新安装判定、配置应用、跳过语义与视图渲染。

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

func TestFirstRunDetection(t *testing.T) {
	a := newTestApp(t)
	if !a.firstRun() {
		t.Fatal("数据文件不存在应判定为全新安装")
	}
	if err := a.notes.SaveNow(a.snapshotFile()); err != nil {
		t.Fatal(err)
	}
	if a.firstRun() {
		t.Fatal("数据文件已存在不应判定为全新安装")
	}
}

func TestOnboardingCloseDecision(t *testing.T) {
	for _, tc := range []struct {
		button int
		err    error
		want   onboardingCloseDecision
	}{
		{button: 0, want: onboardingCloseContinue},
		{button: 1, want: onboardingCloseDefaults},
		{button: 2, want: onboardingCloseExit},
		{button: 2, err: os.ErrPermission, want: onboardingCloseContinue},
		{button: -1, want: onboardingCloseContinue},
	} {
		if got := onboardingCloseDecisionFor(tc.button, tc.err); got != tc.want {
			t.Errorf("button=%d err=%v: got %d, want %d", tc.button, tc.err, got, tc.want)
		}
	}
}

func TestOnboardingCloseConfirmGate(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	if !a.beginOnboardingCloseConfirm() || a.beginOnboardingCloseConfirm() {
		t.Fatal("关闭确认应只允许一个在途对话框")
	}
	a.onboarding.closeConfirming = false
	a.onboarding.phase = "saving"
	if a.beginOnboardingCloseConfirm() {
		t.Fatal("保存期间不应允许关闭")
	}
}

func TestShutdownOnboardingExitDoesNotCreateNotes(t *testing.T) {
	a := newTestApp(t)
	path := a.notes.DataPath()
	if err := a.shutdownOnboardingExit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("退出首启向导不应创建笔记文件: stat err=%v", err)
	}
	if !a.firstRun() {
		t.Fatal("不完成设置直接退出后，下次启动仍应进入向导")
	}
}

// 默认值必须来自注入的数据目录，不能触达真实用户 Documents。
func TestOnboardingDefaultStorageDirIsCurrentDataDir(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.initOnboarding()
	if got, want := a.onboarding.dirDraft, filepath.Dir(a.notes.DataPath()); got != want {
		t.Fatalf("向导默认目录 = %q, want 当前数据目录 %q", got, want)
	}
	assertOnlyStorageFiles(t, filepath.Dir(a.notes.DataPath()))
}

func TestOnboardingFinishAppliesConfig(t *testing.T) {
	a := newTestApp(t)
	folder := filepath.Join(filepath.Dir(a.notes.DataPath()), "wizard-storage")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a.onboarding.dirDraft = folder
	a.onboarding.tabCount = 3
	a.onboarding.phase = "ready" // 主按钮只在就绪页生效

	a.finishOnboarding(true)

	// 位置必须持久化为实际业务文件路径。
	wantPath := filepath.Join(folder, "zakki.json")
	if a.prefs.Storage.DataFile != wantPath || a.notes.DataPath() != wantPath {
		t.Fatalf("向导配置未写入偏好: %+v path=%q", a.prefs.Storage, a.notes.DataPath())
	}
	p, err := decodePreferences(persistReadRaw(t, a.preferencesPath()))
	if err != nil {
		t.Fatal(err)
	}
	if p.Storage.DataFile != wantPath {
		t.Fatalf("偏好未持久化: %+v", p.Storage)
	}
	// 数据：3 个标签，前两个为欢迎和时间说明，其余为空；文件已落盘。
	if len(a.tabs) != 3 {
		t.Fatalf("标签数 = %d, want 3", len(a.tabs))
	}
	if a.tabs[0].Text == "" {
		t.Fatal("首个标签应含欢迎文本")
	}
	if want := welcomeTextFor(3); a.tabs[0].Text != want {
		t.Fatalf("欢迎文案应匹配标签数: got %q", a.tabs[0].Text)
	}
	if a.tabs[1].Text != timeGuideText() || a.tabs[1].Name != locales.TimeGuideTabName() {
		t.Fatalf("第二个标签应为时间使用说明: %+v", a.tabs[1])
	}
	for i := 2; i < len(a.tabs); i++ {
		if a.tabs[i].Text != "" {
			t.Fatalf("标签 %d 应为空: %q", i+1, a.tabs[i].Text)
		}
	}
	// ID 单调唯一：收缩后允许跳号，但不得与现有标签冲突。
	seen := map[uint64]bool{}
	for _, tb := range a.tabs {
		if tb.ID == 0 || seen[tb.ID] {
			t.Fatalf("标签 ID 应唯一非零: %+v", a.tabs)
		}
		seen[tb.ID] = true
	}
	if a.nextTabID <= 3 {
		t.Fatalf("nextTabID = %d, 应大于现有最大 ID", a.nextTabID)
	}
	if _, err := os.Stat(a.notes.DataPath()); err != nil {
		t.Fatalf("数据文件应已写入: %v", err)
	}
	// 初始标签只保存到一个业务文件，不创建备份。
	names := dirJSONNames(t, folder)
	if len(names) != 1 || names[0] != "zakki.json" {
		t.Fatalf("向导完成后应只有 zakki.json: %v", names)
	}
	assertOnlyStorageFiles(t, folder, "zakki.json")
	assertOnlyStorageFiles(t, filepath.Dir(a.preferencesPath()), "preferences.json", "wizard-storage")
	b := newAppAt(filepath.Dir(a.preferencesPath()))
	t.Cleanup(b.shutdown)
	if err := b.loadPreferences(); err != nil {
		t.Fatal(err)
	}
	if err := b.openConfiguredNotes(); err != nil {
		t.Fatal(err)
	}
	if b.notes.DataPath() != wantPath || len(b.tabs) != 3 || b.tabs[0].Text != a.tabs[0].Text || b.firstRun() {
		t.Fatalf("重启应沿用向导文件与标签: path=%q tabs=%+v", b.notes.DataPath(), b.tabs)
	}
	// 向导标记结束（OnClosed 在真实窗口关闭时清理状态）。
	if !a.onboarding.finished {
		t.Fatal("向导应标记为已结束")
	}
}

func TestOnboardingSkipBlocksWhenDataSaveFails(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	// 用目录占据目标文件路径，跨平台稳定触发写入失败。
	if err := os.Mkdir(a.notes.DataPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	a.finishOnboarding(false)
	if a.onboarding.finished {
		t.Fatal("落盘失败不应结束向导")
	}
	if a.onboarding.dirErr == "" {
		t.Fatal("应提示数据写入失败")
	}
	info, err := os.Stat(a.notes.DataPath())
	if err != nil || !info.IsDir() {
		t.Fatalf("失败不得替换原目标: %v %v", info, err)
	}
	assertOnlyStorageFiles(t, filepath.Dir(a.notes.DataPath()), "zakki.json")
}

func TestOnboardingSkipKeepsCurrentStoragePath(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	path := a.notes.DataPath()
	a.onboarding.dirDraft = t.TempDir()
	a.finishOnboarding(false)
	if !a.onboarding.finished {
		t.Fatalf("跳过应完成默认初始化: %s", a.onboarding.dirErr)
	}
	if a.notes.DataPath() != path {
		t.Fatalf("跳过不应迁文件: %q", a.notes.DataPath())
	}
	if len(a.tabs) != DefaultTabCount {
		t.Fatalf("跳过应保留默认标签数: %d", len(a.tabs))
	}
	f := persistReadSaved(t, path)
	if len(f.Tabs) != DefaultTabCount || f.Tabs[0].Text != welcomeTextFor(DefaultTabCount) ||
		f.Tabs[1].Text != timeGuideText() || f.Tabs[1].Name != locales.TimeGuideTabName() {
		t.Fatalf("默认内容未保存: %+v", f)
	}
	if a.firstRun() {
		t.Fatal("下次启动不应重复向导")
	}
	assertOnlyStorageFiles(t, filepath.Dir(path), "zakki.json", "preferences.json")
	assertOnlyStorageFiles(t, a.onboarding.dirDraft)
}

func TestObApplyFolderValidates(t *testing.T) {
	a := newTestApp(t)
	base := t.TempDir()

	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	a.obApplyFolder(plain)
	wantPlain, err := canonicalPath(plain)
	if err != nil {
		t.Fatal(err)
	}
	if a.onboarding.dirDraft != wantPlain || a.onboarding.dirErr != "" {
		t.Fatalf("合法目录应原样记录: %+v", a.onboarding)
	}
	// 非法目录：记录错误且不改草稿。
	blocker := filepath.Join(base, "not-a-dir")
	persistWrite(t, blocker, "普通文件")
	a.obApplyFolder(blocker)
	if a.onboarding.dirErr == "" || a.onboarding.dirDraft != wantPlain {
		t.Fatalf("非法目录应报错且不改草稿: %+v", a.onboarding)
	}
}

func TestOnboardingViewRendersSteps(t *testing.T) {
	a := newTestApp(t)
	a.onboarding.tabCount = 7
	folder := filepath.Join(t.TempDir(), "view-folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a.onboarding.dirDraft = folder

	// 用固定顺序的切片而非 map：map 迭代随机，步骤 1 若留在最后，
	// 下面的「下载字体」断言会跑在步骤 1 界面上而随机失败。
	steps := []struct {
		step  int
		wants []string
	}{
		{1, []string{"外观与字体", "English", "日本語", "简体中文", "繁體中文", "明暗", "风格", "中文字体", "英文字体"}},
		{2, []string{"笔记与保存", "初始标签数量", "保存位置", folder, "完成设置"}},
	}
	for _, s := range steps {
		a.onboarding.step = s.step
		tt := ui.NewTester(a.onboardingView, 640, 560)
		tt.Frame()
		for _, want := range s.wants {
			if !tt.HasText(want) {
				t.Fatalf("步骤 %d 缺少 %q: %v", s.step, want, tt.Texts())
			}
		}
	}
	// 未扫描到已装字体且有可下载选择时，主按钮变为下载字体；
	// 字体摘要不出现（选择与下载分离）。
	a.onboarding.appearance.BodyFont = "hand"
	tt := ui.NewTester(a.onboardingView, 640, 560)
	tt.Frame()
	if !tt.HasText("下载字体") || tt.HasText("本次字体选择") {
		t.Fatalf("提交按钮或摘要不符合预期: %v", tt.Texts())
	}
	// 就绪页等待「开始使用」，不自动进入主界面。
	a.onboarding.phase = "ready"
	tt = ui.NewTester(a.onboardingView, 640, 560)
	tt.Frame()
	if !tt.HasText("开始使用") || !tt.HasText("准备完成") {
		t.Fatalf("就绪页应显示开始使用: %v", tt.Texts())
	}
	// 直接调用 finishOnboarding(true) 必须先经过就绪页。
	a.onboarding.phase = ""
	a.finishOnboarding(true)
	if a.onboarding.finished {
		t.Fatal("未经就绪页不应结束向导")
	}
	// 语言按钮的可见文字必须是 Text 子元素：ButtonBase 的 Label 只是
	// 可访问性标题，不设 Label 时 Texts 里的唯一一条就是可见文字。
	a.onboarding.step = 1
	tt = ui.NewTester(a.onboardingView, 640, 560)
	tt.Frame()
	for _, want := range []string{"English", "日本語", "简体中文", "繁體中文"} {
		if strings.Count(strings.Join(tt.Texts(), "\n"), want) < 1 {
			t.Fatalf("语言 %s 缺少可见 Text 子元素: %v", want, tt.Texts())
		}
	}
}

func dirJSONNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestOnboardingTabCountInput 标签数输入框：越界钳制到 1–12，非法输入回写规范化文本。
func TestOnboardingTabCountInput(t *testing.T) {
	a := newTestApp(t)
	a.onboarding.tabCountText = "0"
	a.commitTabCountText()
	if a.onboarding.tabCount != 1 {
		t.Fatalf("0 应钳制到 1: %d", a.onboarding.tabCount)
	}
	a.onboarding.tabCountText = "99"
	a.commitTabCountText()
	if a.onboarding.tabCount != MaxTabCount {
		t.Fatalf("99 应钳制到 %d: %d", MaxTabCount, a.onboarding.tabCount)
	}
	a.onboarding.tabCountText = "abc"
	a.commitTabCountText()
	if a.onboarding.tabCountText != strconv.Itoa(MaxTabCount) {
		t.Fatalf("非法输入应回写规范化文本: %q", a.onboarding.tabCountText)
	}
}

// 向导选手账风格：完成后写入风格，颜色与排版保持标准默认（切回标准时用），
// 偏好通过版本 10 校验。
func TestOnboardingSketchStyle(t *testing.T) {
	a := newTestApp(t)
	a.initOnboarding()
	folder := filepath.Join(filepath.Dir(a.notes.DataPath()), "wizard-style")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a.onboarding.dirDraft = folder
	tt := ui.NewTester(a.onboardingView, 640, 560)
	tt.Frame()
	if err := tt.Click("手账"); err != nil {
		t.Fatal(err)
	}
	if a.onboarding.appearance.Style != styleSketch {
		t.Fatalf("选手账应写入风格草稿: %+v", a.onboarding.appearance)
	}
	a.onboarding.phase = "ready"
	a.finishOnboarding(true)
	ap := a.prefs.Appearance
	if ap.Style != styleSketch || ap.ColorTheme != "classic" || ap.MarkdownTheme != "sonnet" {
		t.Fatalf("向导应只写风格、颜色与排版保持标准默认: %+v", ap)
	}
}

// 标签数步进器：减号、数字、加号同高，符号落在各自格子的中心。
func TestOnboardingTabStepperAligned(t *testing.T) {
	a := newTestApp(t)
	a.onboarding.step = 2
	a.onboarding.tabCount = 7
	a.onboarding.tabCountText = "7"
	tt := ui.NewTester(a.onboardingView, 640, 560)
	tt.Frame()
	img := tt.Image()
	minus, ok := tt.Find("−")
	if !ok {
		t.Fatalf("缺少减号: %v", tt.Texts())
	}
	plus, ok := tt.Find("+")
	if !ok {
		t.Fatalf("缺少加号: %v", tt.Texts())
	}
	mb := mustChrome(t, img, minus, "−")
	pb := mustChrome(t, img, plus, "+")
	nb, ok := chromeBetween(img, mb, pb)
	if !ok {
		t.Fatal("减号和加号之间找不到数字框")
	}
	digit, ok := darkGlyph(img, nb)
	if !ok {
		t.Fatal("数字框里没有数字")
	}
	for _, item := range []struct {
		name  string
		glyph ui.Rect
		box   [4]int
	}{
		{"−", minus, mb},
		{"7", digit, nb},
		{"+", plus, pb},
	} {
		h := item.box[3] - item.box[1]
		if absInt(h-(mb[3]-mb[1])) > 2 {
			t.Fatalf("%q 高 %d，减号高 %d", item.name, h, mb[3]-mb[1])
		}
		gx := item.glyph.X + item.glyph.W/2
		gy := item.glyph.Y + item.glyph.H/2
		cx := float32(item.box[0]+item.box[2]) / 2
		cy := float32(item.box[1]+item.box[3]) / 2
		if math.Abs(float64(gx-cx)) > 3 || math.Abs(float64(gy-cy)) > 3 {
			t.Fatalf("%q 符号中心 (%.1f,%.1f) 框 %v 格子中心 (%.1f,%.1f)", item.name, gx, gy, item.box, cx, cy)
		}
	}
}

func mustChrome(t *testing.T, img *image.RGBA, glyph ui.Rect, name string) [4]int {
	t.Helper()
	x0, y0, x1, y1, ok := chromeBox(img, glyph)
	if !ok {
		t.Fatalf("%s 找不到控件外框", name)
	}
	return [4]int{x0, y0, x1, y1}
}

// chromeBetween 取两个控件之间、与它们同一行的那一个外框。
func chromeBetween(img *image.RGBA, left, right [4]int) ([4]int, bool) {
	y := (left[1] + left[3]) / 2
	x := left[2] + 1
	for x < right[0] && segmentPixel(img, x, y) == 'W' {
		x++
	}
	if x >= right[0] {
		return [4]int{}, false
	}
	x0, y0, x1, y1, ok := chromeBox(img, ui.Rect{X: float32(x), Y: float32(y), W: 1, H: 1})
	if !ok {
		return [4]int{}, false
	}
	return [4]int{x0, y0, x1, y1}, true
}

func darkGlyph(img *image.RGBA, box [4]int) (ui.Rect, bool) {
	minx, miny := box[2], box[3]
	maxx, maxy := box[0], box[1]
	n := 0
	for y := box[1]; y <= box[3]; y++ {
		for x := box[0]; x <= box[2]; x++ {
			if segmentPixel(img, x, y) != 'D' {
				continue
			}
			n++
			if x < minx {
				minx = x
			}
			if y < miny {
				miny = y
			}
			if x > maxx {
				maxx = x
			}
			if y > maxy {
				maxy = y
			}
		}
	}
	if n < 8 {
		return ui.Rect{}, false
	}
	return ui.Rect{X: float32(minx), Y: float32(miny), W: float32(maxx - minx + 1), H: float32(maxy - miny + 1)}, true
}

func chromeBox(img *image.RGBA, glyph ui.Rect) (x0, y0, x1, y1 int, ok bool) {
	sx, sy := int(glyph.X+glyph.W/2), int(glyph.Y+glyph.H/2)
	b := img.Bounds()
	if sx < b.Min.X || sy < b.Min.Y || sx >= b.Max.X || sy >= b.Max.Y {
		return 0, 0, 0, 0, false
	}
	seen := map[[2]int]bool{}
	q := [][2]int{{sx, sy}}
	seen[[2]int{sx, sy}] = true
	x0, y0, x1, y1 = sx, sy, sx, sy
	for len(q) > 0 {
		p := q[0]
		q = q[1:]
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := p[0]+d[0], p[1]+d[1]
			key := [2]int{nx, ny}
			if seen[key] || nx < b.Min.X || ny < b.Min.Y || nx >= b.Max.X || ny >= b.Max.Y {
				continue
			}
			if segmentPixel(img, nx, ny) == 'W' {
				continue
			}
			seen[key] = true
			q = append(q, key)
			if nx < x0 {
				x0 = nx
			}
			if ny < y0 {
				y0 = ny
			}
			if nx > x1 {
				x1 = nx
			}
			if ny > y1 {
				y1 = ny
			}
		}
	}
	return x0, y0, x1, y1, x1-x0 > 8 && y1-y0 > 8
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// 分段轨道被拉到与字体下拉同宽时，每一段应等分轨道，选中底铺满自己那一格。
func TestOnboardingSegmentedCellsEqual(t *testing.T) {
	a := newTestApp(t)
	a.onboarding.step = 1
	a.onboarding.languageIndex = 2
	a.onboarding.modeIndex = 0
	a.onboarding.styleIndex = 0
	tt := ui.NewTester(a.onboardingView, 640, 560)
	tt.Frame()

	assertSegmentCells(t, tt, []string{"English", "日本語", "简体中文", "繁體中文"}, 2)
	assertSegmentCells(t, tt, []string{"跟随系统", "浅色", "深色"}, 0)
	assertSegmentCells(t, tt, []string{"标准", "手账"}, 0)
}

func assertSegmentCells(t *testing.T, tt *ui.Tester, labels []string, selected int) {
	t.Helper()
	rects := make([]ui.Rect, len(labels))
	for i, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("缺少分段 %q: %v", label, tt.Texts())
		}
		rects[i] = r
	}
	y := int(rects[0].Y) - 2
	track0, track1, sel0, sel1, ok := segmentSpans(tt.Image(), y)
	if !ok {
		t.Fatalf("%v 在 y=%d 找不到分段轨道", labels, y)
	}
	trackW := track1 - track0 + 1
	cell := float32(trackW) / float32(len(labels))
	for i, r := range rects {
		cx := r.X + r.W/2
		want := float32(track0) + (float32(i)+0.5)*cell
		if math.Abs(float64(cx-want)) > 10 {
			t.Fatalf("%q 中心 %.1f，等分中心 %.1f（轨道 %d–%d）", labels[i], cx, want, track0, track1)
		}
	}
	selW := sel1 - sel0 + 1
	if math.Abs(float64(selW)-float64(cell)) > 16 {
		t.Fatalf("%q 选中底宽 %d，等分约 %.1f（轨道 %d–%d）", labels[selected], selW, cell, track0, track1)
	}
}

// segmentSpans 在文字上方的分段内边距上量轨道与选中白底。
// 轨道两端是灰边，选中格是夹在灰底之间的白底。
func segmentSpans(img *image.RGBA, y int) (track0, track1, sel0, sel1 int, ok bool) {
	if y < 0 || y >= img.Bounds().Dy() {
		return 0, 0, 0, 0, false
	}
	track0, track1 = -1, -1
	for x := 0; x < img.Bounds().Dx(); x++ {
		if segmentPixel(img, x, y) == 'G' {
			if track0 < 0 {
				track0 = x
			}
			track1 = x
		}
	}
	if track0 < 0 {
		return 0, 0, 0, 0, false
	}
	sel0, sel1 = -1, -1
	run0 := -1
	for x := track0; x <= track1; x++ {
		inside := segmentPixel(img, x, y) != 'G'
		if inside && run0 < 0 {
			run0 = x
		}
		if (!inside || x == track1) && run0 >= 0 {
			end := x - 1
			if inside && x == track1 {
				end = x
			}
			if end-run0+1 > sel1-sel0 {
				sel0, sel1 = run0, end
			}
			run0 = -1
		}
	}
	return track0, track1, sel0, sel1, sel0 >= 0
}

func segmentPixel(img *image.RGBA, x, y int) byte {
	i := img.PixOffset(x, y)
	r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
	if r >= 250 && g >= 250 && b >= 250 {
		return 'W'
	}
	if r >= 200 && g >= 200 && b >= 200 && abs8(r, g) < 12 && abs8(g, b) < 12 {
		return 'G'
	}
	return 'D'
}

func abs8(a, b uint8) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}
