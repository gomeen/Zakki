package app

// 系统通知投递与点击回落：打包运行时用 MyGo 原生通知（macOS
// UNUserNotificationCenter、Windows 托盘气泡），拿到真实应用身份与点击回调；
// 未打包运行（go run、go test）退回 internal/sysnotify 的 osascript /
// PowerShell 路径。何时发仍由 reminders.go 调度。

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"zakki/internal/sysnotify"
)

// errNotifyDenied 表示用户未允许系统通知；提醒调度据此在应用内提示一次，
// 不把通知静默丢掉。
var errNotifyDenied = errors.New("系统通知未允许")

// notifyDeliveryTimeout 覆盖首次投递时的系统授权弹窗等待；正常投递是瞬时的。
const notifyDeliveryTimeout = 30 * time.Second

// nativeNotifier 是 MyGo 通知的最小接口，便于用假实现测回落与拒绝语义。
type nativeNotifier interface {
	supported() bool
	show(ctx context.Context, id, title, body string) error
}

type mygoNotifier struct{}

func (mygoNotifier) supported() bool { return mygo.NotificationsSupported() }

func (mygoNotifier) show(ctx context.Context, id, title, body string) error {
	n := mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body})
	return n.ShowContext(ctx)
}

// systemNotifier 通道判定由 prewarm 在事件循环就绪后做一次：MyGo 的
// NotificationsSupported 依赖主线程与已启动的 App，从定时器 goroutine 提前问会
// 拿到 false（Windows 也会），一旦缓存就把整场会话锁在 PowerShell 回落上。
// 判定之前到达的极少数提醒按回落投递，不影响后续。MyGo 可用就只走原生，
// 不再回落到 osascript / PowerShell——用户拒绝授权后绕过系统设置继续弹横幅是错的。
type systemNotifier struct {
	fallback sysnotify.Notifier
	native   nativeNotifier
	onDenied func() // 首次发现未授权时调用一次

	mu        sync.Mutex
	resolved  bool
	useNative bool

	deniedOnce sync.Once
}

func newSystemNotifier(onDenied func()) *systemNotifier {
	return &systemNotifier{fallback: sysnotify.New(), native: mygoNotifier{}, onDenied: onDenied}
}

// prewarm 在 App 就绪后（主线程）判定并缓存投递通道。
func (n *systemNotifier) prewarm() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.resolved {
		return
	}
	n.useNative = n.native.supported()
	n.resolved = true
}

func (n *systemNotifier) Notify(msg sysnotify.Notification) error {
	msg = sysnotify.Sanitize(msg)
	n.mu.Lock()
	resolved, useNative := n.resolved, n.useNative
	n.mu.Unlock()
	if !resolved {
		// 事件循环还没起来：本次按回落投递，不缓存判定。
		return n.fallback.Notify(msg)
	}
	if !useNative {
		return n.fallback.Notify(msg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyDeliveryTimeout)
	defer cancel()
	err := n.native.show(ctx, msg.ID, msg.Title, msg.Body)
	if err == nil {
		return nil
	}
	if errors.Is(err, mygo.ErrNotificationsDenied) {
		if n.onDenied != nil {
			n.deniedOnce.Do(n.onDenied)
		}
		return errNotifyDenied
	}
	return err
}

// scheduleChannelResolution 在事件循环就绪后判定通道；必须在 App.Run 之前调用，
// WhenReady 会把回调排到启动完成后。
func (a *app) scheduleChannelResolution(n *systemNotifier) {
	mygo.App.WhenReady(n.prewarm)
}

// reminderNotificationID 编入标签持久 ID、实例 due 与提醒所在行：点击时按 ID
// 而不是标签索引定位，跨启动仍有效；同一实例重发替换旧通知。
func reminderNotificationID(tabID uint64, dueMs int64, line int) string {
	return "zakki-reminder/" + strconv.FormatUint(tabID, 10) + "/" +
		strconv.FormatInt(dueMs, 10) + "/" + strconv.Itoa(line)
}

func parseReminderNotificationID(id string) (tabID uint64, dueMs int64, line int, ok bool) {
	rest, found := strings.CutPrefix(id, "zakki-reminder/")
	if !found {
		return 0, 0, 0, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, 0, false
	}
	tabID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	dueMs, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	line = -1
	if len(parts) == 3 {
		line, err = strconv.Atoi(parts[2])
		if err != nil {
			return 0, 0, 0, false
		}
	}
	return tabID, dueMs, line, true
}

// registerReminderNotificationClicks 必须在 App.Run 之前调用：macOS 由通知
// 点击启动应用时，回调早于 WhenReady，晚注册会丢这一次点击。
func (a *app) registerReminderNotificationClicks() {
	mygo.App.OnNotificationClick(a.reminderNotificationClicked)
}

func (a *app) reminderNotificationClicked(id string) {
	tabID, dueMs, line, ok := parseReminderNotificationID(id)
	if !ok {
		return
	}
	// 回调已在主线程；WhenReady 兜住「点击启动应用」时还未就绪的窗口。
	mygo.App.WhenReady(func() { a.openReminderAt(tabID, dueMs, line) })
}

// openReminderAt 唤起窗口、切到通知所属标签，并把光标放到提醒所在条目。
func (a *app) openReminderAt(tabID uint64, dueMs int64, line int) {
	a.showWindow()
	a.focusReminderTab(tabID, dueMs, line)
}

// focusReminderTab 只改应用状态（窗口已就绪或已在显示），便于无窗口测试。
func (a *app) focusReminderTab(tabID uint64, dueMs int64, line int) {
	defer a.withWindow(func() {})
	i := a.tabIndexByID(tabID)
	if i < 0 {
		return // 标签已删除：只唤起窗口
	}
	if i != a.active {
		a.switchTab(i)
	}
	if a.cfg.Source {
		return // 源码模式沿用会话选区，不抢光标
	}
	if idx := reminderBlockIndex(a.mdBlocks(), dueMs, line); idx >= 0 {
		a.focusBlock(idx, -1, -1)
	}
}

// reminderBlockIndex 优先取行号命中的条目；行号因编辑漂移时退回按 due 匹配。
func reminderBlockIndex(blocks []liveBlock, dueMs int64, line int) int {
	match := -1
	for idx := range blocks {
		blk := &blocks[idx]
		if !blk.hasTime || blk.due.UnixMilli() != dueMs {
			continue
		}
		if line >= 0 && blk.start <= line && line <= blk.end {
			return idx
		}
		if match < 0 {
			match = idx
		}
	}
	return match
}
