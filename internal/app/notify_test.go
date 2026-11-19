package app

// 系统通知投递与点击回落：ID 编解码、通道选择、拒绝语义与点击定位。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"zakki/internal/sysnotify"
)

func TestReminderNotificationIDRoundTrip(t *testing.T) {
	id := reminderNotificationID(42, 1700000000123, 7)
	tabID, dueMs, line, ok := parseReminderNotificationID(id)
	if !ok || tabID != 42 || dueMs != 1700000000123 || line != 7 {
		t.Fatalf("解析 %q = (%d, %d, %d, %v)", id, tabID, dueMs, line, ok)
	}
	// 两段（没有行号）仍可用：行号只是提示。
	tabID, dueMs, line, ok = parseReminderNotificationID("zakki-reminder/5/-9")
	if !ok || tabID != 5 || dueMs != -9 || line != -1 {
		t.Fatalf("两段 ID 解析 = (%d, %d, %d, %v)", tabID, dueMs, line, ok)
	}
	for _, bad := range []string{
		"",
		"mygo-abc",
		"zakki-reminder-old-1-2-3",
		"zakki-reminder/1",
		"zakki-reminder/x/1/2",
		"zakki-reminder/1/y/2",
		"zakki-reminder/1/2/z",
		"zakki-reminder/1/2/3/4",
	} {
		if _, _, _, ok := parseReminderNotificationID(bad); ok {
			t.Fatalf("%q 不应被当成本应用提醒 ID", bad)
		}
	}
}

type fakeNativeNotifier struct {
	available bool
	err       error
	shown     []sysnotify.Notification
}

func (f *fakeNativeNotifier) supported() bool { return f.available }

func (f *fakeNativeNotifier) show(_ context.Context, id, title, body string) error {
	f.shown = append(f.shown, sysnotify.Notification{ID: id, Title: title, Body: body})
	return f.err
}

type fakeFallbackNotifier struct{ got []sysnotify.Notification }

func (f *fakeFallbackNotifier) Notify(n sysnotify.Notification) error {
	f.got = append(f.got, n)
	return nil
}

func TestSystemNotifierFallsBackWhenUnsupported(t *testing.T) {
	native := &fakeNativeNotifier{available: false}
	fallback := &fakeFallbackNotifier{}
	n := &systemNotifier{fallback: fallback, native: native}
	// 判定之前到达的提醒：按回落投递且不缓存判定。
	if err := n.Notify(sysnotify.Notification{ID: "early", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("回落投递失败: %v", err)
	}
	if len(native.shown) != 0 || len(fallback.got) != 1 || fallback.got[0].ID != "early" {
		t.Fatalf("未判定时应走回落: native=%+v fallback=%+v", native.shown, fallback.got)
	}
	n.prewarm() // 就绪后判定为不可用
	if err := n.Notify(sysnotify.Notification{ID: "id2"}); err != nil {
		t.Fatal(err)
	}
	if len(native.shown) != 0 || len(fallback.got) != 2 {
		t.Fatalf("不可用时应走回落: native=%+v fallback=%+v", native.shown, fallback.got)
	}
	// 通道只判定一次：之后即使原生报告可用也仍走回落。
	native.available = true
	n.prewarm()
	if err := n.Notify(sysnotify.Notification{ID: "id3"}); err != nil {
		t.Fatal(err)
	}
	if len(native.shown) != 0 || len(fallback.got) != 3 {
		t.Fatalf("通道判定被重复执行: native=%+v fallback=%+v", native.shown, fallback.got)
	}
}

func TestSystemNotifierPrewarmChoosesNative(t *testing.T) {
	native := &fakeNativeNotifier{available: true}
	fallback := &fakeFallbackNotifier{}
	n := &systemNotifier{fallback: fallback, native: native}
	n.prewarm()
	if err := n.Notify(sysnotify.Notification{ID: "id", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("原生投递失败: %v", err)
	}
	if len(native.shown) != 1 || len(fallback.got) != 0 {
		t.Fatalf("判定可用后应只走原生: native=%+v fallback=%+v", native.shown, fallback.got)
	}
}

func TestSystemNotifierNativeSanitizes(t *testing.T) {
	native := &fakeNativeNotifier{available: true}
	fallback := &fakeFallbackNotifier{}
	n := &systemNotifier{fallback: fallback, native: native}
	n.prewarm()
	title := "提醒\x00\x07\n标题"
	body := strings.Repeat("长", 400) // 正文上限 300 字符（sysnotify）
	if err := n.Notify(sysnotify.Notification{ID: "id", Title: title, Body: body}); err != nil {
		t.Fatal(err)
	}
	if len(native.shown) != 1 || len(fallback.got) != 0 {
		t.Fatalf("应只走原生通道: native=%+v fallback=%+v", native.shown, fallback.got)
	}
	got := native.shown[0]
	if strings.ContainsAny(got.Title, "\x00\x07") {
		t.Fatalf("标题未净化控制符: %q", got.Title)
	}
	if !strings.HasSuffix(got.Body, "…") {
		t.Fatalf("超长正文未截断: %q", got.Body)
	}
}

func TestSystemNotifierDeniedOnce(t *testing.T) {
	native := &fakeNativeNotifier{available: true, err: mygo.ErrNotificationsDenied}
	fallback := &fakeFallbackNotifier{}
	denied := 0
	n := &systemNotifier{fallback: fallback, native: native, onDenied: func() { denied++ }}
	n.prewarm()
	for i := 0; i < 2; i++ {
		err := n.Notify(sysnotify.Notification{ID: "id", Body: "b"})
		if !errors.Is(err, errNotifyDenied) {
			t.Fatalf("第 %d 次投递错误 = %v，应以未授权上报", i+1, err)
		}
	}
	if denied != 1 {
		t.Fatalf("未授权提示次数 = %d，want 1", denied)
	}
	if len(fallback.got) != 0 {
		t.Fatalf("拒绝授权后不得绕过系统设置回落: %+v", fallback.got)
	}
}

func TestFocusReminderTab(t *testing.T) {
	a := newTestApp(t)
	if len(a.tabs) < 2 {
		t.Fatalf("测试需要至少两个标签，实际 %d", len(a.tabs))
	}
	a.tabs[1].Text = "先做别的\n- [ ] 带伞 @2024-06-02 09:00 ~0m\n收尾"
	blocks := liveBlocks(a.tabs[1].Text)
	want := -1
	for i, blk := range blocks {
		if blk.hasTime {
			want = i
		}
	}
	if want < 0 {
		t.Fatal("测试文本应解析出时间块")
	}
	dueMs := blocks[want].due.UnixMilli()

	a.focusReminderTab(a.tabs[1].ID, dueMs, blocks[want].start)
	if a.active != 1 {
		t.Fatalf("应切到标签 1，实际 active=%d", a.active)
	}
	if a.mdActive < 0 {
		t.Fatal("应把光标放进提醒所在块")
	}
	if got := a.mdBlocks()[a.mdActive]; got.due.UnixMilli() != dueMs {
		t.Fatalf("光标所在块 due=%v，want %d", got.due, dueMs)
	}
	if a.mdCaret != len([]rune(a.mdBlocks()[a.mdActive].raw)) {
		t.Fatalf("光标应在块末，实际 %d", a.mdCaret)
	}
}

func TestFocusReminderTabIgnoresDeletedTab(t *testing.T) {
	a := newTestApp(t)
	a.active = 0
	a.focusReminderTab(9999, 0, -1)
	if a.active != 0 {
		t.Fatalf("标签不存在时不应切标签，active=%d", a.active)
	}
}
