package zakkicore

import (
	"encoding/json"
	"fmt"
	"slices"
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

// ---- M2-4 标签管理绑定（AddNoteTab/DeleteNoteTab/ReorderNoteTabs） ----

// unknownLayersFixture 带三层未知字段与桌面专属 settings 的当前版本文件，
// 两个标签各带一个 tab 层未知字段；active 指向第二个标签。
func unknownLayersFixture() string {
	return `{"version":3,"active":1,"futureTop":{"keep":1},
	"tabs":[{"id":1,"name":"甲","text":"旧","color":"#d4253a","futureTab":[1]},
	        {"id":2,"name":"乙","text":"二","futureTab2":{"n":2}}],
	"settings":{"themeSource":"dark","alwaysOnTop":true,"hideOnEscape":false,"futureSetting":"x"}}`
}

// writeStyleTabs 生成 n 个当前版本标签的桌面写盘形状文件（id 1..n，逐个调色板色）。
func writeStyleTabs(t *testing.T, n, active int) string {
	t.Helper()
	palette := []string{"#d4253a", "#f67500", "#ffd01c", "#65c802", "#0f75e9", "#a40bd8", "#98989d", "#00a3b4", "#f7429c"}
	f := model.File{Version: model.CurrentDataVersion, Active: active}
	for i := 0; i < n; i++ {
		f.Tabs = append(f.Tabs, model.Tab{ID: uint64(i + 1), Name: fmt.Sprintf("标签%d", i+1), Text: fmt.Sprintf("t%d", i+1), Color: palette[i%len(palette)]})
	}
	disk, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(disk)
}

// decodeWriteDoc 把写路径输出重新过 model.Decode 消费（不迁移，MaxTabCount 只传上限）。
func decodeWriteDoc(t *testing.T, raw string) model.Document {
	t.Helper()
	doc, err := model.Decode([]byte(raw), model.MigrateOptions{MaxTabCount: model.MaxTabCount})
	if err != nil {
		t.Fatalf("结果必须可被 model.Decode 消费: %v\n%s", err, raw)
	}
	return doc
}

// addTabOut 调 AddNoteTab 并拆包契约返回形状。
func addTabOut(t *testing.T, raw, name, color string) (string, uint64) {
	t.Helper()
	out, err := AddNoteTab(raw, name, color)
	if err != nil {
		t.Fatalf("AddNoteTab: %v", err)
	}
	var res struct {
		Raw string `json:"raw"`
		ID  uint64 `json:"id"`
	}
	if e := json.Unmarshal([]byte(out), &res); e != nil {
		t.Fatalf("返回不是契约 JSON: %v\n%s", e, out)
	}
	return res.Raw, res.ID
}

func TestAddNoteTabAppendsAtEnd(t *testing.T) {
	raw := writeStyleTabs(t, 2, 1)
	out, id := addTabOut(t, raw, "新标签", "#00a3b4")
	if id != 3 {
		t.Fatalf("新 id = %d，期望现有最大 id+1 = 3", id)
	}
	doc := decodeWriteDoc(t, out)
	if len(doc.File.Tabs) != 3 {
		t.Fatalf("标签数 = %d，期望 3", len(doc.File.Tabs))
	}
	last := doc.File.Tabs[2]
	if last.ID != 3 || last.Name != "新标签" || last.Color != "#00a3b4" || last.Text != "" {
		t.Fatalf("新标签字段不符: %+v", last)
	}
	if doc.File.Active != 2 {
		t.Fatalf("active = %d，期望末尾新索引 2", doc.File.Active)
	}
	if doc.File.Tabs[0].Name != "标签1" || doc.File.Tabs[1].Name != "标签2" {
		t.Fatalf("既有标签不得变动: %+v", doc.File.Tabs)
	}
	// 读路径往返：DecodeNotes 对写出的文件报 ok 且 tabs/active 一致。
	snap := decodeNotesStatus(t, out)
	if !snap.Writable || snap.Reason != "ok" || snap.Active != 2 || len(snap.Tabs) != 3 {
		t.Fatalf("DecodeNotes 往返不一致: %+v", snap)
	}
}

func TestAddNoteTabEmptyFileStartsFromOne(t *testing.T) {
	raw := `{"version":3,"active":0,"tabs":[]}`
	out, id := addTabOut(t, raw, "", "#d4253a")
	if id != 1 {
		t.Fatalf("无标签文件新 id = %d，期望 1", id)
	}
	doc := decodeWriteDoc(t, out)
	if len(doc.File.Tabs) != 1 {
		t.Fatalf("无标签文件加标签后应为 1 个标签，得到 %d（不得补默认标签）", len(doc.File.Tabs))
	}
	if doc.File.Tabs[0].ID != 1 || doc.File.Active != 0 {
		t.Fatalf("id/active = %d/%d，期望 1/0", doc.File.Tabs[0].ID, doc.File.Active)
	}
}

func TestAddNoteTabPreservesUnknownLayers(t *testing.T) {
	out, _ := addTabOut(t, unknownLayersFixture(), "追加", "")
	var probe struct {
		Active int `json:"active"`
		Tabs   []struct {
			ID        uint64          `json:"id"`
			Name      string          `json:"name"`
			FutureTab json.RawMessage `json:"futureTab"`
		} `json:"tabs"`
		Settings struct {
			AlwaysOnTop  bool            `json:"alwaysOnTop"`
			HideOnEscape bool            `json:"hideOnEscape"`
			FutureSet    json.RawMessage `json:"futureSetting"`
		} `json:"settings"`
		FutureTop json.RawMessage `json:"futureTop"`
	}
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatal(err)
	}
	if probe.FutureTop == nil || probe.Tabs[0].FutureTab == nil || probe.Settings.FutureSet == nil {
		t.Fatalf("三层未知字段必须保留: %s", out)
	}
	if !probe.Settings.AlwaysOnTop || probe.Settings.HideOnEscape {
		t.Fatalf("桌面专属设置原值必须保留: %s", out)
	}
	if len(probe.Tabs) != 3 || probe.Tabs[2].ID != 3 || probe.Tabs[2].Name != "追加" || probe.Active != 2 {
		t.Fatalf("追加结果不符: %s", out)
	}
}

