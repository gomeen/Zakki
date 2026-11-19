package app

// 提醒调度测试：假投递器 + 注入时钟，覆盖派生规则、代际门禁与投递。
import (
	"strings"
	"testing"
	"time"

	"zakki/internal/sysnotify"
)

// fakeNotifier 用 channel 交付，避免测试睡眠等待后台 goroutine。
type fakeNotifier struct {
	ch chan sysnotify.Notification
}

func (f *fakeNotifier) Notify(n sysnotify.Notification) error {
	f.ch <- n
	return nil
}

func connectFakeNotifier(a *app) *fakeNotifier {
	fake := &fakeNotifier{ch: make(chan sysnotify.Notification, 8)}
	a.reminders.mu.Lock()
	a.reminders.notifier = fake
	a.reminders.connected = true
	a.reminders.mu.Unlock()
	return fake
}

func reminderGeneration(a *app) int {
	a.reminders.mu.Lock()
	defer a.reminders.mu.Unlock()
	return a.reminders.gen
}

func expectNoReminder(t *testing.T, fake *fakeNotifier, wait time.Duration) {
	t.Helper()
	select {
	case n := <-fake.ch:
		t.Fatalf("不应投递通知: %+v", n)
	case <-time.After(wait):
	}
}

func expectReminderBody(t *testing.T, fake *fakeNotifier, want string) {
	t.Helper()
	select {
	case n := <-fake.ch:
		if n.Body != want {
			t.Fatalf("通知正文 = %q, want %q", n.Body, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时未投递")
	}
}

func TestCollectRemindersReadableBlock(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"paragraph suffix", "正文 **粗体** [链接](https://example.com) @TIME ~10m 后缀\n续行\n\n下一段", "正文 粗体 链接 后缀 续行"},
		{"task boundary", "- [ ] **正文** [链接](https://example.com) @TIME ~10m 尾部\n- [ ] 下个任务", "正文 链接 尾部"},
		{"chip first", "@TIME ~10m **正文** [链接](https://example.com) 结尾", "正文 链接 结尾"},
		{"heading", "## **标题** @TIME ~10m 后缀", "标题 后缀"},
		{"whitespace", "- [ ] 正文\t  **粗体** @TIME ~10m", "正文 粗体"},
		{"no truncation", "- [ ] " + strings.Repeat("长正文", 200) + " @TIME ~10m", strings.Repeat("长正文", 200)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			a.tabs[0].Name = "独立标签"
			future := a.now().Add(24 * time.Hour).Format("2006-01-02 15:04")
			a.tabs[0].Text = strings.ReplaceAll(tc.content, "TIME", future)
			got := a.collectReminders()
			if len(got) != 1 {
				t.Fatalf("提醒数 = %d, want 1: %+v", len(got), got)
			}
			if got[0].text != tc.want || got[0].title != "独立标签" {
				t.Fatalf("提醒 = title %q text %q, want title %q text %q", got[0].title, got[0].text, "独立标签", tc.want)
			}
		})
	}
}

// TestCollectReminders 派生规则：只收未完成任务与非任务行、时刻在未来的提醒；
// 错过的不补发。
func TestCollectReminders(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	now := a.now() // 固定时钟
	future := now.Add(24 * time.Hour).Format("2006-01-02 15:04")
	past := now.Add(-24 * time.Hour).Format("2006-01-02 15:04")
	a.tabs[0].Text = strings.Join([]string{
		"- [ ] 未完成 @" + future + " ~10m", // 命中
		"- [x] 已完成 @" + future + " ~10m", // 完成即排除
		"- 团建 @" + future + " ~1h",       // 非任务行命中
		"- [ ] 错过 @" + past + " ~10m",    // 错过不补发
		"- [ ] 无提醒 @" + future,           // 无提醒令牌
		"- [ ] 普通行",
	}, "\n")
	got := a.collectReminders()
	if len(got) != 2 {
		t.Fatalf("应派生 2 条提醒，得到 %d: %+v", len(got), got)
	}
	wantAlerts := map[string]time.Duration{"未完成": 10 * time.Minute, "团建": time.Hour}
	for _, e := range got {
		want, ok := wantAlerts[e.text]
		if !ok || e.alert != want {
			t.Errorf("条目 %q 提前量 = %v, want %v（存在=%v）", e.text, e.alert, want, ok)
		}
		delete(wantAlerts, e.text)
	}
	if len(wantAlerts) != 0 {
		t.Errorf("遗漏条目: %v", wantAlerts)
	}
	// 触发时刻 = 截止减提前量。
	first := got[0]
	if !first.fireAt.Equal(first.due.Add(-first.alert)) {
		t.Errorf("fireAt = %v，要 截止-提前量", first.fireAt)
	}
}

