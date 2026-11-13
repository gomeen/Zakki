package app

// 所有标签集中存入一个笔记文件，路径和写入协调由 notes.Store 管理。

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"zakki/internal/notes"
	"zakki/locales"
)

const appName = "Zakki"

// snapshotFile 在主线程把当前状态拍成磁盘格式（后台只写该快照）。
func (a *app) snapshotFile() savedFile {
	tabs := make([]savedTab, len(a.tabs))
	for i, tb := range a.tabs {
		tabs[i] = savedTab{ID: tb.ID, Name: tb.Name, Text: tb.Text, Color: tb.Color}
	}
	return savedFile{
		Version: currentDataVersion,
		Active:  a.active,
		Tabs:    tabs,
		Settings: settings{
			ThemeSource:   a.cfg.ThemeSource,
			MarkdownTheme: a.cfg.MarkdownTheme,
			BodyFont:      a.cfg.BodyFont,
			MonoFont:      a.cfg.MonoFont,
			BodySize:      a.cfg.BodySize,
			LineHeight:    a.cfg.LineHeight,
			AlwaysOnTop:   a.cfg.AlwaysOnTop,
			HideOnEscape:  a.cfg.HideOnEscape,
			TabTitleWidth: a.cfg.TabTitleWidth,
			TabAnimSpeed:  a.cfg.TabAnimSpeed,
			MonoDots:      a.cfg.MonoDots,
			GlassStrength: a.cfg.GlassStrength,
			TabMotionOff:  a.cfg.TabMotionOff,
			TabDotShape:   a.cfg.TabDotShape,
		},
	}
}

// newAppAt 用显式目录装配 Store，不探测系统目录，也不触发迁移。
// 测试必须使用这个接缝，避免接触真实用户数据。
func newAppAt(dir string) *app {
	s := settings{ThemeSource: "system", HideOnEscape: true}
	now := time.Now
	a := &app{
		cfg:      s,
		prefs:    defaultPreferences(),
		tabs:     defaultTabs(),
		mdActive: -1, // 启动即全预览态：光标不会让任何一行变成源码
		now:      now,
		notes: newNotesStore(
			filepath.Join(dir, dataFileName),
			now,
		),
		prefsStore: newPrefsStore(filepath.Join(dir, "preferences.json")),
	}
	a.syncNextTabID()
	a.resetSelectionSync()
	a.resetCodeLang()
	return a
}

// resetSelectionSync 把选区同步哨兵复位为「不强制」。
// selSyncStart 等哨兵用 -1 表示不强制；零值 0 会误触强制选区，
// 因此 newApp 显式初始化。
func (a *app) resetSelectionSync() {
	a.selSyncStart, a.selSyncEnd = -1, -1
	a.srcSelSyncStart, a.srcSelSyncEnd = -1, -1
}

// resetCodeLang 把语言徽标会话复位为「无」。codeLangBlock 用 -1 表示无会话；
// 零值 0 是合法的块首行号，会误触提交，因此 newApp 显式初始化。
func (a *app) resetCodeLang() {
	a.codeLangBlock, a.codeLangSeenFocus, a.codeLangSynced, a.codeLangFocus = -1, false, -1, 0
}

// currentDataPath 始终返回唯一笔记文件。
func (a *app) currentDataPath() string { return a.notes.DataPath() }

// load 只读取当前文件；异常时保留原件并暂停写入，不创建替代副本。
// 成功读取（含空文件）解除因载入失败造成的写保护。
func (a *app) load() error {
	f, exists, hadTabs, err := a.notes.Load()
	if err != nil {
		a.notes.SetSaveBlocked(true)
		a.notice = locales.StorageReadFailed(err.Error())
		return err
	}
	if !exists {
		a.initDefaults()
		a.notes.SetSaveBlocked(false)
		a.rescheduleReminders()
		return nil
	}
	a.applySavedFile(f)
	if !hadTabs {
		a.initDefaults() // 空文件：首次运行的欢迎文本
	}
	a.resyncLive(0)
	a.notes.SetSaveBlocked(false)
	a.notice = ""
	a.rescheduleReminders()
	return nil
}

// reloadNotes 是用户显式要求重新读取磁盘文件的入口；失败维持写保护，
// 除 toast 外 load 里的警示也留在状态栏，直到重读成功。
func (a *app) reloadNotes() {
	if err := a.load(); err != nil {
		a.showToast(locales.StorageReadFailed(err.Error()))
	}
	a.refreshSettingsWindow()
}