func TestAddNoteTabRejections(t *testing.T) {
	t.Run("已有 9 个报 invalid_args", func(t *testing.T) {
		if _, err := AddNoteTab(writeStyleTabs(t, model.MaxTabCount, 0), "x", ""); err == nil ||
			!strings.HasPrefix(err.Error(), "invalid_args: ") {
			t.Fatalf("9 满文件必须报 invalid_args: %v", err)
		}
	})
	t.Run("已有 10 个报 write_protected", func(t *testing.T) {
		if _, err := AddNoteTab(writeStyleTabs(t, model.MaxTabCount+1, 0), "x", ""); err == nil ||
			!strings.HasPrefix(err.Error(), "write_protected: ") {
			t.Fatalf("超限文件必须报 write_protected: %v", err)
		}
	})
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"版本超前", `{"version":4,"tabs":[{"id":1,"text":"甲"}]}`},
		{"legacy 版本", `{"version":2,"tabs":[{"id":1,"text":"甲"}]}`},
		{"文件损坏", `{oops`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AddNoteTab(tc.raw, "x", ""); err == nil ||
				!strings.HasPrefix(err.Error(), "write_protected: ") {
				t.Fatalf("必须报 write_protected: %v", err)
			}
		})
	}
}

func TestDeleteNoteTabActiveSemantics(t *testing.T) {
	// 桌面 deleteTabNow parity：删 active → min(i, len-1)；删其他 → 按 id 跟随。
	cases := []struct {
		name      string
		active    int
		deleteID  uint64
		wantIDs   []uint64
		wantIndex int // 期望 active 指向 wantIDs 中的这个位置
	}{
		{"删中间的 active", 1, 2, []uint64{1, 3}, 1},
		{"删首个的 active", 0, 1, []uint64{2, 3}, 0},
		{"删末尾的 active", 2, 3, []uint64{1, 2}, 1},
		{"删 active 前面的标签", 2, 1, []uint64{2, 3}, 1},
		{"删 active 后面的标签", 0, 3, []uint64{1, 2}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := DeleteNoteTab(writeStyleTabs(t, 3, tc.active), int64(tc.deleteID))
			if err != nil {
				t.Fatal(err)
			}
			doc := decodeWriteDoc(t, out)
			if doc.File.Active != tc.wantIndex {
				t.Fatalf("active = %d，期望 %d", doc.File.Active, tc.wantIndex)
			}
			gotIDs := make([]uint64, len(doc.File.Tabs))
			for i, tb := range doc.File.Tabs {
				gotIDs[i] = tb.ID
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Fatalf("剩余 id = %v，期望 %v", gotIDs, tc.wantIDs)
			}
			// 删除的标签未知字段/正文一并消失，剩余标签原样。
			if doc.File.Tabs[tc.wantIndex].Name != fmt.Sprintf("标签%d", tc.wantIDs[tc.wantIndex]) {
				t.Fatalf("剩余标签内容不符: %+v", doc.File.Tabs)
			}
		})
	}
}

func TestDeleteNoteTabRejections(t *testing.T) {
	single := writeStyleTabs(t, 1, 0)
	for _, tc := range []struct {
		name string
		raw  string
		id   int64
	}{
		{"只剩一个", single, 1},
		{"id 不存在", writeStyleTabs(t, 3, 0), 9},
		{"负数 id", writeStyleTabs(t, 3, 0), -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DeleteNoteTab(tc.raw, tc.id); err == nil ||
				!strings.HasPrefix(err.Error(), "invalid_args: ") {
				t.Fatalf("必须报 invalid_args: %v", err)
			}
		})
	}
	for _, raw := range []string{
		`{"version":4,"tabs":[{"id":1,"text":"甲"}]}`,
		`{"version":2,"tabs":[{"id":1,"text":"甲"}]}`,
	} {
		if _, err := DeleteNoteTab(raw, 1); err == nil || !strings.HasPrefix(err.Error(), "write_protected: ") {
			t.Fatalf("写保护文件必须拒绝删除: %v", err)
		}
	}
}

