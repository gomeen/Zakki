package notes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zakki/core/model"
)

// threeLayerRaw 覆盖三层未知字段与已知字段交错；缩进为非规范形，
// 首次落盘会重写成 Encode 的规范形，正好检验未知字段跨重写保留。
const threeLayerRaw = `{
  "version": 3,
  "active": 0,
  "futureTop": {"kept": true},
  "tabs": [
    {"id": 1, "text": "甲", "color": "#d4253a", "futureTab": [1, 2]},
    {"id": 2, "text": "乙", "futureTab": {"v": "乙未知"}}
  ],
  "settings": {"themeSource": "dark", "futureSetting": "设置未知"}
}`

// decodeFile 用共享解码器读回磁盘文件，供未知字段断言。
func decodeFile(t *testing.T, path string) model.Document {
	doc, err := model.Decode(readRaw(t, path), testMigrateOpt())
	if err != nil {
		t.Fatalf("回读 %s 失败: %v", path, err)
	}
	return doc
}

// mustDecode 解码测试夹具，失败直接终止。
func mustDecode(t *testing.T, raw string) model.Document {
	t.Helper()
	doc, err := model.Decode([]byte(raw), testMigrateOpt())
	if err != nil {
		t.Fatalf("解码夹具失败: %v", err)
	}
	return doc
}

// wantThreeLayer 断言三层未知字段都在回读结果里。
func wantThreeLayer(t *testing.T, doc model.Document) {
	t.Helper()
	if string(doc.Unknown["futureTop"]) == "" ||
		doc.SettingsUnknown["futureSetting"] == nil ||
		doc.TabUnknown[1] == nil || doc.TabUnknown[2] == nil {
		t.Fatalf("三层未知字段必须保留: top=%v settings=%v tabs=%v",
			doc.Unknown, doc.SettingsUnknown, doc.TabUnknown)
	}
}

func TestUnknownFieldsSurviveLoadEditSave(t *testing.T) {
	s := newTestStore(t, nil)
	writeRaw(t, s.DataPath(), threeLayerRaw)
	f, exists, hadTabs, err := s.Load()
	if err != nil || !exists || !hadTabs {
		t.Fatalf("读取: exists=%v hadTabs=%v err=%v", exists, hadTabs, err)
	}
	f.Tabs[0].Text = "甲改"
	f.Active = 1
	if err := s.SaveNow(f); err != nil {
		t.Fatalf("SaveNow 报错: %v", err)
	}
	doc := decodeFile(t, s.DataPath())
	wantThreeLayer(t, doc)
	if doc.File.Tabs[0].Text != "甲改" || doc.File.Active != 1 {
		t.Fatalf("已知字段编辑未落盘: %+v", doc.File)
	}
	if !doc.File.Settings.HideOnEscape {
		t.Fatal("hideOnEscape 缺键载入后按 true 落盘")
	}
}

func TestUnknownFieldsDropWithDeletedTab(t *testing.T) {
	s := newTestStore(t, nil)
	writeRaw(t, s.DataPath(), threeLayerRaw)
	f, _, _, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	f.Tabs = []Tab{f.Tabs[1]}
	if err := s.SaveNow(f); err != nil {
		t.Fatalf("SaveNow 报错: %v", err)
	}
	doc := decodeFile(t, s.DataPath())
	if len(doc.File.Tabs) != 1 || doc.File.Tabs[0].ID != 2 {
		t.Fatalf("标签删除未落盘: %+v", doc.File.Tabs)
	}
	if doc.TabUnknown[1] != nil {
		t.Fatalf("被删标签的未知字段应从落盘文件消失: %+v", doc.TabUnknown)
	}
	if doc.TabUnknown[2] == nil || string(doc.TabUnknown[2]["futureTab"]) == "" {
		t.Fatalf("其余标签未知字段必须保留: %+v", doc.TabUnknown)
	}
	if string(doc.Unknown["futureTop"]) == "" || doc.SettingsUnknown["futureSetting"] == nil {
		t.Fatalf("顶层与 settings 未知字段必须保留: %+v", doc)
	}
}

func TestNewTabHasNoUnknownFields(t *testing.T) {
	s := newTestStore(t, nil)
	writeRaw(t, s.DataPath(), threeLayerRaw)
	f, _, _, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	f.Tabs = append(f.Tabs, Tab{ID: 3, Name: "新标签", Text: ""})
	if err := s.SaveNow(f); err != nil {
		t.Fatalf("SaveNow 报错: %v", err)
	}
	doc := decodeFile(t, s.DataPath())
	if doc.TabUnknown[3] != nil {
		t.Fatalf("新建标签不应带未知字段: %+v", doc.TabUnknown[3])
	}
	if doc.TabUnknown[1] == nil || doc.TabUnknown[2] == nil {
		t.Fatalf("既有标签未知字段必须保留: %+v", doc.TabUnknown)
	}
}

func TestRelocatePreservesUnknownFields(t *testing.T) {
	s := newTestStore(t, nil)
	// Zakki 与移动端落盘的都是规范形；先把三层未知字段文件按规范形写好，
	// 迁址的旧文件字节比对才能通过（手工乱序文件不在迁址测试范围）。
	base, err := model.Encode(mustDecode(t, threeLayerRaw))
	if err != nil {
		t.Fatal(err)
	}
	writeRaw(t, s.DataPath(), string(base))
	f, _, _, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := s.DataPath()
	target := filepath.Join(t.TempDir(), "zakki.json")
	if _, err := s.Relocate(target, f, nil); err != nil {
		t.Fatalf("Relocate 报错: %v", err)
	}
	wantThreeLayer(t, decodeFile(t, target))
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("旧文件应已删除: %v", err)
	}
}

func TestUnknownFieldsKeepWriteProtection(t *testing.T) {
	t.Run("高版本未知字段文件拒绝覆盖", func(t *testing.T) {
		s := newTestStore(t, nil)
		raw := `{"version":999,"tabs":[],"futureTop":1}`
		writeRaw(t, s.DataPath(), raw)
		if _, ok, _, err := s.Load(); ok || err == nil || !s.SaveBlocked() {
			t.Fatalf("高版本应写保护: %v", err)
		}
		if err := s.SaveNow(doc("覆盖")); !errors.Is(err, ErrSaveBlocked) {
			t.Fatalf("应拒写: %v", err)
		}
		if got := string(readRaw(t, s.DataPath())); got != raw {
			t.Fatal("原件变化")
		}
	})
	t.Run("未知字段文件的外部改动仍被检出", func(t *testing.T) {
		s := newTestStore(t, nil)
		writeRaw(t, s.DataPath(), threeLayerRaw)
		f, _, _, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		writeRaw(t, s.DataPath(), `{"version":3,"tabs":[{"id":1,"text":"外部","futureTab":1}],"settings":{}}`)
		if err := s.SaveNow(f); !errors.Is(err, ErrExternalChange) {
			t.Fatalf("外部改动应拒写: %v", err)
		}
		if got := string(readRaw(t, s.DataPath())); !strings.Contains(got, "外部") {
			t.Fatal("外部内容被覆盖")
		}
	})
}
