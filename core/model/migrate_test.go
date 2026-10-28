package model

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// 与 tab_model.go 的调色板、颜色校验保持同一规则，迁移结果才能按槽位颜色比较。
var testPalette = []string{
	"#d4253a", "#f67500", "#ffd01c", "#65c802", "#0f75e9", "#a40bd8", "#98989d",
	"#00a3b4", "#f7429c", "#a2845e", "#5856d6", "#32ade6",
}

func testMigrateOpt() MigrateOptions {
	return MigrateOptions{
		DefaultTabCount: 7,
		MaxTabCount:     12,
		PaletteColors:   append([]string(nil), testPalette...),
		ValidTabColor:   validHexColor,
	}
}

func validHexColor(s string) bool {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) != 3 && len(h) != 6 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func defaultTabs(texts ...string) []Tab {
	tabs := make([]Tab, 7)
	for i := range tabs {
		text := ""
		if i < len(texts) {
			text = texts[i]
		}
		tabs[i] = Tab{ID: uint64(i + 1), Text: text, Color: testPalette[i]}
	}
	return tabs
}

func TestMigrate(t *testing.T) {
	cases := []struct {
		name    string
		in      File
		want    func(f File) bool
		wantErr string
	}{
		{
			name: "全缺字段补齐 7 个默认标签",
			in:   File{},
			want: func(f File) bool {
				return f.Version == CurrentDataVersion &&
					reflect.DeepEqual(f.Tabs, defaultTabs()) &&
					f.Dots == nil &&
					f.Settings == Settings{ThemeSource: "system"}
			},
		},
		{
			name: "v1 dots 按原顺序生成 7 个标签",
			in:   File{Active: 6, Dots: paddedDots("x")},
			want: func(f File) bool {
				return f.Version == CurrentDataVersion && f.Active == 6 &&
					reflect.DeepEqual(f.Tabs, defaultTabs("x")) &&
					f.Settings == Settings{ThemeSource: "system"}
			},
		},
		{
			name: "v1 半缺 dots 补空",
			in:   File{Active: 2, Dots: []string{"a", "b"}},
			want: func(f File) bool {
				return reflect.DeepEqual(f.Tabs, defaultTabs("a", "b"))
			},
		},
		{
			name: "v1 多余 dots 截断到 7",
			in:   File{Dots: []string{"0", "1", "2", "3", "4", "5", "6", "7"}},
			want: func(f File) bool {
				return reflect.DeepEqual(f.Tabs, defaultTabs("0", "1", "2", "3", "4", "5", "6"))
			},
		},
		{
			name: "v1 旧版 preview 并入 source",
			in:   File{Settings: Settings{Preview: true}},
			want: func(f File) bool {
				return f.Settings == (Settings{ThemeSource: "system", Source: true, Preview: true})
			},
		},
		{
			name: "v2 补缺失 ID 与无效颜色",
			in: File{Version: 2, Active: 1, Tabs: []Tab{
				{Text: "甲", Name: "工作"},
				{ID: 5, Text: "乙", Color: "red!"},
			}},
			want: func(f File) bool {
				return f.Version == CurrentDataVersion && len(f.Tabs) == 2 &&
					f.Tabs[0].ID > 0 && f.Tabs[0].Name == "工作" &&
					f.Tabs[1].ID > 0 && f.Tabs[0].ID != f.Tabs[1].ID &&
					f.Tabs[1].Color == "" &&
					f.Active == 1
			},
		},
		{
			name: "v2 重复 ID 重新分配",
			in: File{Version: 2, Tabs: []Tab{
				{ID: 3, Text: "a"}, {ID: 3, Text: "b"},
			}},
			want: func(f File) bool {
				return f.Tabs[0].ID != f.Tabs[1].ID && f.Tabs[0].Text == "a" && f.Tabs[1].Text == "b"
			},
		},
		{
			name: "v2 空标签列表补 7 个默认",
			in:   File{Version: 2},
			want: func(f File) bool {
				return reflect.DeepEqual(f.Tabs, defaultTabs())
			},
		},
		{
			name: "v2 超过 12 个拒载",
			in: File{Version: 2, Tabs: func() []Tab {
				tabs := make([]Tab, 15)
				for i := range tabs {
					tabs[i] = Tab{ID: uint64(i + 1), Text: fmt.Sprintf("t%d", i)}
				}
				return tabs
			}(), Active: 14},
			wantErr: "标签数量超过上限",
		},
		{
			name:    "version 高于当前被拒绝",
			in:      File{Version: CurrentDataVersion + 1},
			wantErr: "当前版本无法读取",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Migrate(tc.in, testMigrateOpt())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("错误 = %v, want 含 %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Migrate 报错: %v", err)
			}
			if !tc.want(got) {
				t.Fatalf("迁移结果 = %+v", got)
			}
		})
	}
}

func TestMigrateTooManyTabsAndNilColorValidator(t *testing.T) {
	opt := testMigrateOpt()
	opt.MaxTabCount = 9
	tabs := make([]Tab, 10)
	for i := range tabs {
		tabs[i] = Tab{ID: uint64(i + 1), Text: fmt.Sprintf("t%d", i)}
	}
	if _, err := Migrate(File{Version: 3, Tabs: tabs}, opt); !errors.Is(err, ErrTooManyTabs) {
		t.Fatalf("10 个标签必须拒载：%v", err)
	}
	opt.ValidTabColor = nil
	f, err := Migrate(File{Version: 3, Tabs: []Tab{{ID: 1, Color: "#abc"}}}, opt)
	if err != nil || f.Tabs[0].Color != "#abc" {
		t.Fatalf("未注入校验器应保留颜色：%+v %v", f, err)
	}
}

func paddedDots(items ...string) []string {
	d := make([]string, 7)
	copy(d, items)
	return d
}

// TestMigrateThemeRenaming 版本 3 起排版主题 ID 更名（"tot" → "note"）：
// 旧版本数据文件载入时映射到新 ID，其余设置原样保留。
func TestMigrateThemeRenaming(t *testing.T) {
	f := File{Version: 2, Active: 0,
		Tabs:     []Tab{{ID: 1, Text: "内容"}},
		Settings: Settings{ThemeSource: "system", MarkdownTheme: "tot", HideOnEscape: true}}
	got, err := Migrate(f, testMigrateOpt())
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentDataVersion {
		t.Fatalf("版本应升到 %d，当前 %d", CurrentDataVersion, got.Version)
	}
	if got.Settings.MarkdownTheme != MarkdownThemeNote {
		t.Fatalf("排版主题应映射到 %q，当前 %q", MarkdownThemeNote, got.Settings.MarkdownTheme)
	}
	if got.Settings.ThemeSource != "system" || got.Settings.HideOnEscape != true {
		t.Fatalf("迁移不得改动其余设置: %+v", got.Settings)
	}
	// 版本 3 文件里的旧 ID 不再出现；非旧值原样保留。
	keep := File{Version: 3, Active: 0,
		Tabs:     []Tab{{ID: 1, Text: ""}},
		Settings: Settings{ThemeSource: "dark", MarkdownTheme: "book"}}
	gotKeep, err := Migrate(keep, testMigrateOpt())
	if err != nil {
		t.Fatal(err)
	}
	if gotKeep.Settings.MarkdownTheme != "book" {
		t.Fatalf("版本 3 的其他主题不应被改动: %+v", gotKeep.Settings)
	}
}
