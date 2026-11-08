package prefs

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"zakki/internal/fonts"
)

// CurrentVersion 是当前程序读写的偏好版本。
// 版本 2 起 appearance 分开记录中文字体（bodyFont）与西文字体（latinFont）。
// 版本 3 起 backup 记录外部备份目的地（mirrorDir）与 Git 自动化模式（gitMode）；
// 旧版本程序读到版本 3 会按「版本超前」拒绝并保留原文件。
// 版本 4 起主题 ID 更名：colorTheme "tot" → "classic"、markdownTheme "tot" → "note"。
// 版本 5 起代码块配色默认「柔和」：旧版本写入方从未写过 codeTheme 键，升级时统一补为
// soft；版本 5 起该键始终落盘，空串是显式的「跟随文本」，不再参与补默认。
// 版本 6 起备份只有一个文件夹：backup.mirrorDir 更名为 backup.backupDir 并成为
// 唯一备份存储地址；gitMode 随 Git 自动化一并移除，旧键由迁移读取后丢弃。
// 版本 7 起代码块缩进只有 2 / 4 空格两档（appearance.codeIndent，默认 2 空格）；
// 旧文件从未写过该键，升级时统一补默认。
// 版本 8 扩展既有字体键：支持 local:<真实家族名>，西文与代码字体另支持 fira；
// 不新增 JSON 字段，不改默认字体或触发下载。
// 版本 9 起备份文件夹支持 Git 自动化：backup.gitAutoPush（开关，默认关）与
// backup.gitPushMinutes（自动推送最小间隔，默认 10，1–1440）。旧文件从未写过
// 这两个键，迁移时统一补默认；无破坏性变更。
// 版本 10 新增 appearance.style（"standard" / "sketch"）：手账是整体风格，
// colorTheme / markdownTheme 不再取 "sketch"，只记标准风格下的选择，切回标准时原样恢复。
// 版本 11 把 advanced.windowWidth / windowHeight（像素）换成 windowColumns /
// windowRows（每行字数 × 行数），开窗时按当时的字号换算；旧像素值按默认排版换算。
// 版本 12 以 storage / git 取代备份策略：backup 整节与 autosaveDelayMs 在读取时
// 静默摘除（不迁移语义，含开发期写出的版本 12 带旧键文件），写回即为版本 12。
// 版本 13 新增 appearance.padX / padY（正文区左右 / 上下内边距覆盖，DIP；
// 0 = 跟随排版主题）。旧文件从未写过这两个键，解码即零值 = 跟随，无需迁移。
// 版本 14 新增 sync 节：云同步开关、后端与仓库标识，全部为非凭据配置
// （token 一律进系统安全存储，绝不写入偏好文件，R7）。旧文件缺键解码即
// 零值（关闭），无需迁移逻辑；开启同步时桌面暂停 git.autoCommit /
// git.autoPush 两个旧调度器，用户原开关保留在本节之外的 git 节。
const CurrentVersion = 14

// 整体风格：手账风格下颜色与排版由风格决定，colorTheme / markdownTheme 保留不动。
const (
	StyleStandard = "standard"
	StyleSketch   = "sketch"
)

// AccelValidator 校验 keymap 覆盖中的 accelerator 字符串。
type AccelValidator func(acc string) error

// Preferences 是 preferences.json 的磁盘模型。
type Preferences struct {
	Version    int        `json:"version"`
	Appearance Appearance `json:"appearance"`
	Window     Window     `json:"window"`
	Storage    Storage    `json:"storage"`
	Git        Git        `json:"git"`
	Sync       Sync       `json:"sync"`
	Keymap     Keymap     `json:"keymap"`
	Advanced   Advanced   `json:"advanced"`
}

