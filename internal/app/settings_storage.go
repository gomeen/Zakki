package app

import (
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zakki/internal/notes"
	"zakki/locales"
)

// probeNotesGitRepo 报告笔记文件所在目录是否位于 git 工作区内（向上找 .git），
// 结果按目录缓存，打开窗口或更换位置时失效。
func (a *app) probeNotesGitRepo() bool {
	dir := filepath.Dir(a.notes.DataPath())
	if a.settingsUI.gitProbeDir != dir {
		a.settingsUI.gitProbeDir = dir
		a.settingsUI.gitProbeRepo = notes.DirIsGitRepo(dir)
	}
	return a.settingsUI.gitProbeRepo
}

// storagePage 展示唯一笔记文件与保存状态；Git 区块仅在笔记位于工作区内时出现。
func (a *app) storagePage(c *ui.Context) {
	ui.Scroll(c.Key("storage-controls")).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(0, 22, 0, 0).Children(func() {
			a.persistStatus(c)
			settingsSectionTitle(c, locales.StorageFile())
			settingsLead(c, locales.StorageFileHint())
			ui.Text(c, a.notes.DataPath()).FontSize(12).Wrap()
			ui.Row(c).Gap(8).PaddingY(12).Children(func() {
				if ui.Button(c, locales.StorageShowFile()).Clicked() {
					mygo.Shell.ShowItemInFolder(a.notes.DataPath())
				}
				if ui.Button(c, locales.StorageChangeLocation()).Disabled(a.storageMoving).Clicked() {
					a.pickStorageFolder()
				}
			})
			settingsRow(c, locales.StorageInterval(), "", func() {
				seconds := float64(a.settingsUI.draft.Storage.SaveIntervalMS) / 1000
				if ui.NumberInput(c.Key("save-interval"), &seconds, 0.1, 60, 0.5).Width(150).Label(locales.StorageInterval()).Changed() {
					_ = a.updatePreferences(func(p *preferences) { p.Storage.SaveIntervalMS = int(seconds * 1000) })
				}
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.storageStatus()).FontSize(12).TextColor(tMuted(c)).Grow(1)
				if ui.Button(c, locales.MenuSave()).Clicked() {
					a.saveCurrent()
				}
			})
			if a.probeNotesGitRepo() {
				sectionDivider(c)
				settingsSectionTitle(c, locales.StorageGit())
				status := a.notes.GitStatus()
				if status.Missing {
					settingsLead(c, locales.GitToolsMissing())
				} else {
					ui.Text(c, status.RepoRoot).FontSize(12).TextColor(tMuted(c))
				}
				settingsRow(c, locales.StorageAutoCommit(), "", func() {
					if ui.Switch(c.Key("auto-commit"), &a.settingsUI.draft.Git.AutoCommit).Label(locales.StorageAutoCommit()).Changed() {
						enabled := a.settingsUI.draft.Git.AutoCommit
						_ = a.updatePreferences(func(p *preferences) {
							p.Git.AutoCommit = enabled
							if !enabled {
								p.Git.AutoPush = false
							}
						})
					}
				})
				if a.prefs.Git.AutoCommit {
					settingsRow(c, locales.StorageCommitInterval(), "", func() {
						minutes := float64(a.prefs.Git.CommitMinutes)
						if ui.NumberInput(c.Key("commit-minutes"), &minutes, 1, 1440, 1).Width(150).Label(locales.StorageCommitInterval()).Changed() {
							_ = a.updatePreferences(func(p *preferences) { p.Git.CommitMinutes = int(minutes) })
						}
					})
					settingsRow(c, locales.StorageAutoPush(), locales.StoragePushHint(), func() {
						if ui.Switch(c.Key("auto-push"), &a.settingsUI.draft.Git.AutoPush).Label(locales.StorageAutoPush()).Changed() {
							enabled := a.settingsUI.draft.Git.AutoPush
							_ = a.updatePreferences(func(p *preferences) { p.Git.AutoPush = enabled })
						}
					})
					if a.prefs.Git.AutoPush {
						settingsRow(c, locales.StoragePushInterval(), "", func() {
							minutes := float64(a.prefs.Git.PushMinutes)
							if ui.NumberInput(c.Key("push-minutes"), &minutes, 1, 1440, 1).Width(150).Label(locales.StoragePushInterval()).Changed() {
								_ = a.updatePreferences(func(p *preferences) { p.Git.PushMinutes = int(minutes) })
							}
						})
					}
				}
				for _, event := range []struct {
					label string
					at    time.Time
				}{
					{locales.StorageLastCommit(), status.CommitTime}, {locales.StorageLastPush(), status.PushTime},
				} {
					if !event.at.IsZero() {
						ui.Text(c, event.label+" "+event.at.Format("01-02 15:04")).FontSize(11).TextColor(tMuted(c))
					}
				}
				if status.Error != "" {
					ui.Text(c, status.Error).FontSize(12).TextColor(c.Theme().Danger)
				}
			}
			a.syncSection(c)

		})
	})
}

func (a *app) persistStatus(c *ui.Context) {
	if msg := a.prefsSaveError(); msg != "" {
		ui.Column(c).FillWidth().Gap(6).PaddingY(8).Children(func() {
			ui.Text(c, msg).TextColor(c.Theme().Danger)
			if ui.Button(c, locales.RetrySave()).Clicked() {
				a.retryPreferencesSave()
			}
		})
	}
	if msg := a.storageFailure(); msg != "" {
		ui.Column(c).FillWidth().Gap(6).PaddingY(8).Children(func() {
			ui.Text(c, msg).TextColor(c.Theme().Danger)
			if a.notes.SaveBlocked() {
				if ui.Button(c, locales.StorageReload()).Clicked() {
					a.reloadNotes()
				}
			} else if ui.Button(c, locales.RetrySave()).Clicked() {
				a.saveCurrent()
			}
		})
	}
	if a.storageMoving {
		ui.Text(c, locales.StorageMoving()).TextColor(tMuted(c))
	}
}
