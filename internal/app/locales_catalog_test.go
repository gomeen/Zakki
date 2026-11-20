package app

import (
	"encoding/json"
	"os"
	"testing"

	"zakki/locales"
)

// 除源语言外还需与英文目录逐键对齐的目录语言。
var translatedLocales = []string{"zh-Hans", "ja", "zh-Hant"}

// TestLocalesCatalogParity 英文源目录与各译文目录的键位必须一一对应：
// 漏译会在英文界面下静默回退译文，该测试在提交前拦住。
func TestLocalesCatalogParity(t *testing.T) {
	read := func(path string) map[string]any {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return m
	}
	en := read("../../locales/en.json")
	keys := func(m map[string]any) map[string]bool {
		out := map[string]bool{}
		for k := range m {
			if k == "@@locale" || (len(k) > 0 && k[0] == '@') {
				continue
			}
			out[k] = true
		}
		return out
	}
	enKeys := keys(en)
	if len(enKeys) == 0 {
		t.Fatal("英文目录为空")
	}
	if en["@@locale"] != "en" {
		t.Errorf("@@locale 标注错误: %q", en["@@locale"])
	}
	for _, locale := range translatedLocales {
		m := read("../../locales/" + locale + ".json")
		gotKeys := keys(m)
		for k := range enKeys {
			if !gotKeys[k] {
				t.Errorf("%s 缺少消息 %q", locale, k)
			}
		}
		for k := range gotKeys {
			if !enKeys[k] {
				t.Errorf("%s 多出消息 %q（英文目录没有）", locale, k)
			}
		}
		if m["@@locale"] != locale {
			t.Errorf("@@locale 标注错误: 期望 %q，实际 %q", locale, m["@@locale"])
		}
	}
}

// TestLocalesEnglishDirect 直接取各目录文案，验证 catalog 与生成器可用
// （不依赖进程语言）。
func TestLocalesEnglishDirect(t *testing.T) {
	en := locales.In("en")
	if got := en.CommonOK(); got != "OK" {
		t.Fatalf("CommonOK(en) = %q", got)
	}
	if got := en.TabMaxCannotAdd(12); got != "Up to 12 tabs; cannot add another" {
		t.Fatalf("TabMaxCannotAdd(en,12) = %q", got)
	}
	zh := locales.In("zh-Hans")
	if got := zh.TabMaxCannotAdd(12); got != "最多 12 个标签，无法新增" {
		t.Fatalf("TabMaxCannotAdd(zh,12) = %q", got)
	}
	ja := locales.In("ja")
	if got := ja.TabMaxCannotAdd(12); got != "タブは最大 12 個までです。これ以上追加できません" {
		t.Fatalf("TabMaxCannotAdd(ja,12) = %q", got)
	}
	if got := ja.StatusBackedUpMinutes(5); got != "5 分前にバックアップ" {
		t.Fatalf("StatusBackedUpMinutes(ja,5) = %q", got)
	}
	zht := locales.In("zh-Hant")
	if got := zht.TabMaxCannotAdd(12); got != "最多 12 個分頁，無法新增" {
		t.Fatalf("TabMaxCannotAdd(zh-Hant,12) = %q", got)
	}
	if got := zht.StatusBackedUpMinutes(5); got != "5 分鐘前備份" {
		t.Fatalf("StatusBackedUpMinutes(zh-Hant,5) = %q", got)
	}
}
