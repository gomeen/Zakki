// Package model 定义唯一笔记文件 zakki.json 的共享磁盘模型与版本迁移语义，
// 桌面 internal/notes 与移动端绑定从这一份实现消费。
package model

import (
	"errors"
	"fmt"
)

// CurrentDataVersion 是当前程序读写的数据版本。
// 版本 3 起排版主题 ID 更名："tot" → "note"（应用更名去品牌化）；
// 旧版本程序读到版本 3 会按「版本超前」拒绝并保留原文件。
const CurrentDataVersion = 3

const (
	// MaxTabCount 是磁盘标签数的硬上限（桌面数字快捷键 ⌘N / Ctrl+N 恰好全覆盖）。
	MaxTabCount = 9
	// DefaultTabCount 是新建数据时的默认标签数（Tot 的 7 个点）。
	DefaultTabCount = 7
)

// MarkdownThemeNote 是便签排版主题的当前 ID；legacyMarkdownThemeTot 是
// 更名前旧文件里的写法，载入时映射到新 ID。
const (
	MarkdownThemeNote      = "note"
	legacyMarkdownThemeTot = "tot"
)

// Settings 是数据文件中的 settings 节。
type Settings struct {
	ThemeSource   string  `json:"themeSource"`
	MarkdownTheme string  `json:"markdownTheme,omitempty"`
	BodyFont      string  `json:"bodyFont,omitempty"`
	MonoFont      string  `json:"monoFont,omitempty"`
	BodySize      float32 `json:"bodySize,omitempty"`
	LineHeight    float32 `json:"lineHeight,omitempty"`
	AlwaysOnTop   bool    `json:"alwaysOnTop"`
	HideOnEscape  bool    `json:"hideOnEscape"`
	Source        bool    `json:"source"`
	Preview       bool    `json:"preview"`
	TabTitleWidth float32 `json:"tabTitleWidth,omitempty"`
	TabAnimSpeed  float32 `json:"tabAnimSpeed,omitempty"`
	MonoDots      bool    `json:"monoDots"`
	GlassStrength float32 `json:"glassStrength,omitempty"`
	// TabMotionOff 关闭顶栏液态切换动画（缺键 false = 开启动画）。
	TabMotionOff bool `json:"tabMotionOff"`
	// TabDotShape 是非当前标签圆点的形状；空串或未知值按 "dot" 渲染。
	TabDotShape string `json:"tabDotShape,omitempty"`
}

// Tab 是磁盘上的一条标签。
type Tab struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
}

// File 是 zakki.json 的磁盘格式。
type File struct {
	Version  int      `json:"version"`
	Active   int      `json:"active"`
	Tabs     []Tab    `json:"tabs,omitempty"`
	Dots     []string `json:"dots,omitempty"`
	Settings Settings `json:"settings"`
}

// MigrateOptions 控制版本迁移（标签调色板与校验由 UI 层注入）。
type MigrateOptions struct {
	DefaultTabCount int
	MaxTabCount     int
	PaletteColors   []string
	ValidTabColor   func(string) bool
}

// VersionError 表示数据版本高于当前支持。
type VersionError struct{ Version int }

func (e *VersionError) Error() string {
	return fmt.Sprintf("数据版本 %d 高于当前支持的 %d，当前版本无法读取", e.Version, CurrentDataVersion)
}

// ErrTooManyTabs 表示数据中的标签数量超出桌面端可安全载入的上限。
var ErrTooManyTabs = errors.New("标签数量超过上限")

// Migrate 把磁盘数据迁到当前版本。
func Migrate(f File, opt MigrateOptions) (File, error) {
	if f.Version > CurrentDataVersion {
		return File{}, &VersionError{Version: f.Version}
	}
	if f.Version < 2 && len(f.Tabs) == 0 {
		dots := f.Dots
		if len(dots) < opt.DefaultTabCount {
			padded := make([]string, opt.DefaultTabCount)
			copy(padded, dots)
			dots = padded
		}
		f.Tabs = tabsFromDots(dots, opt.PaletteColors, opt.DefaultTabCount)
	}
	if f.Version < 3 && f.Settings.MarkdownTheme == legacyMarkdownThemeTot {
		f.Settings.MarkdownTheme = MarkdownThemeNote
	}
	if len(f.Tabs) > opt.MaxTabCount {
		return File{}, fmt.Errorf("%w：%d 个，最多 %d 个；请先在其他设备减少标签后重新读取", ErrTooManyTabs, len(f.Tabs), opt.MaxTabCount)
	}
	f.Dots = nil
	f.Tabs = normalizeTabs(f.Tabs, opt)
	f.Active = clamp(f.Active, 0, len(f.Tabs)-1)
	if f.Settings.ThemeSource == "" {
		f.Settings.ThemeSource = "system"
	}
	f.Settings.Source = f.Settings.Source || f.Settings.Preview
	f.Version = CurrentDataVersion
	return f, nil
}

func tabsFromDots(dots []string, palette []string, n int) []Tab {
	tabs := make([]Tab, n)
	for i := range tabs {
		text := ""
		if i < len(dots) {
			text = dots[i]
		}
		color := ""
		if i < len(palette) {
			color = palette[i]
		}
		tabs[i] = Tab{ID: uint64(i + 1), Text: text, Color: color}
	}
	return tabs
}

func normalizeTabs(tabs []Tab, opt MigrateOptions) []Tab {
	if len(tabs) == 0 {
		tabs = tabsFromDots(nil, opt.PaletteColors, opt.DefaultTabCount)
	}
	seen := map[uint64]bool{}
	nextID := uint64(1)
	alloc := func() uint64 {
		for seen[nextID] {
			nextID++
		}
		seen[nextID] = true
		id := nextID
		nextID++
		return id
	}
	for i := range tabs {
		if tabs[i].ID == 0 || seen[tabs[i].ID] {
			tabs[i].ID = alloc()
		} else {
			seen[tabs[i].ID] = true
		}
		if opt.ValidTabColor != nil && !opt.ValidTabColor(tabs[i].Color) {
			tabs[i].Color = ""
		}
	}
	return tabs
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
