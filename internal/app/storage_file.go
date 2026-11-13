package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"zakki/locales"
)

// canonicalPath 跟随目录符号链接，避免同一位置被当作两个保存目标。
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(abs))
}

func (a *app) validateStorageDir(path string) (string, error) { return validateStorageDir(path) }

func validateStorageDir(path string) (string, error) {
	abs, err := canonicalPath(path)
	if err != nil {
		return "", errors.New(locales.StorageInvalidFolder())
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", errors.New(locales.StorageInvalidFolder())
	}
	probe, err := os.CreateTemp(abs, writeProbeTempPrefix+"*")
	if err != nil {
		return "", errors.New(locales.StorageNotWritable())
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil || removeErr != nil {
		return "", errors.New(locales.StorageNotWritable())
	}
	return abs, nil
}

// openConfiguredNotes 必须在载入偏好之后执行；指定文件丢失不能当作首启重建。
func (a *app) openConfiguredNotes() error {
	if path := a.prefs.Storage.DataFile; path != "" && path != a.notes.DataPath() {
		a.notes.Close()
		a.notes = newNotesStore(path, a.now)
	}
	if a.prefs.Storage.DataFile != "" {
		if _, err := os.Stat(a.notes.DataPath()); err != nil {
			a.notes.SetSaveBlocked(true)
			a.notice = locales.StorageReadFailed(err.Error())
			return err
		}
	}
	return a.load()
}

func (a *app) chooseStorageDirAsync(notify *mygo.Window, apply func(string)) {
	if !a.ready {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: notify, Title: locales.StorageChooseTitle(), Message: locales.StorageChooseHint(),
			ButtonLabel: locales.CommonChoose(), Directory: true, CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 || paths[0] == "" {
			return
		}
		path := paths[0]
		if notify != nil {
			notify.Update(func() { apply(path) })
		}
	}()
}

func (a *app) pickStorageFolder() {
	a.chooseStorageDirAsync(a.settingsWin, a.applyStorageDir)
}

// applyStorageDir 在串行持久化成功后才发布新位置；期间不允许其他偏好覆盖路径。
func (a *app) applyStorageDir(path string) {
	if a.storageMoving {
		return
	}
	snapshot, next := a.snapshotFile(), clonePrefs(a.prefs)
	store, prefStore := a.notes, a.prefsStore
	notify := a.settingsWin
	if notify == nil {
		notify = a.win
	}
	a.storageMoving, a.storageError = true, ""
	a.refreshSettingsWindow()
	work := func() (switched bool, err error) {
		dir, err := validateStorageDir(path)
		if err != nil {
			return false, err
		}
		next.Storage.DataFile = filepath.Join(dir, dataFileName)
		if err := validatePrefs(next); err != nil {
			return false, err
		}
		if next.Storage.DataFile == store.DataPath() {
			return false, nil // 位置未变：保持文件与偏好原样
		}
		result, err := store.Relocate(next.Storage.DataFile, snapshot, func() error { return prefStore.SaveNow(next) })
		return result.Committed, err
	}
	complete := func(switched bool, err error) {
		a.finishStorageMove(next, switched, err)
	}
	if !a.ready {
		sw, err := work()
		complete(sw, err)
		return
	}
	go func() {
		sw, err := work()
		if notify != nil {
			notify.Update(func() { complete(sw, err) })
		}
	}()
}

func (a *app) finishStorageMove(next preferences, switched bool, err error) {
	a.storageMoving = false
	if switched {
		a.prefs = next
		a.settingsUI.syncDraftFromPrefs(next)
		a.configureGit()
	}
	if err != nil {
		a.storageError = locales.StorageMoveFailed(err.Error())
	}
	a.refreshSettingsWindow()
	if a.win != nil {
		a.win.Update(func() {})
	}
}

func (a *app) storageFailure() string {
	if a.storageError != "" {
		return a.storageError
	}
	if err := a.saveError(); err != "" {
		return err
	}
	if a.notes.SaveBlocked() {
		return locales.StoragePaused()
	}
	return ""
}

func (a *app) storageStatus() string {
	if a.notes.SaveBlocked() {
		return locales.StoragePaused()
	}
	if a.notes.Unsaved() {
		return locales.StorageUnsaved()
	}
	if last := a.notes.LastSaveTime(); !last.IsZero() {
		return locales.StorageSavedAt(last.Format("15:04:05"))
	}
	return locales.StorageNotSaved()
}