// TestRebuildRemindersGenerational 编辑后全量重建：完成条目后定时器减少；
// 陈旧代号触发作废。
func TestRebuildRemindersGenerational(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	fake := connectFakeNotifier(a)
	future := a.now().Add(24 * time.Hour).Format("2006-01-02 15:04")
	a.tabs[0].Text = "- [ ] 甲 @" + future + " ~10m\n- [ ] 乙 @" + future + " ~1h"
	a.rebuildReminders()

	a.reminders.mu.Lock()
	timers := len(a.reminders.timers)
	a.reminders.mu.Unlock()
	if timers != 2 {
		t.Fatalf("应有 2 个在途定时器，得到 %d", timers)
	}

	// 勾选完成甲：派生只剩乙。
	a.tabs[0].Text = "- [x] 甲 @" + future + " ~10m\n- [ ] 乙 @" + future + " ~1h"
	a.rebuildReminders()
	a.reminders.mu.Lock()
	timers = len(a.reminders.timers)
	staleGen := a.reminders.gen - 1
	a.reminders.mu.Unlock()
	if timers != 1 {
		t.Fatalf("完成后应剩 1 个定时器，得到 %d", timers)
	}

	// 陈旧代号触发：不投递。
	a.fireReminder(reminderEntry{tabID: 1, title: "甲", text: "甲"}, staleGen)
	select {
	case n := <-fake.ch:
		t.Fatalf("陈旧代号不应投递: %+v", n)
	default:
	}
}

func TestRebuildRemindersEditedBody(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	fake := connectFakeNotifier(a)
	future := a.now().Add(24 * time.Hour).Format("2006-01-02 15:04")
	a.tabs[0].Name = "待办"
	a.tabs[0].Text = "- [ ] 旧正文 @" + future + " ~10m"
	a.rebuildReminders()
	before := a.collectReminders()
	if len(before) != 1 {
		t.Fatalf("编辑前提醒数 = %d, want 1", len(before))
	}
	old := before[0]
	oldGen := reminderGeneration(a)
	a.tabs[0].Text = "- [ ] **新正文** [链接](https://example.com) @" + future + " ~10m 后缀"
	a.rebuildReminders()
	a.fireReminder(old, oldGen)
	select {
	case n := <-fake.ch:
		t.Fatalf("旧代通知不应投递: %+v", n)
	default:
	}
	after := a.collectReminders()
	if len(after) != 1 {
		t.Fatalf("编辑后提醒数 = %d, want 1", len(after))
	}
	a.fireReminder(after[0], reminderGeneration(a))
	expectReminderBody(t, fake, "待办 · 新正文 链接 后缀")
}

func TestReminderSnapshotDelivery(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	fake := connectFakeNotifier(a)
	now := a.now()
	entries := []reminderEntry{{tabID: 1, title: "标签", text: "快照正文", fireAt: now.Add(10 * time.Millisecond)}}
	a.scheduleReminderSnapshot(entries, reminderGeneration(a), now)
	a.tabs[0].Text = "不应在后台读取的当前正文"
	expectReminderBody(t, fake, "标签 · 快照正文")
}

func TestReminderDebounceInvalidatesImmediately(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	fake := connectFakeNotifier(a)
	future := a.now().Add(24 * time.Hour).Format("2006-01-02 15:04")
	a.tabs[0].Text = "- [ ] 新正文 @" + future + " ~10m"
	oldGen := reminderGeneration(a)
	old := reminderEntry{title: "旧标签", text: "旧正文", fireAt: a.now().Add(40 * time.Millisecond)}
	a.scheduleReminderSnapshot([]reminderEntry{old}, oldGen, a.now())
	a.rescheduleReminders()
	if reminderGeneration(a) == oldGen {
		t.Fatal("编辑必须立即作废旧代号")
	}
	a.fireReminder(old, oldGen)
	// 即便已启动的旧防抖回调迟到，也不能重新装配旧快照。
	a.scheduleReminderSnapshot([]reminderEntry{old}, oldGen, a.now())
	expectNoReminder(t, fake, 100*time.Millisecond)
	a.reminders.mu.Lock()
	timers, pending := len(a.reminders.timers), a.reminders.debounce != nil
	a.reminders.mu.Unlock()
	if timers != 0 || !pending {
		t.Fatalf("防抖期 timers=%d pending=%v, want 0 true", timers, pending)
	}
}

