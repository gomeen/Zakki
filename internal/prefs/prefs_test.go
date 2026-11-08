package prefs

import (
	"errors"
	"strings"
	"testing"
)

// testAccel 只拒绝含空格的绑定。本文件的非法样例是 "not a key+"；
// 空串由 Validate 跳过，表示解除绑定。生产路径的解析器仍由根目录集成测试覆盖。
func testAccel(acc string) error {
	if strings.Contains(acc, " ") {
		return errors.New("invalid accelerator")
	}
	return nil
}

func TestPreferencesDefaultsAndValidation(t *testing.T) {
	p := Default()
	if err := p.Validate(testAccel); err != nil {
		t.Fatalf("默认偏好必须合法: %v", err)
	}
	if p.Appearance.ColorTheme != "classic" || p.Appearance.MarkdownTheme != "sonnet" || p.Appearance.AppearanceMode != "system" {
		t.Fatalf("全新安装默认：Tot 颜色 + Sonnet 排版 + 跟随系统: %+v", p.Appearance)
	}
	if !p.Window.HideOnEscape || !p.Window.RememberBounds {
		t.Fatalf("窗口默认值错误: %+v", p.Window)
	}
	if p.Storage != (Storage{SaveIntervalMS: 2000}) || p.Git != (Git{CommitMinutes: 1, PushMinutes: 10}) {
		t.Fatalf("存储 / Git 默认值错误: %+v %+v", p.Storage, p.Git)
	}
	if p.Advanced.WindowColumns != 22 || p.Advanced.WindowRows != 16 {
		t.Fatalf("窗口参数默认值错误: %+v", p.Advanced)
	}
	for _, mutate := range []func(*Preferences){
		func(p *Preferences) { p.Appearance.AppearanceMode = "blue" },
		func(p *Preferences) { p.Appearance.ColorTheme = "neon" },
		func(p *Preferences) { p.Appearance.MarkdownTheme = "github" },
		func(p *Preferences) { p.Appearance.BodyFont = "comic" },
		func(p *Preferences) { p.Appearance.BodySize = 9 },
		func(p *Preferences) { p.Appearance.BodySize = 41 },
		func(p *Preferences) { p.Appearance.LineHeight = 0.5 },
		func(p *Preferences) { p.Appearance.LineHeight = 3.1 },
		func(p *Preferences) { p.Storage.SaveIntervalMS = 99 },
		func(p *Preferences) { p.Storage.SaveIntervalMS = 60001 },
		func(p *Preferences) { p.Git.CommitMinutes = 0 },
		func(p *Preferences) { p.Git.CommitMinutes = 1441 },
		func(p *Preferences) { p.Git.PushMinutes = 0 },
		func(p *Preferences) { p.Git.PushMinutes = 1441 },
		func(p *Preferences) { p.Git.AutoPush = true },
		func(p *Preferences) { p.Advanced.WindowColumns = MinWindowColumns - 1 },
		func(p *Preferences) { p.Advanced.WindowColumns = MaxWindowColumns + 1 },
		func(p *Preferences) { p.Advanced.WindowRows = MinWindowRows - 1 },
		func(p *Preferences) { p.Advanced.WindowRows = MaxWindowRows + 1 },
		func(p *Preferences) { p.Keymap.Overrides["format.bold"] = "not a key+" },
	} {
		bad := Default()
		mutate(&bad)
		if err := bad.Validate(testAccel); err == nil {
			t.Fatalf("非法偏好应被拒绝: %+v", bad)
		}
	}
	// 0 = 跟随主题；显式空串 = 解除绑定。
	ok := Default()
	ok.Appearance.BodySize, ok.Appearance.LineHeight = 0, 0
	ok.Keymap.Overrides["format.bold"] = ""
	if err := ok.Validate(testAccel); err != nil {
		t.Fatalf("跟随主题值与显式空绑定必须合法: %v", err)
	}
}