// applySavedFile 把迁移后的磁盘数据放进内存状态（主线程）。
func (a *app) applySavedFile(f savedFile) {
	a.tabs = a.tabs[:0]
	for _, st := range f.Tabs {
		a.tabs = append(a.tabs, tab{ID: st.ID, Name: st.Name, Text: st.Text, Color: st.Color})
	}
	a.active = f.Active
	a.clampActive()
	a.syncNextTabID()
	a.cfg.ThemeSource = orDefault(f.Settings.ThemeSource, "system")
	a.cfg.MarkdownTheme = f.Settings.MarkdownTheme
	a.cfg.BodyFont, a.cfg.MonoFont = f.Settings.BodyFont, f.Settings.MonoFont
	a.cfg.BodySize, a.cfg.LineHeight = f.Settings.BodySize, f.Settings.LineHeight
	a.cfg.AlwaysOnTop = f.Settings.AlwaysOnTop
	a.cfg.HideOnEscape = f.Settings.HideOnEscape
	a.cfg.TabTitleWidth, a.cfg.TabAnimSpeed = f.Settings.TabTitleWidth, f.Settings.TabAnimSpeed
	a.cfg.MonoDots, a.cfg.GlassStrength = f.Settings.MonoDots, f.Settings.GlassStrength
	a.cfg.TabMotionOff = f.Settings.TabMotionOff
	// 未知形状值回退默认圆点，避免旧/外来文件让顶栏画不出形状。
	if !isValidTabDotShape(f.Settings.TabDotShape) {
		f.Settings.TabDotShape = ""
	}
	a.cfg.TabDotShape = f.Settings.TabDotShape
	a.rescheduleReminders()
}

// welcomeTextFor 按初始标签数生成标签 1 的欢迎文本；提示段落来自 catalog。
// 快捷键以 macOS 符号书写，非 macOS 平台展示前转成 Ctrl/Alt/Shift。
func welcomeTextFor(tabCount int) string {
	if tabCount < 1 {
		tabCount = 1
	}
	return locales.WelcomeTitle() + "\n\n" + locales.WelcomeIntro(fmt.Sprint(tabCount)) + "\n\n" +
		shortcutText(locales.WelcomeBodyTips()) + "\n\n" + locales.WelcomeBodyFooter()
}

func timeGuideText() string { return shortcutText(locales.TimeGuideBody()) }

// seedInitialContent 只由初始化入口调用，不为已有笔记补写说明。
// 复用第二个标签；只选一个标签时合并到欢迎文本，保持用户选择的数量。
func (a *app) seedInitialContent() {
	if len(a.tabs) == 0 {
		return
	}
	a.tabs[0].Text = welcomeTextFor(len(a.tabs))
	if len(a.tabs) == 1 {
		a.tabs[0].Text += "\n\n" + timeGuideText()
		return
	}
	a.tabs[1].Name = locales.TimeGuideTabName()
	a.tabs[1].Text = timeGuideText()
}

// welcomeText 默认标签数下的欢迎文案（测试与固定引用）。
var welcomeText = welcomeTextFor(DefaultTabCount)

// commitFirstRunData 写入首启数据快照。tabCount>0 时按数量重建标签；为 0 时
// 保留当前标签数并刷新欢迎文案。非首启时静默成功。
func (a *app) commitFirstRunData(tabCount int) error {
	if !a.firstRun() {
		return nil
	}
	if tabCount > 0 {
		if tabCount < 1 {
			tabCount = 1
		}
		if tabCount > MaxTabCount {
			tabCount = MaxTabCount
		}
		a.rebuildInitialTabs(tabCount)
	} else if len(a.tabs) > 0 {
		a.seedInitialContent()
		a.resyncLive(utf8.RuneCountInString(a.tabs[0].Text))
	}
	err := a.notes.SaveNow(a.snapshotFile())
	if err != nil {
		slog.Warn("首启数据写入失败", "error", err)
	}
	return err
}

// persistFirstRunSnapshot 跳过/关窗路径落盘（保持当前默认标签数）。
func (a *app) persistFirstRunSnapshot() error {
	return a.commitFirstRunData(0)
}

// initDefaults 首次运行/空文件：生成欢迎与时间说明。
// 不改 active（空文件里 active 跟随磁盘数据）。
func (a *app) initDefaults() {
	if len(a.tabs) == 0 {
		a.tabs = defaultTabs()
		a.syncNextTabID()
	}
	a.seedInitialContent()
	a.clampActive()
	a.resyncLive(utf8.RuneCountInString(a.tabs[0].Text))
}

// saveNow 把当前状态快照同步写入磁盘（原子替换）。只保存数据，不做备份。
func (a *app) saveNow() error {
	return a.notes.SaveNow(a.snapshotFile())
}

// scheduleSave 替换待写快照，但不推迟首笔修改的保存期限。
func (a *app) scheduleSave() {
	a.notes.ScheduleSave(a.snapshotFile(), a.saveInterval(), a.storageRefresh())
}

// saveCurrent 是菜单保存入口，原生窗口中不等待磁盘 I/O。
func (a *app) saveCurrent() {
	snapshot, store, refresh := a.snapshotFile(), a.notes, a.storageRefresh()
	save := func() {
		if err := store.SaveNow(snapshot); errors.Is(err, notes.ErrStopped) {
			// 最终保存后到达的菜单请求不再写盘，也不阻止退出。
			return
		}
		refresh()
	}
	if a.ready {
		go save()
	} else {
		save()
	}
}

