package notes

import (
	"encoding/json"

	"zakki/core/model"
)

// zakki.json 的磁盘模型与迁移语义单一事实源在 core/model；
// 桌面经类型别名与转发使用，调用点（notes 其余文件与 internal/app）不感知包位置。

type (
	// File 是 zakki.json 的磁盘格式。
	File = model.File
	// Tab 是磁盘上的一条标签。
	Tab = model.Tab
	// Settings 是数据文件中的 settings 节。
	Settings = model.Settings
	// MigrateOptions 控制版本迁移（标签调色板与校验由 UI 层注入）。
	MigrateOptions = model.MigrateOptions
	// VersionError 表示数据版本高于当前支持。
	VersionError = model.VersionError
)

const (
	// CurrentDataVersion 是当前程序读写的数据版本。
	CurrentDataVersion = model.CurrentDataVersion
	// MarkdownThemeNote 是便签排版主题的当前 ID。
	MarkdownThemeNote = model.MarkdownThemeNote
)

// ErrTooManyTabs 表示数据中的标签数量超出桌面端可安全载入的上限。
var ErrTooManyTabs = model.ErrTooManyTabs

// HasHideOnEscapeKey 探测 settings.hideOnEscape 键是否存在。
func HasHideOnEscapeKey(raw []byte) bool {
	var probe struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return true
	}
	_, ok := probe.Settings["hideOnEscape"]
	return ok
}

// Migrate 把磁盘数据迁到当前版本。
func Migrate(f File, opt MigrateOptions) (File, error) { return model.Migrate(f, opt) }
