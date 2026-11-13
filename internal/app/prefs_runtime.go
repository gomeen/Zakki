package app

// 用户偏好的单一来源：数据目录中的 preferences.json。
//
// 设计要点（对应“单一设置来源与迁移”）：
//   - 偏好只由 preferences.json 管理；笔记文件（zakki.json）保留笔记内容、
//     活动点与编辑会话状态，旧 settings 字段仅在首次迁移时读取一次。
//   - 一次迁移：默认值 → 笔记内旧设置 → 旧配置（config.toml 优先，仅当
//     TOML 不存在时读 config.json）显式值。迁移成功以 preferences.json
//     原子写入为完成标志；旧文件原样保留，之后不再读取。
//   - 磁盘写由 internal/prefs.Store 串行化；后台只接触不可变快照。

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"zakki/internal/notes"
	"zakki/internal/prefs"
	"zakki/locales"
)

const currentPreferencesVersion = prefs.CurrentVersion

// ---- app 集成：载入、保存与统一变更入口 ----

func (a *app) preferencesPath() string { return a.prefsStore.Path }

// loadPreferences 启动时执行：已有 preferences.json 则严格读取；
// 没有则执行一次迁移。失败不静默：损坏/超前版本进入“待恢复”状态，
// 旧配置损坏进入“待确认”状态，都由设置页提供入口。
func (a *app) loadPreferences() error {
	path := a.preferencesPath()
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		a.prefs = defaultPreferences()
		if err := a.prefsStore.WriteInitial(a.prefs); err != nil {
			a.setPrefsSaveError(err)
			return err
		}
		return nil
	case err != nil:
		readErr := fmt.Errorf("%s: %w", locales.PrefsReadErr(path), err)
		a.prefs = defaultPreferences()
		a.applyPreferencesToRuntime()
		a.setPrefsIssue(prefsIssue{
			Kind:    prefsIssuePrefsBroken,
			Message: locales.PrefsBrokenMessage(readErr.Error()),
		})
		a.setPrefsSaveError(errors.New(locales.PrefsSaveBlockedPending()))
		return readErr
	}
	prefs, err := decodePreferences(raw)
	if err != nil {
		if strings.Contains(err.Error(), "json: unknown field") {
			err = fmt.Errorf("%w（%s）", err, locales.PrefsVersionAheadHint())
		}
		a.prefs = defaultPreferences()
		a.applyPreferencesToRuntime()
		a.setPrefsIssue(prefsIssue{
			Kind:    prefsIssuePrefsBroken,
			Message: locales.PrefsBrokenMessage(err.Error()),
		})
		a.setPrefsSaveError(errors.New(locales.PrefsSaveBlockedPending()))
		return err
	}
	a.prefs = prefs
	a.applyPreferencesToRuntime()
	return nil
}

func (a *app) setPrefsIssue(issue prefsIssue) { a.prefsStore.SetIssue(issue) }

func (a *app) currentPrefsIssue() prefsIssue { return a.prefsStore.Issue() }

// savePreferencesNow 把当前偏好同步写盘（原子替换）。
func (a *app) savePreferencesNow() error {
	return a.prefsStore.SaveNow(a.prefs)
}

// schedulePreferencesSave 防抖自动保存偏好。
func (a *app) schedulePreferencesSave() {
	snap := a.prefsStore.TakeSnapshot(a.prefs)
	a.prefsStore.ScheduleDebounced(snap)
}

// setPrefsSaveError 记录最近一次偏好保存错误（设置页显示「未保存」）。
func (a *app) setPrefsSaveError(err error) { a.prefsStore.SetSaveError(err) }

// prefsSaveError 最近一次偏好保存错误（空串表示已保存）。
func (a *app) prefsSaveError() string { return a.prefsStore.LastSaveError() }

// retryPreferencesSave 设置页的「重试」入口。
func (a *app) retryPreferencesSave() {
	if err := a.savePreferencesNow(); err == nil {
		// 成功后由设置页刷新状态。
	}
}

