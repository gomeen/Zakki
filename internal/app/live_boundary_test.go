package app

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// Exercise the real input pipeline: one paragraph break must be removable by
// one Backspace, including restoring the insertion point, not just its display.
func TestLiveEnterBackspaceRoundTrip(t *testing.T) {
	for _, text := range []string{"a", "a|b|c", "中文", "# 标题"} {
		t.Run(text, func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			a.tabs[0].Text = text
			tt := ui.NewTester(a.view, 400, 540)
			n := utf8.RuneCountInString(text)
			a.selSyncStart, a.selSyncEnd = n, n
			tt.Frame()
			tt.Key(0, ui.KeyEnter)
			// Flush extra frames to rule out delayed selection publication.
			for frame := 0; frame < 3; frame++ {
				tt.Frame()
			}
			tt.Key(0, ui.KeyBackspace)
			if got := a.tabs[0].Text; got != text {
				t.Fatalf("one Backspace must undo one Enter: doc=%q block=%d draft=%q, want %q", got, a.mdActive, a.mdDraft, text)
			}
			tt.Type("X")
			if got, want := a.tabs[0].Text, text+"X"; got != want {
				t.Fatalf("caret not restored to previous line end: doc=%q, want %q", got, want)
			}
		})
	}
}

func TestLiveBackspaceBoundary(t *testing.T) {
	cases := []struct {
		name, doc   string
		block       int
		want, typed string
	}{
		{"paragraphs with suffix", "甲\n\n乙\n\n尾", 2, "甲乙\n\n尾", "甲X乙\n\n尾"},
		{"multiline previous paragraph", "甲\n乙\n\n丙", 2, "甲\n乙丙", "甲\n乙X丙"},
		{"multiline current paragraph", "甲\n\n乙\n尾", 2, "甲乙\n尾", "甲X乙\n尾"},
		{"extra blank paragraph", "甲\n\n\n乙", 3, "甲\n\n乙", "甲\n\nX乙"},
		{"leading empty paragraph", "\n\n甲", 2, "甲", "X甲"},
		{"adjacent list rows preserve suffix", "- 甲\n- 乙\n尾", 1, "- 甲\n乙\n尾", "- 甲\nX乙\n尾"},
		{"code fence remains intact", "```\nx\n```\n\n尾", 2, "```\nx\n```\n\n尾", "```\nx\n```\n\nX尾"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			a.tabs[0].Text = tc.doc
			tt := ui.NewTester(a.view, 400, 540)
			a.focusBlock(tc.block, 0, 0)
			tt.Frame()
			// No extra frame: programmatic selection must update the key-hint cache.
			tt.Key(0, ui.KeyBackspace)
			if got := a.tabs[0].Text; got != tc.want {
				t.Fatalf("Backspace doc=%q, want %q", got, tc.want)
			}
			tt.Type("X")
			if got := a.tabs[0].Text; got != tc.typed {
				t.Fatalf("insertion point/suffix corrupted: doc=%q, want %q", got, tc.typed)
			}
		})
	}
}

func TestEmptyListExitAfterMultilineBlockKeepsCaret(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   ui.Key
		caret int
	}{
		{name: "Enter", key: ui.KeyEnter, caret: 2},
		{name: "Backspace", key: ui.KeyBackspace, caret: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			a.tabs[0].Text = "甲\n乙\n\n- \n\n尾"
			tt := ui.NewTester(a.view, 400, 540)
			a.focusBlock(2, tc.caret, tc.caret)
			tt.Frame()
			tt.Key(0, tc.key)
			if got, want := a.tabs[0].Text, "甲\n乙\n\n\n\n尾"; got != want {
				t.Fatalf("退出空列表后文档=%q, want %q", got, want)
			}
			tt.Type("X")
			if got, want := a.tabs[0].Text, "甲\n乙\n\nX\n\n尾"; got != want {
				t.Fatalf("退出空列表后光标错误: 文档=%q, want %q", got, want)
			}
		})
	}
}

