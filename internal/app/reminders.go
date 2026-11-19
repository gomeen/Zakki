package app

// 提醒调度：主线程仅截取标签不可变快照，解析与投影在防抖后台进行。

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"zakki/internal/markdown"
	"zakki/internal/sysnotify"
	"zakki/locales"
)

type reminderEntry struct {
	tabID  uint64
	line   int
	title  string
	text   string
	due    time.Time
	alert  time.Duration
	fireAt time.Time
}

type reminderTab struct {
	id          uint64
	title, text string
}

// 同一实例的排定资格不包含正文；投递去重键仍含正文 hash。
type reminderOccurrence struct {
	tabID uint64
	due   int64
	alert time.Duration
}

func reminderOccurrenceOf(e reminderEntry) reminderOccurrence {
	return reminderOccurrence{tabID: e.tabID, due: e.due.UnixNano(), alert: e.alert}
}

type reminderCache struct {
	text   string
	blocks []liveBlock
}

type reminderScheduler struct {
	mu        sync.Mutex
	gen       int
	timers    []*time.Timer
	debounce  *time.Timer
	pollStop  chan struct{}
	notifier  sysnotify.Notifier
	connected bool
	cache     map[uint64]reminderCache
	tabs      []reminderTab
	pending   map[string]reminderEntry
	delivered map[string]time.Time
	// 首次装配严格只排未来触发，后续编辑允许未过期的新提醒立即投递。
	started bool
}

func reminderKey(e reminderEntry) string {
	body := sha256.Sum256([]byte(e.text))
	return fmt.Sprintf("%d-%d-%d-%x", e.tabID, e.due.UnixNano(), e.alert, body[:])
}

func (a *app) connectReminders() {
	a.registerReminderNotificationClicks()
	a.reminders.mu.Lock()
	// 未授权时提示一次：提醒不能被静默丢掉。
	notifier := newSystemNotifier(func() {
		a.runOnMain(func() { a.showToast(locales.NotificationsDenied()) })
	})
	// 通道判定等事件循环就绪（Windows 的托盘通道与 macOS 的打包判定都要求）。
	a.scheduleChannelResolution(notifier)
	a.reminders.notifier = notifier
	a.reminders.connected = true
	a.reminders.pollStop = make(chan struct{})
	stop := a.reminders.pollStop
	a.reminders.mu.Unlock()
	a.rebuildReminders()
	go a.pollReminders(stop)
}

// 巡检只消费最近的不可变快照，主窗口销毁后仍能补足重复实例。
func (a *app) pollReminders(stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			now := time.Now().Round(0)
			a.reconcileReminders(now)
		}
	}
}

func (a *app) stopReminders() {
	a.reminders.mu.Lock()
	defer a.reminders.mu.Unlock()
	a.reminders.invalidateLocked()
	a.reminders.connected = false
	if a.reminders.pollStop != nil {
		close(a.reminders.pollStop)
		a.reminders.pollStop = nil
	}
}

func (a *app) resumeReminders() {
	a.reminders.mu.Lock()
	if a.reminders.connected {
		a.reminders.mu.Unlock()
		return
	}
	a.reminders.connected = true
	a.reminders.pollStop = make(chan struct{})
	stop := a.reminders.pollStop
	a.reminders.mu.Unlock()
	a.rebuildReminders()
	go a.pollReminders(stop)
}

func (s *reminderScheduler) invalidateLocked() {
	s.gen++
	for _, t := range s.timers {
		t.Stop()
	}
	s.timers = nil
	if s.debounce != nil {
		s.debounce.Stop()
		s.debounce = nil
	}
}

func (a *app) reminderTabs() []reminderTab {
	out := make([]reminderTab, len(a.tabs))
	for i, tab := range a.tabs {
		out[i] = reminderTab{id: tab.ID, title: a.tabTitle(i), text: tab.Text}
	}
	return out
}

func (a *app) rescheduleReminders() {
	tabs := a.reminderTabs()
	clock := a.now
	a.reminders.mu.Lock()
	defer a.reminders.mu.Unlock()
	if !a.reminders.connected {
		return
	}
	a.reminders.invalidateLocked()
	a.reminders.tabs = tabs
	gen := a.reminders.gen
	// 主线程绝不解析完整文档，后台只接触复制出的字符串与 ID。
	a.reminders.debounce = time.AfterFunc(250*time.Millisecond, func() {
		a.installReminderTabs(tabs, gen, clock().Round(0))
	})
}