// confirmRecoverPreferences 用户确认后以默认设置（含本次会话修改）覆盖损坏的
// preferences.json，不另存原件：数据目录不留副本，按钮文案须写明会丢弃原件。
func (a *app) confirmRecoverPreferences() error {
	if a.currentPrefsIssue().Kind != prefsIssuePrefsBroken {
		return nil
	}
	// 先在异常门仍关闭时完成确认写入；失败则保留原 issue，shutdown 与
	// 自动保存继续被拦截。只有成功后才开放正常保存。
	if err := a.prefsStore.WriteInitial(a.prefs); err != nil {
		a.setPrefsSaveError(err)
		return err
	}
	a.setPrefsIssue(prefsIssue{})
	a.prefsStore.BumpRev()
	if err := a.openConfiguredNotes(); err != nil {
		a.notes.SetSaveBlocked(true)
		return err
	}
	// 笔记载入会恢复旧版内嵌外观；偏好仍是运行时设置的权威来源。
	a.applyPreferencesToRuntime()
	return nil
}

// shutdownPreferences 退出流程：等待最终偏好保存并报告失败。
func (a *app) shutdownPreferences() error {
	if err := a.prefsStore.Shutdown(a.prefs); err != nil {
		slog.Error("退出前偏好最终保存失败", "error", err)
		return err
	}
	return nil
}

// updatePreferences 统一变更入口：校验 → 应用运行时 → 同步菜单/窗口/热键 → 保存。
func (a *app) updatePreferences(mutate func(p *preferences)) error {
	if a.storageMoving {
		return errors.New(locales.StorageMoving())
	}
	next := clonePrefs(a.prefs)
	mutate(&next)
	if err := validatePrefs(next); err != nil {
		return err
	}
	a.prefs = next
	a.settingsUI.mergeDraftFromPrefs(next)
	a.applyPreferencesToRuntime()
	a.syncMenus()
	a.refreshSettingsWindow()
	hotkeyChanged := a.syncGlobalHotkey()
	_ = hotkeyChanged
	if a.win != nil && a.winOpen {
		a.win.Update(func() {})
	}
	a.schedulePreferencesSave()
	return nil
}

// applyPreferencesToRuntime 把偏好同步进运行时视图状态（主线程）。
func (a *app) applyPreferencesToRuntime() {
	p := a.prefs
	a.cfg.ThemeSource = p.Appearance.AppearanceMode
	a.cfg.MarkdownTheme = p.Appearance.EffectiveMarkdownTheme()
	a.cfg.BodyFont, a.cfg.MonoFont = p.Appearance.BodyFont, p.Appearance.MonoFont
	a.cfg.BodySize, a.cfg.LineHeight = p.Appearance.BodySize, p.Appearance.LineHeight
	a.cfg.AlwaysOnTop = p.Window.AlwaysOnTop
	a.cfg.HideOnEscape = p.Window.HideOnEscape
	a.configureGit()
	a.configureSync()
	if a.ready {
		applyTheme(p.Appearance.AppearanceMode)
	}
	// 语言变更经 MyGo 即时生效并持久化；OnLocaleChanged 里重建菜单。
	if a.ready && p.Appearance.Language != a.appliedLocale {
		mygo.App.SetLocale(p.Appearance.Language)
		a.appliedLocale = p.Appearance.Language
	}
	a.applyAlwaysOnTop()
	a.applyMinimumWindowSize()
}

func (a *app) saveInterval() time.Duration {
	return time.Duration(a.prefs.Storage.SaveIntervalMS) * time.Millisecond
}

func (a *app) configureGit() {
	p := a.prefs.Git
	a.notes.ConfigureGit(notes.GitConfig{
		AutoCommit: p.AutoCommit, AutoPush: p.AutoPush,
		CommitInterval: time.Duration(p.CommitMinutes) * time.Minute,
		PushInterval:   time.Duration(p.PushMinutes) * time.Minute,
	}, a.storageRefresh())
}