type Appearance struct {
	AppearanceMode string `json:"appearanceMode"`
	// Style 是整体风格；手账风格下实际生效的颜色与排版见 EffectiveColorTheme /
	// EffectiveMarkdownTheme。
	Style         string `json:"style"`
	ColorTheme    string `json:"colorTheme"`
	MarkdownTheme string `json:"markdownTheme"`
	// Language 是界面语言；空串 = 跟随系统（MyGo 按用户系统语言挑目录语言）。
	Language string `json:"language,omitempty"`
	// CodeTheme 是代码块着色方案；空串 = 跟随文本（不上色）。不能加 omitempty：
	// 空串必须落盘，否则「显式跟随文本」与「从未设置」在下次解码时无法区分。
	CodeTheme string `json:"codeTheme"`
	// CodeIndent 是代码块内 Tab 插入的缩进档位："space2"（默认）/ "space4"
	// 空格；Shift+Tab 按同一档位反向移除。
	CodeIndent string `json:"codeIndent"`
	// BodyFont 是中文字体；LatinFont 是英文字体，空串表示跟随中文字体。
	BodyFont   string  `json:"bodyFont"`
	LatinFont  string  `json:"latinFont"`
	MonoFont   string  `json:"monoFont"`
	BodySize   float32 `json:"bodySize"`
	LineHeight float32 `json:"lineHeight"`
	// PadX 是正文区左右内边距，PadY 是上下内边距（DIP）；0 = 跟随排版主题。
	PadX float32 `json:"padX"`
	PadY float32 `json:"padY"`
}

type Window struct {
	AlwaysOnTop    bool `json:"alwaysOnTop"`
	HideOnEscape   bool `json:"hideOnEscape"`
	RememberBounds bool `json:"rememberBounds"`
}

// Storage 是笔记数据路径与保存间隔；空路径由应用层解析为固定数据目录。
type Storage struct {
	DataFile       string `json:"dataFile"`
	SaveIntervalMS int    `json:"saveIntervalMs"`
}

// Git 是独立的自动提交与推送策略，推送必须同时开启自动提交。
type Git struct {
	AutoCommit    bool `json:"autoCommit"`
	AutoPush      bool `json:"autoPush"`
	CommitMinutes int  `json:"commitMinutes"`
	PushMinutes   int  `json:"pushMinutes"`
}

// SyncBackendGitHub 是后端 A（GitHub Contents API，sha CAS）的 backend 标识。
const SyncBackendGitHub = "github"

// Sync 是云同步偏好，只记录开关与后端指向等非凭据配置；token / refresh
// token 一律进系统安全存储，绝不写入本节或任何偏好文件（R7）。
// 开启同步即暂停 git.autoCommit / git.autoPush 两个旧调度器（§2.2），
// 用户原开关原样保留；关闭同步后按保留配置恢复备份功能。
type Sync struct {
	// Enabled 开启云同步；开启时要求 Backend 与 Repo 合法（见 Validate）。
	Enabled bool `json:"enabled"`
	// Backend 是激活的同步后端："github"（后端 A）；空串 = 未选择。
	Backend string `json:"backend"`
	// Repo 是后端仓库标识 owner/name；后端为云盘时复用此键记录文件夹标识。
	Repo string `json:"repo"`
}

type Keymap struct {
	Overrides map[string]string `json:"overrides"`
}

type Advanced struct {
	// WindowColumns / WindowRows 是新建主窗口的默认大小：每行字数 × 行数。
	WindowColumns int `json:"windowColumns"`
	WindowRows    int `json:"windowRows"`
}

// Default 全新安装的默认值。
func Default() Preferences {
	return Preferences{
		Version: CurrentVersion,
		Appearance: Appearance{
			AppearanceMode: "system",
			Style:          StyleStandard,
			ColorTheme:     "classic",
			MarkdownTheme:  "sonnet",
			CodeTheme:      "soft",
			CodeIndent:     "space2",
			LatinFont:      "jetbrains",
		},
		Window:  Window{AlwaysOnTop: false, HideOnEscape: true, RememberBounds: true},
		Storage: Storage{SaveIntervalMS: 2000},
		Git:     Git{CommitMinutes: 1, PushMinutes: 10},
		Keymap:  Keymap{Overrides: map[string]string{}},
		Advanced: Advanced{
			WindowColumns: 22,
			WindowRows:    16,
		},
	}
}

// Clone 深拷贝 Keymap.Overrides。
func (p Preferences) Clone() Preferences {
	out := p
	out.Keymap.Overrides = make(map[string]string, len(p.Keymap.Overrides))
	for k, v := range p.Keymap.Overrides {
		out.Keymap.Overrides[k] = v
	}
	return out
}

