package app

// 设置「保存与同步」页的云同步分区：开关、仓库、连接（Device Flow）、
// 状态行与暂停说明。双方都改时自动合并，没有冲突入口（ADR-0004）。文案一律经 locales.*；开关写入
// preferences.json 的 sync 节，用户原有的 Git 开关不被改写（暂停只在
// 运行期生效，见 notes.Store 的 syncActive 门禁）。

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"zakki/locales"
)

// syncSection 构建云同步分区；位于「保存与同步」页 Git 区块之后。
func (a *app) syncSection(c *ui.Context) {
	sectionDivider(c)
	settingsSectionTitle(c, locales.SyncSection())
	settingsRow(c, locales.SyncEnable(), locales.SyncEnableHint(), func() {
		if ui.Switch(c.Key("sync-enabled"), &a.settingsUI.draft.Sync.Enabled).Label(locales.SyncEnable()).Changed() {
			on := a.settingsUI.draft.Sync.Enabled
			_ = a.updatePreferences(func(p *preferences) {
				p.Sync.Enabled = on
				if on {
					// 开关只负责启用；后端与仓库沿用已保存/正在编辑的值。
					if p.Sync.Backend == "" {
						p.Sync.Backend = syncBackendGitHub
					}
					if p.Sync.Repo == "" {
						p.Sync.Repo = normalizeSyncRepo(a.settingsUI.syncRepoText)
					}
				}
			})
		}
	})
	if a.prefs.Sync.Enabled && (a.prefs.Git.AutoCommit || a.prefs.Git.AutoPush) {
		ui.Column(c).FillWidth().Padding(6, 10, 6, 10).Background(c.Theme().Surface).Gap(2).Children(func() {
			ui.Text(c, locales.SyncPausedGitHint()).FontSize(12).TextColor(tMuted(c))
		})
	}
	// 状态行：当前后端、最近同步时间、运行中与连接状态。
	st := a.notes.SyncStateSnapshot()
	var stateText string
	switch {
	case !a.prefs.Sync.Enabled:
		stateText = locales.SyncOffStatus()
	case st.Running:
		stateText = locales.SyncRunningStatus()
	default:
		stateText = a.syncStatusLine()
	}
	ui.Text(c, stateText).FontSize(12).TextColor(tMuted(c))
	if st.Error != "" {
		ui.Text(c, st.Error).FontSize(12).TextColor(c.Theme().Danger)
	}
	settingsRow(c, locales.SyncRepo(), locales.SyncRepoHint(), func() {
		if ui.TextInput(c.Key("sync-repo"), &a.settingsUI.syncRepoText).Width(220).Placeholder("owner/name").Submitted() {
			_ = a.updatePreferences(func(p *preferences) { p.Sync.Repo = normalizeSyncRepo(a.settingsUI.syncRepoText) })
		}
	})
	ui.Row(c).Gap(8).PaddingY(12).Children(func() {
		connected := a.syncTokenConnected()
		if connected {
			if ui.Button(c, locales.SyncDisconnect()).Clicked() {
				a.disconnectGitHubSync()
			}
		} else if ui.Button(c, locales.SyncConnect()).Disabled(a.syncState.connecting || a.githubClientID() == "").Clicked() {
			a.connectGitHubSync()
		}
		if a.syncState.connecting {
			ui.Text(c, locales.SyncConnectWaiting()).FontSize(12).TextColor(tMuted(c))
		}
		if a.prefs.Sync.Enabled {
			if ui.Button(c, locales.SyncNow()).Disabled(st.Running).Clicked() {
				a.notes.SyncNow()
			}
		}
	})
}

// normalizeSyncRepo 清理仓库输入：去空白；空串原样返回（交由偏好校验）。
func normalizeSyncRepo(raw string) string { return strings.TrimSpace(raw) }