func (a *app) collectReminders() []reminderEntry {
	return collectReminderTabs(a.reminderTabs(), a.now())
}

func collectReminderTabs(tabs []reminderTab, now time.Time) []reminderEntry {
	var out []reminderEntry
	for _, tab := range tabs {
		out = append(out, collectReminderTab(tab, now)...)
	}
	return out
}

func collectReminderTab(tab reminderTab, now time.Time) []reminderEntry {
	return collectReminderBlocks(tab, liveBlocks(tab.text), now)
}

func collectReminderBlocks(tab reminderTab, blocks []liveBlock, now time.Time) []reminderEntry {
	var out []reminderEntry
	for _, blk := range blocks {
		if !blk.hasTime || !blk.hasAlert || blk.checked {
			continue
		}
		text := projectRegion(blk.text, 0, utf8.RuneCountInString(blk.text), true, false).text
		for _, due := range reminderDues(blk, now) {
			if now.After(due.Add(time.Minute)) {
				continue
			}
			out = append(out, reminderEntry{
				tabID: tab.id, line: blk.start, title: tab.title,
				text: strings.Join(strings.Fields(text), " "), due: due, alert: blk.alert, fireAt: due.Add(-blk.alert),
			})
		}
	}
	return out
}

// reminderDues 每次以墙钟重展未来窗口；额外保留一分钟内的过去实例，
// 是否允许补投仍由调度器的本会话排定资格决定。
func reminderDues(blk liveBlock, now time.Time) []time.Time {
	if blk.recur == "" {
		return []time.Time{blk.due}
	}
	rule, err := markdown.ParseRule(blk.recur)
	if err != nil {
		return []time.Time{blk.due}
	}
	value, err := markdown.Expand(rule, blk.due.UnixMilli(), now.UnixMilli(), 60, 64)
	var millis []int64
	if err != nil || json.Unmarshal([]byte(value), &millis) != nil {
		return nil
	}
	out := make([]time.Time, 0, len(millis)+1)
	// Expand 的左端开区间不能承载已排条目的迟到资格。
	past, err := markdown.Expand(rule, blk.due.UnixMilli(), now.Add(-time.Minute-time.Millisecond).UnixMilli(), 1, 64)
	var recent []int64
	if err == nil && json.Unmarshal([]byte(past), &recent) == nil {
		for _, ms := range recent {
			if ms <= now.UnixMilli() {
				out = append(out, time.UnixMilli(ms).In(blk.due.Location()))
			}
		}
	}
	for _, ms := range millis {
		if len(out) == 64 {
			break
		}
		out = append(out, time.UnixMilli(ms).In(blk.due.Location()))
	}
	return out
}

func (a *app) rebuildReminders() {
	tabs := a.reminderTabs()
	a.reminders.mu.Lock()
	if !a.reminders.connected {
		a.reminders.mu.Unlock()
		return
	}
	a.reminders.invalidateLocked()
	a.reminders.tabs = tabs
	gen := a.reminders.gen
	a.reminders.mu.Unlock()
	a.installReminderTabs(tabs, gen, a.now().Round(0))
}

func (a *app) installReminderTabs(tabs []reminderTab, gen int, now time.Time) {
	a.reminders.mu.Lock()
	if gen != a.reminders.gen || !a.reminders.connected {
		a.reminders.mu.Unlock()
		return
	}
	cache := a.reminders.cache
	a.reminders.mu.Unlock()
	next := make(map[uint64]reminderCache, len(tabs))
	var entries []reminderEntry
	for _, tab := range tabs {
		cached, ok := cache[tab.id]
		if !ok || cached.text != tab.text {
			cached = reminderCache{text: tab.text, blocks: liveBlocks(tab.text)}
		}
		for _, e := range collectReminderBlocks(tab, cached.blocks, now) {
			e.title = tab.title
			entries = append(entries, e)
		}
		next[tab.id] = cached
	}
	a.reminders.mu.Lock()
	if gen != a.reminders.gen || !a.reminders.connected {
		a.reminders.mu.Unlock()
		return
	}
	a.reminders.cache = next
	a.reminders.mu.Unlock()
	a.scheduleReminderSnapshot(entries, gen, now)
}