// Validate 校验全部取值范围。
func (p Preferences) Validate(v AccelValidator) error {
	for _, field := range []struct {
		name, value string
		allowed     []string
	}{
		{"appearance.appearanceMode", p.Appearance.AppearanceMode, []string{"system", "light", "dark"}},
		{"appearance.language", p.Appearance.Language, []string{"", "en", "zh-Hans"}},
		{"appearance.style", p.Appearance.Style, []string{StyleStandard, StyleSketch}},
		{"appearance.colorTheme", p.Appearance.ColorTheme, []string{"classic", "paper", "warmSand", "mistTeal", "graphite", "claude", "qoder"}},
		{"appearance.markdownTheme", p.Appearance.MarkdownTheme, []string{"sonnet", "note", "clear", "book", "compact"}},
		{"appearance.codeTheme", p.Appearance.CodeTheme, []string{"", "soft", "vivid", "claude", "qoder"}},
		{"appearance.codeIndent", p.Appearance.CodeIndent, []string{"space2", "space4"}},
		{"appearance.bodyFont", p.Appearance.BodyFont, []string{"", "kai", "sonnet", "pingfang", "songti", "hand", "hannotate"}},
		{"appearance.latinFont", p.Appearance.LatinFont, []string{"", "jetbrains", "system", "noteworthy", "bradley", "fira"}},
		{"appearance.monoFont", p.Appearance.MonoFont, []string{"", "jbm", "sonnet", "sfmono", "menlo", "fira"}},
	} {
		valid := false
		for _, allowed := range field.allowed {
			valid = valid || field.value == allowed
		}
		if !valid {
			switch field.name {
			case "appearance.bodyFont", "appearance.latinFont", "appearance.monoFont":
				valid = fonts.LocalFamily(field.value) != ""
			}
		}
		if !valid {
			return fmt.Errorf("%s 的值 %q 无效，可选值：%q", field.name, field.value, field.allowed)
		}
	}
	for _, field := range []struct {
		name          string
		value, lo, hi float32
		allowFollow   bool
	}{
		{"appearance.bodySize", p.Appearance.BodySize, 10, 40, true},
		{"appearance.lineHeight", p.Appearance.LineHeight, 1, 3, true},
		{"appearance.padX", p.Appearance.PadX, 8, 120, true},
		{"appearance.padY", p.Appearance.PadY, 8, 120, true},
	} {
		if field.allowFollow && field.value == 0 {
			continue
		}
		if math.IsNaN(float64(field.value)) || math.IsInf(float64(field.value), 0) || field.value < field.lo || field.value > field.hi {
			return fmt.Errorf("%s 必须在 %g–%g 之间（0 表示跟随主题）", field.name, field.lo, field.hi)
		}
	}
	for _, field := range []struct {
		name          string
		value, lo, hi int
	}{
		{"storage.saveIntervalMs", p.Storage.SaveIntervalMS, 100, 60000},
		{"git.commitMinutes", p.Git.CommitMinutes, 1, 1440},
		{"git.pushMinutes", p.Git.PushMinutes, 1, 1440},
		{"advanced.windowColumns", p.Advanced.WindowColumns, MinWindowColumns, MaxWindowColumns},
		{"advanced.windowRows", p.Advanced.WindowRows, MinWindowRows, MaxWindowRows},
	} {
		if field.value < field.lo || field.value > field.hi {
			return fmt.Errorf("%s 必须在 %d–%d 之间", field.name, field.lo, field.hi)
		}
	}
	if path := p.Storage.DataFile; path != "" {
		if strings.TrimSpace(path) != path || strings.ContainsRune(path, '\x00') || !filepath.IsAbs(path) {
			return errors.New("storage.dataFile 必须是合法的非空白绝对路径（空串表示默认路径）")
		}
	}
	if p.Git.AutoPush && !p.Git.AutoCommit {
		return errors.New("git.autoPush 要求开启 git.autoCommit")
	}
	switch p.Sync.Backend {
	case "", SyncBackendGitHub:
	default:
		return fmt.Errorf("sync.backend 的值 %q 无效，可选值：%q", p.Sync.Backend, []string{"", SyncBackendGitHub})
	}
	if p.Sync.Repo != "" || p.Sync.Enabled {
		if err := validateSyncRepo(p.Sync.Repo); err != nil {
			return err
		}
	}
	if p.Sync.Enabled && p.Sync.Backend == "" {
		return errors.New("sync.enabled 开启时必须选择 sync.backend")
	}
	if v != nil {
		for id, acc := range p.Keymap.Overrides {
			if acc == "" {
				continue
			}
			if err := v(acc); err != nil {
				return fmt.Errorf("keymap.overrides[%s] 的快捷键 %q 无效: %w", id, acc, err)
			}
		}
	}
	return nil
}

