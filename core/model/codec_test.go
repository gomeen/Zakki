package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// roundTripOpts 与移动端写入路径一致：不注入调色板与颜色校验，标签数上限用共享常量。
func roundTripOpts() MigrateOptions {
	return MigrateOptions{DefaultTabCount: DefaultTabCount, MaxTabCount: MaxTabCount}
}

// desktopWrite 是桌面现在的写盘路径（internal/notes/save.go 的 MarshalIndent）。
func desktopWrite(t *testing.T, f File) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEncodeMatchesDesktopWritePath(t *testing.T) {
	fullSettings := Settings{
		ThemeSource: "dark", MarkdownTheme: "note", BodyFont: "jetbrains", MonoFont: "fira",
		BodySize: 15.5, LineHeight: 1.6, AlwaysOnTop: true, HideOnEscape: true,
		Source: false, Preview: false, TabTitleWidth: 88, TabAnimSpeed: 1.5,
		MonoDots: true, GlassStrength: 0.4, TabMotionOff: true, TabDotShape: "ring",
	}
	cases := []struct {
		name string
		file File
	}{
		{
			name: "全字段",
			file: File{
				Version: CurrentDataVersion, Active: 1,
				Tabs: []Tab{
					{ID: 1, Name: "工作", Text: "# 标题\n正文 <>&\"'", Color: "#d4253a"},
					{ID: 2, Text: "无名字无颜色"},
					{ID: 30, Name: "只有名字", Text: ""},
				},
				Dots:     []string{"旧点一", "旧点二"},
				Settings: fullSettings,
			},
		},
		{
			name: "空文档省略 tabs 与 dots",
			file: File{Version: CurrentDataVersion, Settings: Settings{ThemeSource: "system"}},
		},
		{
			name: "零值 settings 保留非 omitempty 键",
			file: File{Version: CurrentDataVersion, Active: 4, Tabs: []Tab{{ID: 9, Text: "x"}}, Settings: Settings{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := desktopWrite(t, tc.file)
			got, err := Encode(Document{File: tc.file})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("与桌面写盘不一致:\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestDecodeEncodeRoundTripUnknownFields(t *testing.T) {
	raw := `{
		"version": 3,
		"active": 1,
		"futureTop": {"kept": "顶层未知", "n": [1, 2, 3]},
		"tabs": [
			{"id": 1, "text": "甲", "color": "#65c802", "futureTab": "标签未知", "nested": {"a": true}},
			{"id": 2, "name": "乙", "text": "乙文"},
			{"id": 3, "text": "丙", "futureTab": 42}
		],
		"settings": {
			"themeSource": "system",
			"alwaysOnTop": false,
			"hideOnEscape": false,
			"futureSetting": ["设置未知"]
		}
	}`
	doc, err := Decode([]byte(raw), roundTripOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.File.Tabs) != 3 || doc.File.Active != 1 {
		t.Fatalf("已知字段解码错误: %+v", doc.File)
	}
	if string(doc.Unknown["futureTop"]) == "" || doc.SettingsUnknown["futureSetting"] == nil ||
		doc.TabUnknown[1] == nil || doc.TabUnknown[3] == nil {
		t.Fatalf("三层未知字段必须都被捕获: %+v", doc)
	}
	if doc.TabUnknown[2] != nil {
		t.Fatalf("无未知字段的标签不应产生空 map: %+v", doc.TabUnknown)
	}

	encoded, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(encoded, roundTripOpts())
	if err != nil {
		t.Fatalf("回读失败: %v\n%s", err, encoded)
	}
	if !equalRaw(doc.Unknown, again.Unknown) ||
		!equalRaw(doc.SettingsUnknown, again.SettingsUnknown) ||
		!equalTabUnknown(doc.TabUnknown, again.TabUnknown) {
		t.Fatalf("未知字段往返丢失:\n首次: %+v %+v %+v\n二次: %+v %+v %+v\n编码: %s",
			doc.Unknown, doc.SettingsUnknown, doc.TabUnknown,
			again.Unknown, again.SettingsUnknown, again.TabUnknown, encoded)
	}
}

// TestEditKnownFieldsKeepsUnknown 修改已知字段后未知字段原样保留（规格 §2.1 D18）。
func TestEditKnownFieldsKeepsUnknown(t *testing.T) {
	raw := `{
		"version": 3,
		"active": 0,
		"legacyTop": true,
		"tabs": [{"id": 5, "text": "旧文本", "futureTab": {"keep": "me"}}],
		"settings": {"themeSource": "light", "futureSetting": {"x": 1}}
	}`
	doc, err := Decode([]byte(raw), roundTripOpts())
	if err != nil {
		t.Fatal(err)
	}
	doc.File.Tabs[0].Text = "新文本"
	doc.File.Settings.ThemeSource = "dark"
	encoded, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Tabs []struct {
			Text      string          `json:"text"`
			FutureTab json.RawMessage `json:"futureTab"`
		} `json:"tabs"`
		Settings struct {
			ThemeSource  string          `json:"themeSource"`
			FutureKnown  json.RawMessage `json:"futureSetting"`
			HideOnEscape bool            `json:"hideOnEscape"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Tabs[0].Text != "新文本" {
		t.Fatalf("已知字段应已更新: %s", probe.Tabs[0].Text)
	}
	if probe.Settings.ThemeSource != "dark" {
		t.Fatalf("设置应已更新: %+v", probe.Settings)
	}
	if !equalRawValue(probe.Tabs[0].FutureTab, json.RawMessage(`{"keep":"me"}`)) {
		t.Fatalf("tab 未知字段必须原样保留: %s", probe.Tabs[0].FutureTab)
	}
	if !equalRawValue(probe.Settings.FutureKnown, json.RawMessage(`{"x":1}`)) {
		t.Fatalf("settings 未知字段必须原样保留: %s", probe.Settings.FutureKnown)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &top); err != nil {
		t.Fatal(err)
	}
	if string(top["legacyTop"]) != "true" {
		t.Fatalf("顶层未知字段必须原样保留: %s", top["legacyTop"])
	}
}

func TestDecodeErrorPaths(t *testing.T) {
	var versionErr *VersionError
	if _, err := Decode([]byte(`{"version":99}`), roundTripOpts()); !errors.As(err, &versionErr) {
		t.Fatalf("版本超前应返回 VersionError: %v", err)
	}
	var tabs []Tab
	for i := range MaxTabCount + 1 {
		tabs = append(tabs, Tab{ID: uint64(i + 1), Text: "x"})
	}
	tooMany, err := json.Marshal(File{Version: CurrentDataVersion, Tabs: tabs})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(tooMany, roundTripOpts()); !errors.Is(err, ErrTooManyTabs) {
		t.Fatalf("超过 %d 个标签应返回 ErrTooManyTabs: %v", MaxTabCount, err)
	}
	// hideOnEscape 缺键与桌面载入一致按 true。
	doc, err := Decode([]byte(`{"version":3,"tabs":[{"id":1,"text":"a"}]}`), roundTripOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !doc.File.Settings.HideOnEscape {
		t.Fatal("hideOnEscape 缺键应按 true")
	}
}

func equalRaw(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !equalRawValue(av, bv) {
			return false
		}
	}
	return true
}

func equalTabUnknown(a, b map[uint64]map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for id, am := range a {
		bm, ok := b[id]
		if !ok || !equalRaw(am, bm) {
			return false
		}
	}
	return true
}

// equalRawValue 比较两段 JSON 语义相等（压缩后字节一致，忽略原有空白差异）。
func equalRawValue(a, b json.RawMessage) bool {
	var ac, bc bytes.Buffer
	if json.Compact(&ac, a) != nil || json.Compact(&bc, b) != nil {
		return false
	}
	return bytes.Equal(ac.Bytes(), bc.Bytes())
}
