package app

import (
	"time"

	"zakki/internal/prefs"
)

type preferences = prefs.Preferences
type appearancePrefs = prefs.Appearance
type windowPrefs = prefs.Window
type keymapPrefs = prefs.Keymap
type advancedPrefs = prefs.Advanced
type prefsIssue = prefs.Issue
type prefsIssueKind = prefs.IssueKind

const (
	prefsIssueNone        = prefs.IssueNone
	prefsIssuePrefsBroken = prefs.IssuePrefsBroken

	styleStandard = prefs.StyleStandard
	styleSketch   = prefs.StyleSketch

	syncBackendGitHub = prefs.SyncBackendGitHub
)

func defaultPreferences() preferences {
	return prefs.Default()
}

func validatePrefsAccel(acc string) error {
	if acc == "" {
		return nil
	}
	_, err := parseAccelerator(acc)
	return err
}

func validatePrefs(p preferences) error { return p.Validate(validatePrefsAccel) }

func clonePrefs(p preferences) preferences { return p.Clone() }

func decodePreferences(raw []byte) (preferences, error) {
	return prefs.Decode(raw, validatePrefsAccel)
}

func newPrefsStore(prefsPath string) *prefs.Store {
	return prefs.NewStore(prefsPath, 500*time.Millisecond)
}

func migrateToggleWindow(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	return normalizeLegacyAccelerator(raw)
}