// storageRefresh 只捕获窗口引用，后台不读取 app 状态。
func (a *app) storageRefresh() func() {
	main, settings := a.win, a.settingsWin
	return func() {
		if main != nil {
			main.Update(func() {})
		}
		if settings != nil {
			settings.Update(func() {})
		}
	}
}

// shutdown 退出流程（OnWillQuit，主线程）：取消防抖定时器，做一次最终快照
// 保存并等它完成；此后不再接受任何保存请求，也不会与定时保存并发写两份。
func (a *app) shutdown() {
	if err := a.shutdownForQuit(); err != nil {
		slog.Error("最终保存失败", "error", err)
	}
}

func (a *app) shutdownForQuit() (err error) {
	defer func() {
		if err != nil {
			a.resumeReminders()
		}
	}()
	// 在途提醒定时器随退出全部作废；错过的提醒不补发（ADR-0001）。
	a.stopReminders()
	if a.storageMoving {
		return errors.New(locales.StorageMoving())
	}
	// 写保护期间无法写笔记文件：保留原件并放行退出，不能把用户困在应用里。
	if a.notes.SaveBlocked() {
		slog.Warn("笔记处于写保护，退出不写笔记文件，原件保持不变")
		a.notes.SetStopped(true)
		if err := a.shutdownPreferences(); err != nil {
			a.notes.SetStopped(false)
			return err
		}
		a.notes.Close()
		return nil
	}
	if err := a.notes.Shutdown(a.snapshotFile()); err != nil {
		a.notes.SetStopped(false)
		return err
	}
	if err := a.shutdownPreferences(); err != nil {
		a.notes.SetStopped(false)
		return err
	}
	// 最终保存已成功：尽力把本机修改推到云端，换设备时另一端立即拿得到。
	// 失败不阻止退出，下次启动会自动补推。
	if a.syncEnabledPref() {
		if err := a.notes.FinalSync(finalSyncTimeout); err != nil {
			slog.Warn("退出前推送未完成，下次启动自动补推", "error", err)
		}
	}
	a.notes.Close()
	slog.Info("退出前已最终保存")
	return nil
}

// finalSyncTimeout 是退出时最多等待推送的时间；超时照常退出。
const finalSyncTimeout = 3 * time.Second

// clearActiveTab 清空当前标签内容；与普通编辑一致走自动保存。
func (a *app) clearActiveTab() {
	a.activeTab().Text = ""
	a.resyncLive(0)
	a.liveFocus = 1 // 焦点回到编辑器（点击按钮会把焦点带走）
	a.onEdited()
}

// reportPersist 上报持久化结果：失败记 ERROR 日志并让状态栏显示，成功清错误。
func (a *app) reportPersist(op string, err error) { a.notes.ReportPersist(op, err) }

// saveError 最近一次持久化错误（空串表示没有）。
func (a *app) saveError() string { return a.notes.LastSaveError() }

// ---- 小工具 ----

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// findMatches 找出 query 在 text 中的全部出现位置（大小写不敏感），
// 返回按 rune 计的区间。
func findMatches(text, query string) []ui.TextRange {
	if query == "" {
		return nil
	}
	return matchLower(strings.ToLower(text), strings.ToLower(query))
}

// matchLower 在已小写的文本里找全部出现位置，O(n)：
// rune 偏移随扫描递增，不对每个命中重扫前缀。
func matchLower(lt, lq string) []ui.TextRange {
	if lq == "" || len(lq) > len(lt) {
		return nil
	}
	var out []ui.TextRange
	runes, off := 0, 0
	for {
		idx := strings.Index(lt[off:], lq)
		if idx < 0 {
			break
		}
		b := off + idx
		runes += utf8.RuneCountInString(lt[off:b])
		out = append(out, ui.TextRange{Start: runes, End: runes + utf8.RuneCountInString(lq)})
		runes += utf8.RuneCountInString(lq)
		off = b + len(lq)
	}
	return out
}

// loweredDoc 返回 doc 的小写副本（缓存；全文 ToLower 每帧做太浪费）。
func (a *app) loweredDoc(doc string) string {
	if doc != a.lowerDoc {
		a.lowerDoc, a.lowerCache = doc, strings.ToLower(doc)
	}
	return a.lowerCache
}

// docMatches 与 findMatches 相同，但复用当前点文本的小写缓存
// （查找条打开时每帧都会调用）。
func (a *app) docMatches(doc, query string) []ui.TextRange {
	if query == "" {
		return nil
	}
	// 查找条打开时每帧都会调用，按文档串与查询串缓存全文扫描结果。
	if a.matchesCacheDoc == doc && a.matchesCacheQuery == query {
		return a.matchesCache
	}
	ms := matchLower(a.loweredDoc(doc), strings.ToLower(query))
	a.matchesCacheDoc, a.matchesCacheQuery, a.matchesCache = doc, query, ms
	return ms
}