func TestDeleteNoteTabPreservesUnknownLayers(t *testing.T) {
	// 删非 active 的第二个标签：剩余标签的 tab 层未知字段与顶层/settings 层保留。
	out, err := DeleteNoteTab(unknownLayersFixture(), 2)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Active int `json:"active"`
		Tabs   []struct {
			ID        uint64          `json:"id"`
			FutureTab json.RawMessage `json:"futureTab"`
		} `json:"tabs"`
		Settings struct {
			AlwaysOnTop  bool            `json:"alwaysOnTop"`
			HideOnEscape bool            `json:"hideOnEscape"`
			FutureSet    json.RawMessage `json:"futureSetting"`
		} `json:"settings"`
		FutureTop json.RawMessage `json:"futureTop"`
	}
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Tabs) != 1 || probe.Tabs[0].ID != 1 || probe.Tabs[0].FutureTab == nil {
		t.Fatalf("剩余标签及其未知字段不符: %s", out)
	}
	if probe.FutureTop == nil || probe.Settings.FutureSet == nil {
		t.Fatalf("未知字段必须保留: %s", out)
	}
	// 删的是 active（磁盘 active=1 指向被删标签）：active 落到 min(1, 0)=0。
	if !probe.Settings.AlwaysOnTop || probe.Settings.HideOnEscape || probe.Active != 0 {
		t.Fatalf("桌面设置/active 不符: %s", out)
	}
}

func TestReorderNoteTabsSemantics(t *testing.T) {
	t.Run("active 按 id 跟随到新位置", func(t *testing.T) {
		out, err := ReorderNoteTabs(writeStyleTabs(t, 3, 0), "[3,1,2]")
		if err != nil {
			t.Fatal(err)
		}
		doc := decodeWriteDoc(t, out)
		gotIDs := make([]uint64, len(doc.File.Tabs))
		for i, tb := range doc.File.Tabs {
			gotIDs[i] = tb.ID
		}
		if !slices.Equal(gotIDs, []uint64{3, 1, 2}) {
			t.Fatalf("顺序 = %v，期望 [3 1 2]", gotIDs)
		}
		if doc.File.Active != 1 {
			t.Fatalf("active = %d，期望按 id 跟随到 1", doc.File.Active)
		}
	})
	t.Run("active 在末尾时跟随", func(t *testing.T) {
		out, err := ReorderNoteTabs(writeStyleTabs(t, 3, 2), "[3,1,2]")
		if err != nil {
			t.Fatal(err)
		}
		doc := decodeWriteDoc(t, out)
		if doc.File.Active != 0 || doc.File.Tabs[0].ID != 3 {
			t.Fatalf("active/首位 = %d/%d，期望 0/3", doc.File.Active, doc.File.Tabs[0].ID)
		}
	})
	t.Run("重排保真未知字段", func(t *testing.T) {
		out, err := ReorderNoteTabs(unknownLayersFixture(), "[2,1]")
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"futureTop", "futureTab", "futureTab2", "futureSetting"} {
			if !strings.Contains(out, key) {
				t.Fatalf("未知字段 %s 必须保留: %s", key, out)
			}
		}
		doc := decodeWriteDoc(t, out)
		if doc.File.Active != 0 || doc.File.Tabs[0].ID != 2 {
			t.Fatalf("active 应按 id 跟随到 0: %+v", doc.File)
		}
	})
	for _, tc := range []struct {
		name   string
		ids    string
		raw    string
		active int
	}{
		{"长度不一致", "[1,2]", writeStyleTabs(t, 3, 0), 0},
		{"id 不存在", "[1,2,9]", writeStyleTabs(t, 3, 0), 0},
		{"重复 id", "[1,1,2]", writeStyleTabs(t, 3, 0), 0},
		{"负数 id", "[-1,2,3]", writeStyleTabs(t, 3, 0), 0},
		{"非法 JSON", "{oops", writeStyleTabs(t, 3, 0), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReorderNoteTabs(tc.raw, tc.ids)
			if err == nil {
				t.Fatalf("必须拒绝 %s", tc.ids)
			}
			want := "invalid_args: "
			if tc.ids == "{oops" {
				want = "invalid_json: "
			}
			if !strings.HasPrefix(err.Error(), want) {
				t.Fatalf("错误码必须为 %s: %v", want, err)
			}
		})
	}
	for _, raw := range []string{
		`{"version":4,"tabs":[{"id":1,"text":"甲"}]}`,
		`{"version":2,"tabs":[{"id":1,"text":"甲"}]}`,
	} {
		if _, err := ReorderNoteTabs(raw, "[1]"); err == nil || !strings.HasPrefix(err.Error(), "write_protected: ") {
			t.Fatalf("写保护文件必须拒绝重排: %v", err)
		}
	}
}