func TestReminderDebounceUsesMainThreadSnapshot(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	fake := connectFakeNotifier(a)
	future := a.now().Add(24 * time.Hour).Format("2006-01-02 15:04")
	a.tabs[0].Text = "- [ ] 快照正文 @" + future + " ~10m"
	a.rescheduleReminders()
	// 不请求重调度的改动不属于本代；后台只能看到已采集的快照。
	a.tabs[0].Text = "没有提醒"
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		a.reminders.mu.Lock()
		pending, timers := a.reminders.debounce != nil, len(a.reminders.timers)
		a.reminders.mu.Unlock()
		if !pending {
			if timers != 1 {
				t.Fatalf("快照应装配 1 个定时器，得到 %d", timers)
			}
			break
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("防抖回调未完成")
		}
	}
	expectNoReminder(t, fake, 10*time.Millisecond)
}

func TestReminderStopAndReplacementGates(t *testing.T) {
	for _, action := range []string{"stop", "replacement", "rebuild"} {
		t.Run(action, func(t *testing.T) {
			a := newTestApp(t)
			t.Cleanup(a.shutdown)
			fake := connectFakeNotifier(a)
			a.rescheduleReminders()
			gen := reminderGeneration(a)
			entry := reminderEntry{title: "旧标签", text: "旧正文", fireAt: a.now().Add(10 * time.Millisecond)}
			switch action {
			case "stop":
				a.stopReminders()
			case "replacement":
				a.rescheduleReminders()
			case "rebuild":
				a.rebuildReminders()
			}
			a.scheduleReminderSnapshot([]reminderEntry{entry}, gen, a.now())
			a.fireReminder(entry, gen)
			if action == "stop" {
				// 连当前代号也不能绕过断连门禁。
				a.scheduleReminderSnapshot([]reminderEntry{entry}, reminderGeneration(a), a.now())
				a.fireReminder(entry, reminderGeneration(a))
			}
			// 等待有效防抖结束（最长 2 秒），再严格检查旧代未复活。
			done := make(chan struct{})
			go func() {
				defer close(done)
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for range tick.C {
					a.reminders.mu.Lock()
					pending := a.reminders.debounce != nil
					a.reminders.mu.Unlock()
					if !pending {
						return
					}
				}
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("防抖等待超时")
			}
			select {
			case n := <-fake.ch:
				t.Fatalf("旧代通知不应投递: %+v", n)
			default:
			}
			a.reminders.mu.Lock()
			timers, pending := len(a.reminders.timers), a.reminders.debounce != nil
			a.reminders.mu.Unlock()
			if timers != 0 || pending {
				t.Fatalf("旧回调不应装配定时器：timers=%d pending=%v", timers, pending)
			}
		})
	}
}

// TestFireReminderDelivers 到点投递系统通知，标题与正文来自标签名与待办文本。
func TestFireReminderDelivers(t *testing.T) {
	a := newTestApp(t)
	t.Cleanup(a.shutdown)
	fake := connectFakeNotifier(a)
	a.rebuildReminders() // 取当前代号
	a.reminders.mu.Lock()
	gen := a.reminders.gen
	a.reminders.mu.Unlock()
	due := a.now().Add(time.Hour)
	entry := reminderEntry{tabID: 1, line: 0, title: "待办", text: "交报告", due: due, alert: time.Minute}
	a.fireReminder(entry, gen)
	select {
	case n := <-fake.ch:
		wantID := reminderNotificationID(entry.tabID, entry.due.UnixMilli(), entry.line)
		if n.Body != "待办 · 交报告" || n.Title != "Zakki" || n.ID != wantID {
			t.Errorf("通知 = %+v, want 完整 ID、Zakki 标题与待办正文", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时未投递")
	}
}
