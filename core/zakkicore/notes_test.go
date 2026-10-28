package zakkicore

import (
	"encoding/json"
	"strings"
	"testing"

	"zakki/core/model"
)

// desktopStyleFile 是桌面写盘路径会产出的当前版本文件（全部已知键齐备）。
func desktopStyleFile() model.File {
	return model.File{
		Version: model.CurrentDataVersion, Active: 1,
		Tabs: []model.Tab{
			{ID: 1, Name: "工作", Text: "# 标题", Color: "#d4253a"},
			{ID: 2, Text: "第二个标签", Color: "#65c802"},
		},
		Settings: model.Settings{ThemeSource: "system", HideOnEscape: true},
	}
}

func decodeNotesStatus(t *testing.T, raw string) notesSnapshot {
	t.Helper()
	out, err := DecodeNotes(raw)
	if err != nil {
		t.Fatalf("DecodeNotes 不应报 Go error: %v", err)
	}
	var s notesSnapshot
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("返回不是契约 JSON: %v %s", err, out)
	}
	return s
}

func TestDecodeNotesReasons(t *testing.T) {
	tooMany, err := json.Marshal(model.File{Version: 3, Tabs: func() []model.Tab {
		tabs := make([]model.Tab, model.MaxTabCount+1)
		for i := range tabs {
			tabs[i] = model.Tab{ID: uint64(i + 1)}
		}
		return tabs
	}()})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name         string
		raw          string
		wantReason   string
		wantWritable bool
		wantTabs     string // "null"、"[]" 或 "present"
		wantVersion  int
		wantActive   int
	}{
		{"正常 v3", `{"version":3,"active":1,"tabs":[{"id":1,"text":"甲"}]}`, "ok", true, "present", 3, 1},
		{"版本超前", `{"version":4,"active":0,"tabs":[{"id":1,"text":"甲"}]}`, "versionTooNew", false, "null", 4, 0},
		{"标签超限", string(tooMany), "tooManyTabs", false, "null", 3, 0},
		{"v1 只有 dots", `{"version":1,"active":2,"dots":["甲","乙"]}`, "legacyVersion", false, "[]", 1, 2},
		{"v2 只读展示", `{"version":2,"active":0,"tabs":[{"id":3,"text":"旧"}]}`, "legacyVersion", false, "present", 2, 0},
		{"非法 JSON", `{oops`, "invalid", false, "null", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeNotesStatus(t, tc.raw)
			if got.Reason != tc.wantReason || got.Writable != tc.wantWritable {
				t.Fatalf("reason/writable = %s/%v，期望 %s/%v（raw: %s）", got.Reason, got.Writable, tc.wantReason, tc.wantWritable, tc.raw)
			}
			if got.Version != tc.wantVersion || got.Active != tc.wantActive {
				t.Fatalf("version/active = %d/%d，期望 %d/%d", got.Version, got.Active, tc.wantVersion, tc.wantActive)
			}
			switch tc.wantTabs {
			case "null":
				if got.Tabs != nil {
					t.Fatalf("不返回 tabs 时应为 null: %s", tc.raw)
				}
			case "[]":
				if got.Tabs == nil || len(got.Tabs) != 0 {
					t.Fatalf("v1 应返回空数组: %s", tc.raw)
				}
			}
		})
	}
	t.Run("legacy 标签按磁盘原样", func(t *testing.T) {
		got := decodeNotesStatus(t, `{"version":2,"active":9,"tabs":[{"id":3,"name":"旧名","text":"旧"}]}`)
		if len(got.Tabs) != 1 || got.Tabs[0].ID != 3 || got.Tabs[0].Name != "旧名" || got.Active != 9 {
			t.Fatalf("legacy 必须按磁盘原样返回且不修正 active: %+v", got)
		}
	})
	t.Run("正常文件 active 按磁盘原样不修正", func(t *testing.T) {
		got := decodeNotesStatus(t, `{"version":3,"active":99,"tabs":[{"id":1,"text":"甲"}]}`)
		if got.Active != 99 || got.Reason != "ok" {
			t.Fatalf("active 不迁移不修正: %+v", got)
		}
	})
}

func TestEncodeNotesWriteProtection(t *testing.T) {
	tooMany, err := json.Marshal(model.File{Version: 3, Tabs: func() []model.Tab {
		tabs := make([]model.Tab, model.MaxTabCount+1)
		for i := range tabs {
			tabs[i] = model.Tab{ID: uint64(i + 1)}
		}
		return tabs
	}()})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		raw  string
	}{
		{"版本超前", `{"version":4,"tabs":[{"id":1,"text":"甲"}]}`},
		{"legacy 版本", `{"version":2,"tabs":[{"id":1,"text":"甲"}]}`},
		{"标签超限", string(tooMany)},
		{"文件损坏", `{oops`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := EncodeNotes(tc.raw, `[{"id":1,"text":"改"}]`)
			if err == nil {
				t.Fatalf("必须拒绝写入，得到 %s", out)
			}
			if !strings.HasPrefix(err.Error(), "write_protected: ") {
				t.Fatalf("错误码必须是 write_protected: %v", err)
			}
		})
	}
}