func TestLiveParagraphSplitRoundTrip(t *testing.T) {
	for _, text := range []string{"甲乙", "甲乙\n\n尾", "# 标题"} {
		// Hidden heading prefixes are no longer caret positions.
		first := projectBlock(liveBlocks(text)[0]).sourceOffset(0)
		for caret := first; caret <= utf8.RuneCountInString(text); caret++ {
			// Split only the first block; the rest is an untouched suffix.
			if caret > utf8.RuneCountInString(liveBlocks(text)[0].raw) {
				break
			}
			t.Run(fmt.Sprintf("%q/%d", text, caret), func(t *testing.T) {
				a := newTestApp(t)
				t.Cleanup(a.shutdown)
				a.tabs[0].Text = text
				tt := ui.NewTester(a.view, 400, 540)
				a.selSyncStart, a.selSyncEnd = caret, caret
				tt.Frame()
				tt.Key(0, ui.KeyEnter)
				tt.Key(0, ui.KeyBackspace)
				if a.tabs[0].Text != text {
					t.Fatalf("split/merge changed text: %q, want %q", a.tabs[0].Text, text)
				}
				tt.Type("X")
				b := runeToByte(text, caret)
				if want := text[:b] + "X" + text[b:]; a.tabs[0].Text != want {
					t.Fatalf("split/merge changed caret: %q, want %q", a.tabs[0].Text, want)
				}
			})
		}
	}
}

func TestLiveRepeatedEnterBackspace(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.tabs[0].Text = ""
	tt := ui.NewTester(a.view, 400, 540)
	tt.Type("a")
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	if a.tabs[0].Text != "a\n\n\n" {
		t.Fatalf("two Enters: %q", a.tabs[0].Text)
	}
	tt.Key(0, ui.KeyBackspace)
	if a.tabs[0].Text != "a\n\n" {
		t.Fatalf("first Backspace should remove only the extra empty paragraph: %q", a.tabs[0].Text)
	}
	tt.Key(0, ui.KeyBackspace)
	if a.tabs[0].Text != "a" {
		t.Fatalf("second Backspace should return to a: %q", a.tabs[0].Text)
	}
	tt.Type("X")
	if a.tabs[0].Text != "aX" {
		t.Fatalf("repeated Enter/Backspace lost caret: %q", a.tabs[0].Text)
	}
}

func TestSingleNewlineModesUnaffected(t *testing.T) {
	for _, source := range []bool{false, true} {
		t.Run(fmt.Sprintf("source=%t", source), func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			a.tabs[0].Text = ""
			a.cfg.Source = source
			tt := ui.NewTester(a.view, 400, 540)
			tt.Type("a")
			mods := ui.Shift
			if source {
				mods = 0
			}
			tt.Key(mods, ui.KeyEnter)
			if a.tabs[0].Text != "a\n" {
				t.Fatalf("native newline: %q", a.tabs[0].Text)
			}
			tt.Key(0, ui.KeyBackspace)
			tt.Type("X")
			if a.tabs[0].Text != "aX" {
				t.Fatalf("native newline round trip: %q", a.tabs[0].Text)
			}
		})
	}
}

func TestLiveArrowSkipsParagraphSeparator(t *testing.T) {
	for _, key := range []ui.Key{ui.KeyLeft, ui.KeyUp, ui.KeyRight, ui.KeyDown} {
		t.Run(fmt.Sprint(key), func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			a.tabs[0].Text = "甲\n\n乙"
			tt := ui.NewTester(a.view, 400, 540)
			backward := key == ui.KeyLeft || key == ui.KeyUp
			if backward {
				a.focusBlock(2, 0, 0)
			} else {
				a.focusBlock(0, 1, 1)
			}
			tt.Frame()
			tt.Key(0, key)
			want := 2
			if backward {
				want = 0
			}
			if a.mdActive != want {
				t.Fatalf("arrow stopped in separator: block=%d, want %d", a.mdActive, want)
			}
			tt.Type("X")
			typed := "甲\n\nX乙"
			if backward {
				typed = "甲X\n\n乙"
			}
			if got := a.tabs[0].Text; got != typed {
				t.Fatalf("arrow caret doc=%q, want %q", got, typed)
			}
		})
	}
}

// ⌃P/⌃N 在 darwin 上必须与上下方向键同语义：跨块走边界提示，块内走原生移动。
func TestLiveCtrlPMovesToPreviousBlock(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.tabs[0].Text = "甲\n\n乙\n\n尾"
	tt := ui.NewTester(a.view, 400, 540)
	a.focusBlock(1, 0, 0)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyP)
	for frame := 0; frame < 3; frame++ {
		tt.Frame()
	}
	tt.Type("X")
	if got, want := a.tabs[0].Text, "甲X\n\n乙\n\n尾"; got != want {
		t.Fatalf("⌃P doc=%q, want %q", got, want)
	}
}

func TestLiveCtrlNMovesWithinBlock(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	a.tabs[0].Text = "甲\n乙"
	tt := ui.NewTester(a.view, 400, 540)
	a.focusBlock(0, 1, 1) // 第一个软行的行尾
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyN)
	tt.Type("X")
	if got, want := a.tabs[0].Text, "甲\n乙X"; got != want {
		t.Fatalf("⌃N doc=%q, want %q", got, want)
	}
}