// validateSyncRepo 校验 sync.repo 的 owner/name 形状；非法值在设置页
// 以行内错误呈现，不会写入磁盘。
func validateSyncRepo(repo string) error {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return errors.New("sync.repo 必须是 owner/name 形式的仓库标识")
	}
	for _, part := range []string{owner, name} {
		if part == "." || part == ".." || len(part) > 100 {
			return errors.New("sync.repo 必须是 owner/name 形式的仓库标识")
		}
		for _, r := range part {
			valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
				r == '-' || r == '_' || r == '.'
			if !valid {
				return errors.New("sync.repo 只允许字母、数字与 . _ -（owner/name）")
			}
		}
	}
	return nil
}

// Decode 严格解码偏好文件。
func Decode(raw []byte, v AccelValidator) (Preferences, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return Preferences{}, err
	}
	if _, ok := value.(map[string]any); !ok {
		return Preferences{}, errors.New("偏好必须是 JSON 对象")
	}
	if err := rejectJSONNull(value, "preferences"); err != nil {
		return Preferences{}, err
	}
	// 两个摘除都必须执行：像素键与备份键可能同时存在，不能让 || 短路掉后者。
	legacyW, legacyH, hasLegacyWindow, err := stripLegacyWindowKeys(value)
	if err != nil {
		return Preferences{}, err
	}
	storageStripped := stripLegacyStorageKeys(value)
	if hasLegacyWindow || storageStripped {
		// 严格解码不认旧键；重新编回 JSON 再解码。map 编码只影响键序，
		// 不改变取值，原文件保持原样。
		sanitized, err := json.Marshal(value)
		if err != nil {
			return Preferences{}, err
		}
		raw = sanitized
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p Preferences
	if err := dec.Decode(&p); err != nil {
		return Preferences{}, err
	}
	if p.Keymap.Overrides == nil {
		p.Keymap.Overrides = map[string]string{}
	}
	if p.Version > CurrentVersion {
		return Preferences{}, fmt.Errorf("偏好版本 %d 高于当前支持的 %d（文件保持原样）", p.Version, CurrentVersion)
	}
	p = migrate(p)
	def := Default()
	if p.Appearance.AppearanceMode == "" {
		p.Appearance.AppearanceMode = def.Appearance.AppearanceMode
	}
	if p.Appearance.ColorTheme == "" {
		p.Appearance.ColorTheme = def.Appearance.ColorTheme
	}
	if p.Appearance.MarkdownTheme == "" {
		p.Appearance.MarkdownTheme = def.Appearance.MarkdownTheme
	}
	if p.Appearance.Style == "" {
		p.Appearance.Style = def.Appearance.Style
	}
	// codeTheme 不做空串补默认：空串是显式「跟随文本」，默认柔和由版本 5 迁移承担。
	// codeIndent 只认 2 / 4 空格两档：版本 7 早期内部构建写过的 tab / space8
	// 档位（未发布）归为 2 空格，缺键或空串补默认，非法取值仍由 Validate 拒绝。
	if p.Appearance.CodeIndent == "tab" || p.Appearance.CodeIndent == "space8" {
		p.Appearance.CodeIndent = "space2"
	}
	if p.Appearance.CodeIndent == "" {
		p.Appearance.CodeIndent = def.Appearance.CodeIndent
	}
	// 新字段仅缺键时补默认；显式 0 仍交给 Validate 拒绝。
	root := value.(map[string]any)
	for _, field := range []struct {
		section, key string
		target       *int
		fallback     int
	}{
		{"storage", "saveIntervalMs", &p.Storage.SaveIntervalMS, def.Storage.SaveIntervalMS},
		{"git", "commitMinutes", &p.Git.CommitMinutes, def.Git.CommitMinutes},
		{"git", "pushMinutes", &p.Git.PushMinutes, def.Git.PushMinutes},
	} {
		fields, _ := root[field.section].(map[string]any)
		if _, exists := fields[field.key]; !exists {
			*field.target = field.fallback
		}
	}
	if hasLegacyWindow {
		// 版本 11 之前的像素尺寸：缺的一边按旧默认 400 × 540 补齐后换算。
		legacyW, legacyH = cmp.Or(legacyW, 400), cmp.Or(legacyH, 540)
		p.Advanced.WindowColumns, p.Advanced.WindowRows = WindowGrid(legacyW, legacyH, legacyWindowMetrics(p.Appearance))
	}
	if p.Advanced.WindowColumns == 0 {
		p.Advanced.WindowColumns = def.Advanced.WindowColumns
	}
	if p.Advanced.WindowRows == 0 {
		p.Advanced.WindowRows = def.Advanced.WindowRows
	}
	if err := p.Validate(v); err != nil {
		return Preferences{}, err
	}
	return p, nil
}

