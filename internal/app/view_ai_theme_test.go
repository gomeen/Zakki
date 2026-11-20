package app

// AI 建议卡配色的对比度矩阵：全部颜色主题 × 明暗、经典中性灰变体与
// 七点派生盘上，diff 墨色对底色 ≥ 4.5，卡片描边对卡面可见。

import (
	"testing"
)

func TestAIColorsContrast(t *testing.T) {
	check := func(name string, pal colorPalette, cols aiColors) {
		t.Helper()
		if r := contrastRatio(cols.delInk, cols.delBg); r < 4.5 {
			t.Errorf("%s 删除墨色对底色 %.2f < 4.5", name, r)
		}
		if r := contrastRatio(cols.addInk, cols.addBg); r < 4.5 {
			t.Errorf("%s 新增墨色对底色 %.2f < 4.5", name, r)
		}
		if r := contrastRatio(cols.delInk, pal.background); r < 4.5 {
			t.Errorf("%s 删除墨色对主题背景 %.2f < 4.5", name, r)
		}
		if r := contrastRatio(cols.addInk, pal.background); r < 4.5 {
			t.Errorf("%s 新增墨色对主题背景 %.2f < 4.5", name, r)
		}
		if r := contrastRatio(cols.cardBorder, cols.cardBg); r < 1.2 {
			t.Errorf("%s 卡片描边对卡面 %.2f < 1.2", name, r)
		}
	}
	for _, def := range append([]colorThemeDef{{id: "classic", light: classicNeutralPalette(false), dark: classicNeutralPalette(true)}}, colorThemes...) {
		for _, dark := range []bool{false, true} {
			pal := def.light
			if dark {
				pal = def.dark
			}
			mode := "light"
			if dark {
				mode = "dark"
			}
			check(def.id+"/"+mode, pal, aiColorsFor(def.id, pal, dark))
		}
	}
	// 经典主题随点配色：七个点各自派生。
	for i := 0; i < 7; i++ {
		for _, dark := range []bool{false, true} {
			pal := dotPalette(i, dark)
			mode := "light"
			if dark {
				mode = "dark"
			}
			check("classic-dot"+string(rune('1'+i))+"/"+mode, pal, aiColorsFor("classic", pal, dark))
		}
	}
}

// TestAIColorsSeedsDistinct 覆盖锚点主题的配色确实偏向主题基调（与默认
// 锚点不同），避免覆盖表名存实亡。
func TestAIColorsSeedsDistinct(t *testing.T) {
	palLight := colorThemes[0].light
	base := aiColorsFor("paper", palLight, false)
	for _, id := range []string{"sketch", "claude", "qoder"} {
		def, ok := colorThemeByID(id)
		if !ok {
			t.Fatalf("主题 %s 缺失", id)
		}
		got := aiColorsFor(id, def.light, false)
		if got.delInk == base.delInk && got.addInk == base.addInk {
			t.Errorf("主题 %s 的建议卡配色与默认完全相同，覆盖未生效", id)
		}
	}
}