func TestPreferencesDecodeStrict(t *testing.T) {
	for _, raw := range []string{
		`{`, `null`, `[]`, `{"version":1,}`,
		`{"unknown":1}`, `{"appearance":{"bodySize":99}}`,
		`{"appearance":null}`, `{"keymap":{"overrides":null}}`,
		`{"version":99}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := Decode([]byte(raw), testAccel); err == nil {
				t.Fatalf("不应接受 %s", raw)
			}
		})
	}
	got, err := Decode([]byte(`{"version":1,"appearance":{"colorTheme":"graphite"}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance.ColorTheme != "graphite" || got.Appearance.MarkdownTheme != "sonnet" {
		t.Fatalf("缺项应用默认值: %+v", got)
	}
	if got.Keymap.Overrides == nil {
		t.Fatal("缺 keymap 节应补空 map")
	}
}

// TestPreferencesMigrateFontSplit 版本 1 只有单一正文字体键；升级后
// latinFont 留空（跟随中文字体），渲染结果与升级前一致，其余字段原样保留。
func TestPreferencesMigrateFontSplit(t *testing.T) {
	v1 := `{"version":1,"appearance":{"bodyFont":"songti","monoFont":"menlo","bodySize":19,"colorTheme":"paper"}}`
	got, err := Decode([]byte(v1), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion || CurrentVersion != 14 {
		t.Fatalf("版本应升到 %d，当前 %d", CurrentVersion, got.Version)
	}
	if got.Appearance.LatinFont != "" {
		t.Fatalf("升级后西文应跟随中文字体: %q", got.Appearance.LatinFont)
	}
	if got.Appearance.BodyFont != "songti" || got.Appearance.MonoFont != "menlo" ||
		got.Appearance.BodySize != 19 || got.Appearance.ColorTheme != "paper" {
		t.Fatalf("升级不得改动其余偏好: %+v", got)
	}
	// 无 version 的旧文件同样升级。
	noVer, err := Decode([]byte(`{"appearance":{"bodyFont":"hand"}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if noVer.Version != CurrentVersion || noVer.Appearance.BodyFont != "hand" || noVer.Appearance.LatinFont != "" {
		t.Fatalf("缺 version 的旧文件应升级: %+v", noVer)
	}
	// 版本 2 文件里的西文字体原样读回，非法值拒绝。
	v2, err := Decode([]byte(`{"version":2,"appearance":{"bodyFont":"hand","latinFont":"noteworthy"}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Appearance.LatinFont != "noteworthy" || v2.Appearance.BodyFont != "hand" {
		t.Fatalf("版本 2 应分别读回中西文字体: %+v", v2.Appearance)
	}
	if _, err := Decode([]byte(`{"version":2,"appearance":{"latinFont":"comic"}}`), testAccel); err == nil {
		t.Fatal("未知西文字体必须拒绝")
	}
	// 磁盘模型随当前版本落盘。
	if d := Default(); d.Version != CurrentVersion || d.Appearance.LatinFont != "jetbrains" {
		t.Fatalf("默认偏好应为当前版本且英文默认 JetBrains Mono: %+v", d)
	}
}

// TestPreferencesMigrateThemeRenaming 版本 4 把更名前的主题 ID（"tot"）
// 映射到新 ID（"classic" / "note"），主题取值本身不变。
func TestPreferencesMigrateThemeRenaming(t *testing.T) {
	v3 := `{"version":3,"appearance":{"colorTheme":"tot","markdownTheme":"tot","bodyFont":"kai"}}`
	got, err := Decode([]byte(v3), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion {
		t.Fatalf("版本应升到 %d，当前 %d", CurrentVersion, got.Version)
	}
	if got.Appearance.ColorTheme != "classic" || got.Appearance.MarkdownTheme != "note" {
		t.Fatalf("主题 ID 应映射到新 ID: %+v", got.Appearance)
	}
	if got.Appearance.BodyFont != "kai" {
		t.Fatalf("迁移不得改动其余偏好: %+v", got)
	}
	// 新 ID 原样通过；缺 colorTheme 键的新文件直接用新默认。
	fresh, err := Decode([]byte(`{"version":4}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Appearance.ColorTheme != "classic" || fresh.Appearance.MarkdownTheme != "sonnet" {
		t.Fatalf("新文件默认值: %+v", fresh.Appearance)
	}
}

// TestPreferencesMigrateCodeThemeSoft 版本 5 起代码块配色默认「柔和」：版本 4 及
// 更早的写入方从未写过 codeTheme 键，缺值文件补 soft；带值文件原样保留。版本 5
// 起该键始终落盘，缺键与空串都按显式「跟随文本」处理，不再补默认。
func TestPreferencesMigrateCodeThemeSoft(t *testing.T) {
	// 0.1.0/0.2.0 升级路径：v4 文件无 codeTheme 键 → 补默认 soft。
	v4, err := Decode([]byte(`{"version":4,"appearance":{"colorTheme":"paper"}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if v4.Appearance.CodeTheme != "soft" {
		t.Fatalf("v4 缺键应补默认 soft: %+v", v4.Appearance)
	}
	// 旧开发文件带显式值：迁移不得覆盖。
	v4set, err := Decode([]byte(`{"version":4,"appearance":{"codeTheme":"vivid"}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if v4set.Appearance.CodeTheme != "vivid" {
		t.Fatalf("v4 带值应原样保留: %+v", v4set.Appearance)
	}
	// v5 文件缺键：不补默认，按「跟随文本」处理。
	v5missing, err := Decode([]byte(`{"version":5}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if v5missing.Appearance.CodeTheme != "" {
		t.Fatalf("v5 缺键应保持跟随文本: %+v", v5missing.Appearance)
	}
	// v5 显式空串 = 跟随文本，解码原样读回。
	v5follow, err := Decode([]byte(`{"version":5,"appearance":{"codeTheme":""}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if v5follow.Appearance.CodeTheme != "" {
		t.Fatalf("显式跟随文本应原样保留: %+v", v5follow.Appearance)
	}
	if got := Default().Appearance.CodeTheme; got != "soft" {
		t.Fatalf("全新安装默认应为 soft: %q", got)
	}
}

// TestPreferencesMigratePadding 版本 13 新增 appearance.padX / padY（正文区
// 左右 / 上下内边距覆盖，DIP）：旧文件从未写过这两个键，缺键解码即零值 = 跟随
// 排版主题，无需迁移；带值文件原样读回；越界值拒绝，显式 0 表示跟随。
func TestPreferencesMigratePadding(t *testing.T) {
	v12, err := Decode([]byte(`{"version":12,"appearance":{"bodySize":18}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if v12.Version != CurrentVersion {
		t.Fatalf("版本应升到 %d，当前 %d", CurrentVersion, v12.Version)
	}
	if v12.Appearance.PadX != 0 || v12.Appearance.PadY != 0 {
		t.Fatalf("缺键应跟随主题（零值）: %+v", v12.Appearance)
	}
	if v12.Appearance.BodySize != 18 {
		t.Fatalf("迁移不得改动其余偏好: %+v", v12.Appearance)
	}
	set, err := Decode([]byte(`{"version":13,"appearance":{"padX":40,"padY":12}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if set.Appearance.PadX != 40 || set.Appearance.PadY != 12 {
		t.Fatalf("带值应原样读回: %+v", set.Appearance)
	}
	for _, raw := range []string{
		`{"version":13,"appearance":{"padX":4}}`,
		`{"version":13,"appearance":{"padY":120.5}}`,
		`{"version":13,"appearance":{"padX":-8}}`,
	} {
		if _, err := Decode([]byte(raw), testAccel); err == nil {
			t.Fatalf("越界内边距必须拒绝: %s", raw)
		}
	}
	follow, err := Decode([]byte(`{"version":13,"appearance":{"padX":0,"padY":0}}`), testAccel)
	if err != nil || follow.Appearance.PadX != 0 || follow.Appearance.PadY != 0 {
		t.Fatalf("显式 0 表示跟随主题，必须接受: %+v %v", follow, err)
	}
}

func TestPreferencesRejectBackup(t *testing.T) {
	// 任何不超过当前版本的文件：backup 整节与 autosaveDelayMs 都静默摘除。
	for _, version := range []string{"1", "3", "5", "6", "8", "9", "11", "12"} {
		raw := `{"version":` + version + `,"backup":{"mirrorDir":"/old"},"advanced":{"autosaveDelayMs":900}}`
		got, err := Decode([]byte(raw), nil)
		if err != nil {
			t.Fatalf("旧备份键应摘除而不是拒绝: %s: %v", raw, err)
		}
		if got.Version != CurrentVersion {
			t.Fatalf("应升级到当前版本: %d", got.Version)
		}
	}
	// 真正超前的版本仍然拒绝。
	if _, err := Decode([]byte(`{"version":99}`), nil); err == nil {
		t.Fatal("超前版本必须拒绝")
	}
	// 版本 11 及更早：backup 整节与 autosaveDelayMs 静默摘除，其余设置原样保留。
	raw := `{"version":11,"appearance":{"colorTheme":"paper"},"backup":{"mirrorDir":"/old","gitMode":"commitPush"},"window":{"rememberBounds":false},"advanced":{"autosaveDelayMs":900,"windowColumns":30,"windowRows":20}}`
	got, err := Decode([]byte(raw), nil)
	if err != nil {
		t.Fatalf("旧 backup 键应摘除而不是拒绝: %v", err)
	}
	def := Default()
	if got.Version != CurrentVersion || got.Appearance.ColorTheme != "paper" || got.Window.RememberBounds {
		t.Fatalf("非存储设置应保留: %+v", got)
	}
	if got.Storage != def.Storage || got.Git != def.Git || got.Advanced.WindowColumns != 30 || got.Advanced.WindowRows != 20 {
		t.Fatalf("存储应用默认，不迁移旧语义: %+v", got)
	}
}

// TestPreferencesMigrateCodeIndent 版本 7 起代码块缩进只有 2 / 4 空格两档：
// 旧文件从未写过该键，升级时补默认 4 空格；早期内部构建写过的 tab / space8
// 废弃档位归为默认；其余非法取值仍拒绝。
func TestPreferencesMigrateCodeIndent(t *testing.T) {
	v6, err := Decode([]byte(`{"version":6,"appearance":{"colorTheme":"paper"}}`), testAccel)
	if err != nil {
		t.Fatal(err)
	}
	if v6.Version != CurrentVersion || v6.Appearance.CodeIndent != "space2" {
		t.Fatalf("v6 升级后应补默认 2 空格: 版本 %d codeIndent=%q", v6.Version, v6.Appearance.CodeIndent)
	}
	for _, key := range []string{"space2", "space4"} {
		v7, err := Decode([]byte(`{"version":7,"appearance":{"codeIndent":"`+key+`"}}`), testAccel)
		if err != nil {
			t.Fatalf("codeIndent=%s 应合法: %v", key, err)
		}
		if v7.Appearance.CodeIndent != key {
			t.Fatalf("codeIndent 应原样保留: %q", v7.Appearance.CodeIndent)
		}
	}
	for _, legacy := range []string{"tab", "space8"} {
		v7, err := Decode([]byte(`{"version":7,"appearance":{"codeIndent":"`+legacy+`"}}`), testAccel)
		if err != nil {
			t.Fatalf("废弃档位 %q 应归为默认而非拒绝: %v", legacy, err)
		}
		if v7.Appearance.CodeIndent != "space2" {
			t.Fatalf("废弃档位 %q 应归为默认 2 空格: %q", legacy, v7.Appearance.CodeIndent)
		}
	}
	if _, err := Decode([]byte(`{"version":7,"appearance":{"codeIndent":"space3"}}`), testAccel); err == nil {
		t.Fatal("非法 codeIndent 必须拒绝")
	}
}

// TestPreferencesMigrateSketchStyle 版本 10：手绘 + 手账整套组合迁为手账风格，
// 颜色与排版回到标准默认（切回标准时用）；半套组合迁为标准风格，手绘/手账那一项
// 回到默认，另一项原样保留；其余字段不动。
func TestPreferencesMigrateSketchStyle(t *testing.T) {
	cases := []struct {
		name, color, layout      string
		wantStyle, wantC, wantMD string
	}{
		{"整套", "sketch", "sketch", StyleSketch, "classic", "sonnet"},
		{"只有颜色", "sketch", "book", StyleStandard, "classic", "book"},
		{"只有排版", "paper", "sketch", StyleStandard, "paper", "sonnet"},
		{"都不是", "paper", "book", StyleStandard, "paper", "book"},
	}
	for _, tc := range cases {
		raw := `{"version":9,"appearance":{"colorTheme":"` + tc.color + `","markdownTheme":"` + tc.layout + `","bodyFont":"hand"}}`
		got, err := Decode([]byte(raw), testAccel)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		ap := got.Appearance
		if got.Version != CurrentVersion || ap.Style != tc.wantStyle || ap.ColorTheme != tc.wantC || ap.MarkdownTheme != tc.wantMD {
			t.Fatalf("%s: 迁移结果 %+v", tc.name, ap)
		}
		if ap.BodyFont != "hand" {
			t.Fatalf("%s: 迁移不得改动其余偏好: %+v", tc.name, got)
		}
		wantEffC, wantEffMD := tc.wantC, tc.wantMD
		if tc.wantStyle == StyleSketch {
			wantEffC, wantEffMD = "sketch", "sketch"
		}
		if ap.EffectiveColorTheme() != wantEffC || ap.EffectiveMarkdownTheme() != wantEffMD {
			t.Fatalf("%s: 生效主题 %q / %q", tc.name, ap.EffectiveColorTheme(), ap.EffectiveMarkdownTheme())
		}
	}
	// 版本 10 起手绘/手账只能经风格表达：直接写进颜色或排版必须拒绝。
	for _, raw := range []string{
		`{"version":10,"appearance":{"colorTheme":"sketch"}}`,
		`{"version":10,"appearance":{"markdownTheme":"sketch"}}`,
		`{"version":10,"appearance":{"style":"journal"}}`,
	} {
		if _, err := Decode([]byte(raw), testAccel); err == nil {
			t.Fatalf("应拒绝: %s", raw)
		}
	}
	// 缺 style 键按标准风格。
	got, err := Decode([]byte(`{"version":10,"appearance":{"colorTheme":"paper"}}`), testAccel)
	if err != nil || got.Appearance.Style != StyleStandard {
		t.Fatalf("缺 style 应为标准风格: %+v %v", got.Appearance, err)
	}
}

// TestPreferencesMigrateWindowGrid 版本 11：像素尺寸换算为每行字数 × 行数。
// 旧默认 400 × 540 在默认排版下是 22 × 16；有字号 / 行高覆盖时按覆盖值换算；
// 缺的一边按旧默认补齐；非法旧值照旧拒绝；版本 11 起像素键是未知字段。
func TestPreferencesMigrateWindowGrid(t *testing.T) {
	cases := []struct {
		name, raw  string
		cols, rows int
	}{
		{"旧默认", `{"version":10,"advanced":{"windowWidth":400,"windowHeight":540}}`, 22, 16},
		{"自定义", `{"version":10,"advanced":{"windowWidth":560,"windowHeight":700}}`, 32, 23},
		{"字号覆盖", `{"version":10,"appearance":{"bodySize":20,"lineHeight":2},"advanced":{"windowWidth":560,"windowHeight":700}}`, 26, 14},
		{"只有宽", `{"version":10,"advanced":{"windowWidth":560}}`, 32, 16},
		{"缺键", `{"version":10,"advanced":{}}`, 22, 16},
		{"很早的版本", `{"version":4,"advanced":{"windowWidth":4096,"windowHeight":4096}}`, MaxWindowColumns, MaxWindowRows},
	}
	for _, tc := range cases {
		got, err := Decode([]byte(tc.raw), testAccel)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Version != CurrentVersion || got.Advanced.WindowColumns != tc.cols || got.Advanced.WindowRows != tc.rows {
			t.Fatalf("%s: 得到 %d × %d，期望 %d × %d", tc.name, got.Advanced.WindowColumns, got.Advanced.WindowRows, tc.cols, tc.rows)
		}
	}
	for _, raw := range []string{
		`{"version":10,"advanced":{"windowWidth":299}}`,
		`{"version":10,"advanced":{"windowHeight":"540"}}`,
		`{"version":10,"advanced":{"windowWidth":400.5}}`,
	} {
		if _, err := Decode([]byte(raw), testAccel); err == nil {
			t.Fatalf("应拒绝: %s", raw)
		}
	}
	// v11 带像素键：键被摘除（不再因版本守卫漏摘），列数行数走默认。
	got, err := Decode([]byte(`{"version":11,"advanced":{"windowWidth":400}}`), testAccel)
	if err != nil {
		t.Fatalf("v11 像素键应摘除: %v", err)
	}
	if got.Advanced.WindowColumns != 22 || got.Advanced.WindowRows != 16 {
		t.Fatalf("像素键摘除后应补默认: %+v", got.Advanced)
	}
}

// 列数 × 行数与像素互换：默认值往返一致，换算随字号变化。
func TestWindowPixelsRoundTrip(t *testing.T) {
	m := legacyWindowMetrics(Appearance{})
	w, h := WindowPixels(22, 16, m)
	if w != 400 || h != 532 {
		t.Fatalf("22 × 16 应约为 400 × 532: %d × %d", w, h)
	}
	if c, r := WindowGrid(w, h, m); c != 22 || r != 16 {
		t.Fatalf("往返应一致: %d × %d", c, r)
	}
	big := m
	big.BodySize = 20
	if bw, bh := WindowPixels(22, 16, big); bw <= w || bh <= h {
		t.Fatalf("字号变大窗口应随之变大: %d × %d", bw, bh)
	}
}