// scheduleReminderSnapshot 只消费不可变快照，按真实当前时间判迟到与去重。
func (a *app) scheduleReminderSnapshot(entries []reminderEntry, gen int, now time.Time) {
	a.reminders.mu.Lock()
	if gen != a.reminders.gen || !a.reminders.connected {
		a.reminders.mu.Unlock()
		return
	}
	a.reminders.debounce = nil
	// 同代巡检也会重装；先取消上轮计时器，避免每次巡检积累重复回调。
	for _, timer := range a.reminders.timers {
		timer.Stop()
	}
	a.reminders.timers = nil
	if a.reminders.pending == nil {
		a.reminders.pending = make(map[string]reminderEntry)
	}
	if a.reminders.delivered == nil {
		a.reminders.delivered = make(map[string]time.Time)
	}
	for key, at := range a.reminders.delivered {
		if now.Sub(at) > 24*time.Hour {
			delete(a.reminders.delivered, key)
		}
	}
	previous := a.reminders.pending
	eligible := make(map[reminderOccurrence]bool, len(previous))
	for _, e := range previous {
		eligible[reminderOccurrenceOf(e)] = true
	}
	a.reminders.pending = make(map[string]reminderEntry)
	first := !a.reminders.started
	a.reminders.started = true
	var immediate []reminderEntry
	for _, e := range entries {
		key := reminderKey(e)
		if _, sent := a.reminders.delivered[key]; sent {
			continue
		}
		if e.fireAt.After(now) {
			a.reminders.pending[key] = e
			a.reminders.timers = append(a.reminders.timers, time.AfterFunc(e.fireAt.Sub(now), func() { a.fireReminder(e, gen) }))
		} else if eligible[reminderOccurrenceOf(e)] && !now.After(e.due.Add(time.Minute)) {
			immediate = append(immediate, e)
		} else if !first && now.Before(e.due) {
			immediate = append(immediate, e)
		}
	}
	a.reminders.mu.Unlock()
	for _, e := range immediate {
		a.fireReminder(e, gen)
	}
}

// reconcileReminders 处理合盖/改时后已经过点的在途项；旧回调并发命中由 delivered 门禁去重。
func (a *app) reconcileReminders(now time.Time) {
	a.reminders.mu.Lock()
	if !a.reminders.connected || a.reminders.debounce != nil {
		a.reminders.mu.Unlock()
		return
	}
	gen := a.reminders.gen
	tabs := a.reminders.tabs
	var late []reminderEntry
	for _, e := range a.reminders.pending {
		if !e.fireAt.After(now) && !now.After(e.due.Add(time.Minute)) {
			late = append(late, e)
		}
	}
	a.reminders.mu.Unlock()
	for _, e := range late {
		a.fireReminder(e, gen)
	}
	a.installReminderTabs(tabs, gen, now)
}

func reminderBody(e reminderEntry) string {
	if e.title == "" {
		return e.text
	}
	if e.text == "" {
		return e.title
	}
	return e.title + " · " + e.text
}

func (a *app) fireReminder(e reminderEntry, gen int) {
	key := reminderKey(e)
	a.reminders.mu.Lock()
	if gen != a.reminders.gen || !a.reminders.connected {
		a.reminders.mu.Unlock()
		return
	}
	if a.reminders.delivered == nil {
		a.reminders.delivered = make(map[string]time.Time)
	}
	if _, sent := a.reminders.delivered[key]; sent {
		a.reminders.mu.Unlock()
		return
	}
	a.reminders.delivered[key] = time.Now().Round(0)
	delete(a.reminders.pending, key)
	notifier := a.reminders.notifier
	a.reminders.mu.Unlock()
	body := reminderBody(e)
	if notifier != nil {
		go func() {
			if err := notifier.Notify(sysnotify.Notification{ID: reminderNotificationID(e.tabID, e.due.UnixMilli(), e.line), Title: "Zakki", Body: body}); err != nil {
				slog.Warn("系统通知投递失败", "err", err)
			}
		}()
	}
	a.runOnMain(func() {
		a.reminders.mu.Lock()
		current := gen == a.reminders.gen && a.reminders.connected
		a.reminders.mu.Unlock()
		if current {
			a.showToast(body)
		}
	})
}
