package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFontPreferencesVersion7RoundTrip(t *testing.T) {
	if CurrentVersion != 14 {
		t.Fatalf("CurrentVersion = %d, want 14", CurrentVersion)
	}
	want := Default()
	want.Appearance.BodyFont = "songti"
	want.Appearance.LatinFont = "bradley"
	want.Appearance.MonoFont = "sfmono"
	want.Appearance.CodeIndent = "space4"
	want.Appearance.CodeTheme = ""
	want.Appearance.BodySize = 19
	want.Storage.DataFile = filepath.Join(t.TempDir(), "zakki.json")
	want.Keymap.Overrides["format.bold"] = "CmdOrCtrl+B"
	old := want.Clone()
	old.Version = 7
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("v7 migration changed values: got %+v, want %+v", got, want)
	}
	assertFontPreferencesSaved(t, got)
}

func TestFontPreferencesLocalSave(t *testing.T) {
	p := Default()
	p.Appearance.BodyFont = "local:Dank Mono"
	p.Appearance.LatinFont = "local:Dank Mono"
	p.Appearance.MonoFont = "local:Dank Mono"
	assertFontPreferencesSaved(t, p)
}

func TestFontPreferencesRejectInvalidKeys(t *testing.T) {
	for _, field := range []string{"bodyFont", "latinFont", "monoFont"} {
		for _, key := range []string{
			"unknown", "Dank Mono", "local:", "local: ",
			"local: Dank Mono", "local:Dank Mono ",
			"local:Dank Mono,Arial", "local:Dank\nMono", "local:Dank\rMono",
			"local:Dank\tMono", "local:Dank\x00Mono", "local:Dank\x7fMono",
			"local:" + strings.Repeat("a", 201),
			"local:" + strings.Repeat("字", 67),
		} {
			t.Run(field+"/"+key, func(t *testing.T) {
				p := Default()
				setFontPreference(&p, field, key)
				if err := p.Validate(nil); err == nil {
					t.Fatalf("Validate accepted %s = %q", field, key)
				}
				raw, err := json.Marshal(p)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Decode(raw, nil); err == nil {
					t.Fatalf("Decode accepted %s = %q", field, key)
				}
			})
		}
	}
}

func TestFontPreferencesFira(t *testing.T) {
	p := Default()
	p.Appearance.LatinFont, p.Appearance.MonoFont = "fira", "fira"
	assertFontPreferencesSaved(t, p)
	p.Appearance.BodyFont = "fira"
	if err := p.Validate(nil); err == nil {
		t.Fatal("bodyFont must not accept fira")
	}
}

func TestFontPreferencesExistingCombinations(t *testing.T) {
	for _, body := range []string{"", "kai", "sonnet", "pingfang", "songti", "hand", "hannotate"} {
		for _, latin := range []string{"", "jetbrains", "system", "noteworthy", "bradley"} {
			for _, mono := range []string{"", "jbm", "sonnet", "sfmono", "menlo"} {
				p := Default()
				p.Appearance.BodyFont, p.Appearance.LatinFont, p.Appearance.MonoFont = body, latin, mono
				if err := p.Validate(nil); err != nil {
					t.Fatalf("existing fonts %q/%q/%q rejected: %v", body, latin, mono, err)
				}
			}
		}
	}
}

func TestFontPreferencesLocalKeysDoNotRelaxOtherEnums(t *testing.T) {
	for _, mutate := range []func(*Preferences){
		func(p *Preferences) { p.Appearance.AppearanceMode = "local:Dank Mono" },
		func(p *Preferences) { p.Appearance.Language = "local:Dank Mono" },
		func(p *Preferences) { p.Appearance.ColorTheme = "local:Dank Mono" },
		func(p *Preferences) { p.Appearance.MarkdownTheme = "local:Dank Mono" },
		func(p *Preferences) { p.Appearance.CodeTheme = "local:Dank Mono" },
		func(p *Preferences) { p.Appearance.CodeIndent = "local:Dank Mono" },
	} {
		p := Default()
		mutate(&p)
		if err := p.Validate(nil); err == nil {
			t.Fatalf("non-font enum accepted local key: %+v", p.Appearance)
		}
	}
}

func setFontPreference(p *Preferences, field, key string) {
	switch field {
	case "bodyFont":
		p.Appearance.BodyFont = key
	case "latinFont":
		p.Appearance.LatinFont = key
	case "monoFont":
		p.Appearance.MonoFont = key
	}
}

func assertFontPreferencesSaved(t *testing.T, p Preferences) {
	t.Helper()
	if err := p.Validate(nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := NewStore(path, 0).SaveNow(p); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("saved round trip: got %+v, want %+v", got, p)
	}
}
