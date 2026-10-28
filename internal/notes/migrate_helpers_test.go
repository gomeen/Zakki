package notes

import "strings"

// Migrate 的测试用例已迁至 core/model（单一事实源）；这里只保留
// notes 其余测试构造 MigrateOptions / 初始 File 所需的助手。

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
