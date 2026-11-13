package notes

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"zakki/core/model"
)

// mtab 是合并测试用的标签简写。
type mtab struct {
	id   uint64
	text string
}

func mergeFile(active int, tabs ...mtab) []byte {
	f := doc("")
	f.Tabs = nil
	for _, t := range tabs {
		f.Tabs = append(f.Tabs, Tab{ID: t.id, Text: t.text, Color: testPalette[0]})
	}
	f.Active = active
	raw, err := encodeForTest(f)
	if err != nil {
		panic(err)
	}
	return raw
}

func decodeTabs(t *testing.T, raw []byte) (texts []string, ids []uint64, active int) {
	t.Helper()
	d, err := model.Decode(raw, testMigrateOpt())
	if err != nil {
		t.Fatalf("合并结果无法解码: %v", err)
	}
	for _, tb := range d.File.Tabs {
		texts = append(texts, tb.Text)
		ids = append(ids, tb.ID)
	}
	return texts, ids, d.File.Active
}

// 逐标签合并规则表：只一边改取改的一边；两边都改较新者胜；新增保留；
// 一边删、另一边没动则删；一边删、另一边改则保留改动。
func TestMergeSyncContentRules(t *testing.T) {
	base := mergeFile(0, mtab{1, "一"}, mtab{2, "二"}, mtab{3, "三"})
	cases := []struct {
		name          string
		local, remote []byte
		localNewer    bool
		want          []string
	}{
		{"不同标签各改各的",
			mergeFile(0, mtab{1, "一L"}, mtab{2, "二"}, mtab{3, "三"}),
			mergeFile(0, mtab{1, "一"}, mtab{2, "二"}, mtab{3, "三R"}),
			false, []string{"一L", "二", "三R"}},
		{"同一标签两边都改·本机较新",
			mergeFile(0, mtab{1, "一L"}, mtab{2, "二"}, mtab{3, "三"}),
			mergeFile(0, mtab{1, "一R"}, mtab{2, "二"}, mtab{3, "三"}),
			true, []string{"一L", "二", "三"}},
		{"同一标签两边都改·远端较新",
			mergeFile(0, mtab{1, "一L"}, mtab{2, "二"}, mtab{3, "三"}),
			mergeFile(0, mtab{1, "一R"}, mtab{2, "二"}, mtab{3, "三"}),
			false, []string{"一R", "二", "三"}},
		{"一边新增标签",
			mergeFile(0, mtab{1, "一"}, mtab{2, "二"}, mtab{3, "三"}, mtab{4, "四L"}),
			mergeFile(0, mtab{1, "一"}, mtab{2, "二R"}, mtab{3, "三"}),
			false, []string{"一", "二R", "三", "四L"}},
		{"一边删、另一边没动→删",
			mergeFile(0, mtab{1, "一"}, mtab{3, "三L"}),
			mergeFile(0, mtab{1, "一"}, mtab{2, "二"}, mtab{3, "三"}),
			false, []string{"一", "三L"}},
		{"一边删、另一边改→保留改动",
			mergeFile(0, mtab{1, "一"}, mtab{3, "三"}),
			mergeFile(0, mtab{1, "一"}, mtab{2, "二R"}, mtab{3, "三"}),
			true, []string{"一", "三", "二R"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, perTab := mergeSyncContent(base, tc.local, tc.remote, tc.localNewer, testMigrateOpt())
			if !perTab {
				t.Fatal("应逐标签合并")
			}
			got, _, _ := decodeTabs(t, merged)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("合并 = %q, want %q", got, tc.want)
			}
		})
	}
}

// 两边各自新建了同 ID 标签：两条都保留，较旧一侧换新 ID。
func TestMergeBothCreatedSameID(t *testing.T) {
	base := mergeFile(0, mtab{1, "一"})
	local := mergeFile(0, mtab{1, "一"}, mtab{2, "本机新建"})
	remote := mergeFile(0, mtab{1, "一"}, mtab{2, "远端新建"})
	merged, perTab := mergeSyncContent(base, local, remote, true, testMigrateOpt())
	if !perTab {
		t.Fatal("应逐标签合并")
	}
	texts, ids, _ := decodeTabs(t, merged)
	if strings.Join(texts, "|") != "一|本机新建|远端新建" {
		t.Fatalf("两条新建都应保留: %q", texts)
	}
	if ids[1] == ids[2] {
		t.Fatalf("撞号的标签应换新 ID: %v", ids)
	}
}

// 当前标签跟本机走，合并不把用户正在看的标签切走。
func TestMergeKeepsLocalActiveTab(t *testing.T) {
	base := mergeFile(0, mtab{1, "一"}, mtab{2, "二"}, mtab{3, "三"})
	local := mergeFile(2, mtab{1, "一"}, mtab{2, "二"}, mtab{3, "三L"})
	remote := mergeFile(0, mtab{1, "一R"}, mtab{2, "二"}, mtab{3, "三"})
	merged, _ := mergeSyncContent(base, local, remote, false, testMigrateOpt())
	texts, _, active := decodeTabs(t, merged)
	if texts[active] != "三L" {
		t.Fatalf("当前标签应仍是本机的第 3 个: active=%d %q", active, texts)
	}
}