func TestEncodeNotesOverlayAndPreserve(t *testing.T) {
	raw := `{
		"version": 3,
		"active": 1,
		"futureTop": {"keep": "顶层未知"},
		"tabs": [
			{"id": 1, "name": "旧名", "text": "旧文", "color": "#d4253a", "futureTab": [1, 2]},
			{"id": 2, "text": "不动", "color": "#65c802"}
		],
		"settings": {
			"themeSource": "dark",
			"alwaysOnTop": true,
			"hideOnEscape": false,
			"futureSetting": "设置未知"
		}
	}`
	updates := `[{"id":2,"name":"二号","text":"新文","color":"#0f75e9"}]`
	out, err := EncodeNotes(raw, updates)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Active int `json:"active"`
		Tabs   []struct {
			ID        uint64          `json:"id"`
			Name      string          `json:"name"`
			Text      string          `json:"text"`
			Color     string          `json:"color"`
			FutureTab json.RawMessage `json:"futureTab"`
		} `json:"tabs"`
		Settings struct {
			ThemeSource  string          `json:"themeSource"`
			AlwaysOnTop  bool            `json:"alwaysOnTop"`
			HideOnEscape bool            `json:"hideOnEscape"`
			FutureSet    json.RawMessage `json:"futureSetting"`
		} `json:"settings"`
		FutureTop json.RawMessage `json:"futureTop"`
	}
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("编码结果不可解析: %v\n%s", err, out)
	}
	if len(probe.Tabs) != 2 || probe.Tabs[1].Text != "新文" || probe.Tabs[1].Name != "二号" ||
		probe.Tabs[1].Color != "#0f75e9" {
		t.Fatalf("按 id 覆盖失败: %s", out)
	}
	if probe.Tabs[0].Text != "旧文" || probe.Tabs[0].Name != "旧名" || probe.Tabs[0].Color != "#d4253a" {
		t.Fatalf("未覆盖的标签不得变动: %s", out)
	}
	if probe.Active != 1 {
		t.Fatalf("active 必须保持: %s", out)
	}
	if !probe.Settings.AlwaysOnTop || probe.Settings.HideOnEscape || probe.Settings.ThemeSource != "dark" {
		t.Fatalf("桌面专属设置原值必须保留: %s", out)
	}
	if probe.Tabs[0].FutureTab == nil || probe.FutureTop == nil || probe.Settings.FutureSet == nil {
		t.Fatalf("三层未知字段必须保留: %s", out)
	}
	if !strings.Contains(string(probe.Tabs[0].FutureTab), "1") {
		t.Fatalf("tab 未知字段内容丢失: %s", string(probe.Tabs[0].FutureTab))
	}

	// 与 core/model 对一轮往返：再解码后未知字段仍在，已知字段为覆盖后的值。
	doc, err := model.Decode([]byte(out), model.MigrateOptions{MaxTabCount: model.MaxTabCount})
	if err != nil {
		t.Fatal(err)
	}
	if doc.File.Tabs[1].Text != "新文" || doc.TabUnknown[1] == nil || doc.SettingsUnknown["futureSetting"] == nil ||
		doc.Unknown["futureTop"] == nil {
		t.Fatalf("model.Decode 回读不一致: %+v", doc)
	}
}

func TestEncodeNotesRejectsBadTabIDs(t *testing.T) {
	raw := `{"version":3,"active":0,"tabs":[{"id":1,"text":"甲"},{"id":2,"text":"乙"}]}`
	if _, err := EncodeNotes(raw, `[{"id":9,"text":"不存在的标签"}]`); err == nil ||
		!strings.HasPrefix(err.Error(), "invalid_args: ") {
		t.Fatalf("不存在的 id 必须报 invalid_args: %v", err)
	}
	if _, err := EncodeNotes(raw, `[{"id":1,"text":"a"},{"id":1,"text":"b"}]`); err == nil ||
		!strings.HasPrefix(err.Error(), "invalid_args: ") {
		t.Fatalf("重复 id 必须报 invalid_args: %v", err)
	}
	if _, err := EncodeNotes(raw, `{oops`); err == nil ||
		!strings.HasPrefix(err.Error(), "invalid_json: ") {
		t.Fatalf("非法 tabsJSON 必须报 invalid_json: %v", err)
	}
}

// TestEncodeNotesStableOnDesktopFile 桌面写盘形状的文件在无覆盖时逐字节稳定。
func TestEncodeNotesStableOnDesktopFile(t *testing.T) {
	f := desktopStyleFile()
	disk, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeNotes(string(disk), `[]`)
	if err != nil {
		t.Fatal(err)
	}
	if out != string(disk) {
		t.Fatalf("无未知字段时输出应与桌面写盘一致:\n got: %s\nwant: %s", out, disk)
	}
	// 覆盖一个标签后仍是 model.Encode 的形状（可再被 model.Decode 消费）。
	out2, err := EncodeNotes(string(disk), `[{"id":1,"name":"改名","text":"新内容","color":"#d4253a"}]`)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := model.Decode([]byte(out2), model.MigrateOptions{MaxTabCount: model.MaxTabCount})
	if err != nil {
		t.Fatalf("覆盖后的输出必须可被 model.Decode 消费: %v\n%s", err, out2)
	}
	if doc.File.Tabs[0].Name != "改名" || doc.File.Tabs[0].Text != "新内容" || doc.File.Tabs[1].Text != "第二个标签" {
		t.Fatalf("覆盖结果错误: %+v", doc.File.Tabs)
	}
}