// migrate 把旧版本偏好升级到 CurrentVersion。版本 1 只有一个正文字体键，
// 西文当时取自同一套字体栈，因此升级后 latinFont 留空（跟随中文字体），
// 渲染结果与升级前完全一致。版本 4 把更名前的主题 ID（"tot"）映射到新 ID
// （"classic" / "note"），主题取值不变、只换名字。版本 5 给从未设置过代码
// 配色的旧文件补默认方案 soft；带值文件原样保留。
// 版本 7 给从未写过 codeIndent 的旧文件补默认 2 空格。
// 版本 8 仅扩展字体键取值，保留字体值与此前迁移行为。
// 版本 10 把颜色「手绘」+ 排版「手账」的整套组合迁为手账风格，颜色与排版回到
// 标准默认；只有一项是手绘/手账的半套组合迁为标准风格，那一项回到默认。
func migrate(p Preferences) Preferences {
	if p.Version < 2 {
		p.Appearance.LatinFont = ""
	}
	if p.Version < 4 {
		if p.Appearance.ColorTheme == legacyThemeTot {
			p.Appearance.ColorTheme = "classic"
		}
		if p.Appearance.MarkdownTheme == legacyThemeTot {
			p.Appearance.MarkdownTheme = "note"
		}
	}
	if p.Version < 5 && p.Appearance.CodeTheme == "" {
		// 版本 4 及更早的写入方从未写过 codeTheme 键，空值即「从未设置」，
		// 补默认方案；带值的旧文件（含显式 soft/vivid）原样保留。
		p.Appearance.CodeTheme = "soft"
	}
	if p.Version < 7 && p.Appearance.CodeIndent == "" {
		// 版本 6 及更早的写入方从未写过 codeIndent 键，升级时补默认 2 空格。
		p.Appearance.CodeIndent = "space2"
	}
	if p.Version < 10 {
		def := Default().Appearance
		p.Appearance.Style = StyleStandard
		if p.Appearance.ColorTheme == StyleSketch && p.Appearance.MarkdownTheme == StyleSketch {
			p.Appearance.Style = StyleSketch
		}
		if p.Appearance.ColorTheme == StyleSketch {
			p.Appearance.ColorTheme = def.ColorTheme
		}
		if p.Appearance.MarkdownTheme == StyleSketch {
			p.Appearance.MarkdownTheme = def.MarkdownTheme
		}
	}
	p.Version = CurrentVersion
	return p
}

// EffectiveColorTheme 实际生效的颜色主题：手账风格固定为手绘点阵纸。
func (a Appearance) EffectiveColorTheme() string {
	if a.Style == StyleSketch {
		return "sketch"
	}
	return a.ColorTheme
}

// EffectiveMarkdownTheme 实际生效的排版主题：手账风格固定为手账排版。
func (a Appearance) EffectiveMarkdownTheme() string {
	if a.Style == StyleSketch {
		return "sketch"
	}
	return a.MarkdownTheme
}

// legacyThemeTot 是主题 ID 更名前旧文件里的写法；仅用于识别旧值并映射新 ID。
const legacyThemeTot = "tot"

func rejectJSONNull(value any, path string) error {
	if value == nil {
		return fmt.Errorf("%s 不允许 null", path)
	}
	if fields, ok := value.(map[string]any); ok {
		for key, child := range fields {
			if err := rejectJSONNull(child, path+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}