// 无法逐标签合并时退化为整个文件较新者胜：缺基线原文、任一侧损坏、结果超上限。
func TestMergeFallsBackToWholeFile(t *testing.T) {
	local := mergeFile(0, mtab{1, "本机"})
	remote := mergeFile(0, mtab{1, "远端"})
	t.Run("旧基线没有原文", func(t *testing.T) {
		if got, perTab := mergeSyncContent(nil, local, remote, false, testMigrateOpt()); perTab || !bytes.Equal(got, remote) {
			t.Fatalf("应整体取较新的远端: perTab=%v", perTab)
		}
		if got, _ := mergeSyncContent(nil, local, remote, true, testMigrateOpt()); !bytes.Equal(got, local) {
			t.Fatal("应整体取较新的本机")
		}
	})
	t.Run("任一侧损坏", func(t *testing.T) {
		if _, perTab := mergeSyncContent(local, []byte("{bad"), remote, false, testMigrateOpt()); perTab {
			t.Fatal("损坏内容不应逐标签合并")
		}
	})
	t.Run("合并后超过标签上限", func(t *testing.T) {
		opt := testMigrateOpt()
		opt.MaxTabCount = 3
		base := mergeFile(0, mtab{1, "一"})
		l := mergeFile(0, mtab{1, "一"}, mtab{2, "L2"}, mtab{3, "L3"})
		r := mergeFile(0, mtab{1, "一"}, mtab{4, "R4"}, mtab{5, "R5"})
		if got, perTab := mergeSyncContent(base, l, r, true, opt); perTab || !bytes.Equal(got, l) {
			t.Fatalf("超上限应整体取较新者: perTab=%v", perTab)
		}
	})
}

// 标签的未知字段跟着标签一起去留（移动端扩展数据不因合并丢失）。
func TestMergeKeepsTabUnknownFields(t *testing.T) {
	withExtra := func(raw []byte, extra string) []byte {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		tabs := m["tabs"].([]any)
		tabs[1].(map[string]any)["x-mobile"] = extra
		out, _ := json.Marshal(m)
		return out
	}
	base := mergeFile(0, mtab{1, "一"}, mtab{2, "二"})
	local := mergeFile(0, mtab{1, "一L"}, mtab{2, "二"})
	remote := withExtra(mergeFile(0, mtab{1, "一"}, mtab{2, "二"}), "远端扩展")
	merged, perTab := mergeSyncContent(base, local, remote, false, testMigrateOpt())
	if !perTab || !bytes.Contains(merged, []byte("远端扩展")) {
		t.Fatalf("远端标签的未知字段应保留: perTab=%v %s", perTab, merged)
	}
}

// FinalSync：本机与基线一致时不碰网络；有未推送的修改则在 Shutdown 后推一轮。
func TestFinalSync(t *testing.T) {
	s, be, _ := newSyncStore(t, "基线")
	be.existed, be.remote, be.version = true, docBytes("基线"), "r1"
	if st := startSync(t, s, be); st.Error != "" {
		t.Fatal(st.Error)
	}
	if err := s.FinalSync(time.Second); err != nil {
		t.Fatal(err)
	}
	if _, _, _, pulls, _ := be.snapshot(); pulls != 1 {
		t.Fatalf("内容未变不应访问网络: pulls=%d", pulls)
	}
	if err := s.Shutdown(doc("退出前的修改")); err != nil {
		t.Fatal(err)
	}
	if err := s.FinalSync(time.Second); err != nil {
		t.Fatal(err)
	}
	if remote, _, _, _, _ := be.snapshot(); !bytes.Equal(remote, docBytes("退出前的修改")) {
		t.Fatalf("退出前的修改应已推送: %s", remote)
	}
}

// FinalSync 超时：网络卡住时按时返回错误，不把退出拖住。
func TestFinalSyncTimeout(t *testing.T) {
	s, fb, _ := newSyncStore(t, "基线")
	fb.existed, fb.remote, fb.version = true, docBytes("基线"), "r1"
	if st := startSync(t, s, fb); st.Error != "" {
		t.Fatal(st.Error)
	}
	if err := s.Shutdown(doc("未推送")); err != nil {
		t.Fatal(err)
	}
	s.ConfigureSync(blockingBackend{fb}, "gomeen/notes", readBaselinePath(s), nil)
	start := time.Now()
	if err := s.FinalSync(50 * time.Millisecond); err == nil {
		t.Fatal("超时应返回错误")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("超时未及时返回: %v", d)
	}
}

// blockingBackend 的 Pull 一直阻塞到上下文取消，模拟网络卡住。
type blockingBackend struct{ *fakeSyncBackend }

func (blockingBackend) Pull(ctx context.Context) ([]byte, string, bool, error) {
	<-ctx.Done()
	return nil, "", false, ctx.Err()
}

func readBaselinePath(s *Store) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncStateFile
}
