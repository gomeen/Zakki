package text

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
)

// The language of a text decides the glyphs of the scripts it writes,
// which some fonts draw apart by language, as the Han characters Chinese
// and Japanese share, and the fonts the system falls back to for them.
// Han characters in a text of a language that does not write them, as a
// Chinese name in an English sentence, keep the glyphs the system picks
// for them by the user's settings, as they had without a language: the
// language says nothing of them, and an English app shown to a reader of
// Japanese should not draw a Japanese name with Chinese glyphs. Browsers
// do the same (Chromium's LayoutLocale::LocaleForHan). Core Text does it
// by itself; DirectWrite and Pango are told (cjkRuns).
//
// On Windows the user's settings are more than the locale of their
// regional formats, which DirectWrite falls back by. DirectWrite's fallback
// for English draws Chinese text in Yu Gothic UI, a Japanese font, where it
// has the characters, and in Microsoft JhengHei UI, a Traditional Chinese
// one, where it lacks them, strokes of two weights in one label (#1454):
// a user of English Windows who reads Chinese lists it among their
// preferred languages. Han characters in no language that writes them take
// the first of those that does (userHanLanguage), as Chromium takes the
// first of its accepted languages. Windows keeps two such lists: the
// display languages (GetUserPreferredUILanguages), only those whose
// language pack is installed, and the languages of Settings > Time &
// language > Language & region, which may list Chinese below English
// without its pack (#1479). The first is read first, the second where it
// has none (resolveUserHanLanguage), and both only once a text has Han
// characters in no language that writes them, which most apps' never do.

// writesHan reports whether a language writes Han characters: Chinese,
// with its languages of other codes, Japanese and Korean.
func writesHan(lang string) bool {
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	base, _, _ = strings.Cut(base, "_")
	switch base {
	case "zh", "ja", "ko", "yue", "cmn", "wuu", "hak", "nan", "gan", "hsn", "lzh", "cjy", "cpx", "czh", "czo", "mnp", "cdo":
		return true
	}
	return false
}

// isCJK reports whether r is a Han character, or a mark of punctuation
// or a full-width form that the languages writing Han draw apart.
func isCJK(r rune) bool {
	switch {
	case r < 0x2E80:
		return false
	case r >= 0x3000 && r <= 0x303F, r >= 0xFF00 && r <= 0xFFEF:
		return true
	}
	return unicode.Is(unicode.Han, r)
}

// languageShapes reports whether a language can change how a text is drawn
// or broken, and so whether the text engine must be told it, which costs Core
// Text a new break iterator for each line it breaks. Latin letters look the
// same in the languages but those whose fonts draw some of them apart (the
// Turkic languages' i and fi, Dutch's ij, Polish's ó, Catalan's l·l), and in
// the languages writing Han, the marks beyond ASCII may come from a font of
// that language rather than the text's.
func languageShapes(text []rune, lang string) bool {
	if lang == "" {
		return false
	}
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	base, _, _ = strings.Cut(base, "_")
	switch base {
	case "tr", "az", "crh", "tt", "kk", "ba", "nl", "pl", "ca":
		return true
	}
	limit := rune(0xFF)
	if writesHan(lang) {
		limit = 0x7F
	}
	for _, r := range text {
		if r > limit {
			return true
		}
	}
	return false
}

// langRun is a run of runes of a text, from start to end, in one language
// as the text engine is to be told it.
type langRun struct {
	start, end int
	lang       string
}

// langRuns returns the runs of a text whose language the engine must be
// told apart from base, the language of its style, given the language of
// the spans. A run of "" is in the engine's own language, as Han
// characters are in a language that does not write them. It returns nil
// when every rune is in base.
func langRuns(text []rune, base string, spans []Span) []langRun {
	if (base == "" || writesHan(base)) && !spansHaveLang(spans) {
		return nil
	}
	var out []langRun
	eachLang(text, base, spans, func(i int, lang string) {
		if lang != base {
			out = addLangRun(out, i, lang)
		}
	})
	return out
}

// userHanRuns returns the runs of Han characters of a text, and their
// punctuation, in no language that writes them: those langRuns leaves in
// the engine's own language, in which the system picks their glyphs for
// the user.
func userHanRuns(text []rune, base string, spans []Span) []langRun {
	if writesHan(base) && !spansHaveLang(spans) || !slices.ContainsFunc(text, isCJK) {
		return nil
	}
	var out []langRun
	eachLang(text, base, spans, func(i int, lang string) {
		if lang == "" && isCJK(text[i]) {
			out = addLangRun(out, i, "")
		}
	})
	return out
}

// eachLang calls fn with the index of each rune of a text and its language
// as the engine is to be told it: the base's, its span's, or "" for a Han
// character in a language that does not write it.
func eachLang(text []rune, base string, spans []Span, fn func(i int, lang string)) {
	span, from := 0, 0
	for i, r := range text {
		lang := base
		for span < len(spans) && max(spans[span].End, from) <= i {
			from = max(spans[span].End, from)
			span++
		}
		if span < len(spans) && spans[span].Lang != "" {
			lang = spans[span].Lang
		}
		if lang != "" && isCJK(r) && !writesHan(lang) {
			lang = ""
		}
		fn(i, lang)
	}
}

// addLangRun adds rune i, in a language, to runs, extending the last.
func addLangRun(runs []langRun, i int, lang string) []langRun {
	if n := len(runs); n > 0 && runs[n-1].end == i && runs[n-1].lang == lang {
		runs[n-1].end = i + 1
		return runs
	}
	return append(runs, langRun{i, i + 1, lang})
}

// userHanLanguage returns the language in which Han characters in no
// language that writes them are drawn, given the locale of the user's
// regional formats and the languages they prefer, in order: "" when the
// locale writes them, which the engine draws them in already, else the
// first preferred language that writes them, else "" for the system's own
// choice.
func userHanLanguage(locale string, preferred []string) string {
	if writesHan(locale) {
		return ""
	}
	for _, lang := range preferred {
		if writesHan(lang) {
			return lang
		}
	}
	return ""
}

// resolveUserHanLanguage returns userHanLanguage of the user's locale and
// the first of the lists of their preferred languages that has one that
// writes Han characters, reading each list only when those before it have
// none, and reading none when the locale writes them.
func resolveUserHanLanguage(locale string, lists ...func() []string) string {
	if writesHan(locale) {
		return ""
	}
	for _, list := range lists {
		if lang := userHanLanguage(locale, list()); lang != "" {
			return lang
		}
	}
	return ""
}

// splitMultiString returns the strings of a list in UTF-16, each ending
// in a NUL and the list in another (REG_MULTI_SZ, and the MUI's lists of
// languages), skipping empty ones; a last string without its NUL counts.
func splitMultiString(buf []uint16) []string {
	var out []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i > start {
				out = append(out, string(utf16.Decode(buf[start:i])))
			}
			start = i + 1
		}
	}
	if start < len(buf) {
		out = append(out, string(utf16.Decode(buf[start:])))
	}
	return out
}

func spansHaveLang(spans []Span) bool {
	for _, sp := range spans {
		if sp.Lang != "" {
			return true
		}
	}
	return false
}
