package app

// 桌面云同步桥接：把偏好 sync 节翻译成 notes.Store 的同步配置；管理
// GitHub Device Flow 连接、远端（或合并）内容写入后的界面重载，以及
// 窗口聚焦/失焦时的拉推时机（ADR-0004）。
// 后台 goroutine 只触碰注入的快照与系统存储，回主线程一律经窗口 Update。

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"zakki/internal/notes"
	"zakki/locales"
)

// syncState 是云同步桥接的主线程状态；不持有任何 ui 元素。匿名嵌入
// app，字段访问经 a.syncState。
type syncState struct {
	// handledRemoteApply 是已触发界面重载的最近「远端已应用」时间，
	// 防止每次回调重复重载。
	handledRemoteApply time.Time
	// lastFocusPull / lastBlurPush 是窗口聚焦拉取、失焦推送的最近时间（限频）。
	lastFocusPull, lastBlurPush time.Time
	// connecting 表示 Device Flow 进行中；带 15 分钟时效，应用重启前
	// 遗留的标志不会永久锁死连接按钮。
	connecting       bool
	connectStartedAt time.Time
	// flow 是 Device Flow 端点实例（懒创建）；keyring 是系统安全存储。
	flow    *notes.DeviceFlow
	keyring notes.Keyring
	// tokenConnected 缓存「安全存储里有凭据」的判定结果与时间，避免
	// 每帧调用系统安全存储。
	tokenCheckedAt time.Time
	tokenConnected bool
}

// dataDir 返回应用数据目录（preferences.json 所在目录）。
func (a *app) dataDir() string { return filepath.Dir(a.prefsStore.Path) }

// systemKeyringFactory 供测试注入内存实现；生产经 notes.SystemKeyring。
var systemKeyringFactory = notes.SystemKeyring

// syncDeliver 经仍存活的窗口把消化回调投回主线程；测试注入同步执行
// 以在无窗口环境下锁定「回调可达」。
var syncDeliver = func(win *mygo.Window, fn func()) { win.Update(fn) }

// systemKeyring 懒创建系统安全存储（失败只记日志，调用处如实展示）。
func (a *app) systemKeyring() notes.Keyring {
	if a.syncState.keyring != nil {
		return a.syncState.keyring
	}
	k, err := systemKeyringFactory(a.dataDir())
	if err != nil {
		slog.Warn("同步安全存储不可用", "error", err)
		return nil
	}
	a.syncState.keyring = k
	return k
}

// githubClientID 读取 GitHub App 的 Client ID（非凭据配置项，注册后经
// 环境变量注入；真实账号连接为待验项）。
func (a *app) githubClientID() string { return strings.TrimSpace(os.Getenv("ZAKKI_GITHUB_CLIENT_ID")) }

// githubOverrideBase 返回自测/联调用的 GitHub 端点覆盖（空 = 官方地址）。
// 仅经环境变量注入，供隔离探针用本地假服务跑通全流程；不提供设置页
// 入口，也不写进偏好。
func githubOverrideBase() string { return strings.TrimSpace(os.Getenv("ZAKKI_GITHUB_BASE_URL")) }

// deviceFlow 懒创建 Device Flow 实例；自测注入时指向同一假服务。
func (a *app) deviceFlow() *notes.DeviceFlow {
	if a.syncState.flow == nil {
		a.syncState.flow = notes.NewDeviceFlow()
		a.syncState.flow.Now = a.now
		if base := githubOverrideBase(); base != "" {
			a.syncState.flow.DeviceURL = base + "/login/device/code"
			a.syncState.flow.TokenURL = base + "/login/oauth/access_token"
		}
	}
	return a.syncState.flow
}

// syncStatePath 返回基线状态文件路径：应用数据目录的独立文件（设备本
// 地，不随笔记文件夹漂移；旧 Git 调度器只提交 zakki.json 精确路径，
// 该文件不会进入 Git 备份）。
func (a *app) syncStatePath() string { return filepath.Join(a.dataDir(), "sync-state.json") }

// syncEnabledPref 报告偏好是否指向已启用的 GitHub 后端。
func (a *app) syncEnabledPref() bool {
	return a.prefs.Sync.Enabled && a.prefs.Sync.Backend == syncBackendGitHub && a.prefs.Sync.Repo != ""
}

// configureSync 把当前偏好应用到 notes.Store：关闭即解除旧 Git 调度器
// 的暂停门禁（配置原样保留）；开启时构造后端 A 并注入 token provider。
// 每次偏好应用都会重放；未变化的配置在 Store 内部短路。
func (a *app) configureSync() {
	if !a.syncEnabledPref() {
		a.notes.ConfigureSync(nil, "", "", a.syncRefresh())
		return
	}
	clientID := a.githubClientID()
	provider := notes.TokenProvider(a.systemKeyring(), clientID, a.deviceFlow(), a.now)
	backend := notes.NewGitHubBackend(a.prefs.Sync.Repo, "", provider)
	if base := githubOverrideBase(); base != "" {
		backend.APIBase = base
	}
	a.notes.ConfigureSync(backend, a.prefs.Sync.Repo, a.syncStatePath(), a.syncRefresh())
}

// syncRefresh 供 Store 在每轮结束后回调：投递回主线程消化状态
// （重载远端或合并内容、刷新设置页）。窗口在回调触发时经
// atomic 持有器解析，而不是配置时捕获——configureSync 在建窗之前执行
// （偏好预置开启同步的冷启动），那时捕获的窗口指针恒为 nil，重载、
// toast 会静默丢失（#60）。
func (a *app) syncRefresh() func() {
	return func() {
		if win := a.syncRefreshTarget(); win != nil {
			syncDeliver(win, func() { a.syncPump() })
		}
	}
}

// syncRefreshTarget 在回调触发时解析投递窗口：主窗口优先，未开时回退
// 设置窗。两个持有器都是 atomic，后台回调 goroutine 读取不构成数据
// 竞态；窗口若恰在投递后关闭，Update 进主线程后对已关窗口是 no-op，
// 消化逻辑本身幂等。
func (a *app) syncRefreshTarget() *mygo.Window {
	if win := a.mainWindow.Load(); win != nil {
		return win
	}
	return a.settingsWindow.Load()
}

// syncOnActivate 在窗口激活（启动、唤起）时拉推一轮：前台聚焦时同步
// 的桌面触发点。主线程调用。
func (a *app) syncOnActivate() {
	a.syncPump()
	if a.syncEnabledPref() {
		a.syncState.lastFocusPull = a.now()
		a.notes.SyncNow()
	}
}

// syncFocusPullInterval / syncBlurPushInterval 是聚焦拉取、失焦推送的最小间隔：
// 反复切窗口不刷爆 GitHub API。
const (
	syncFocusPullInterval = 30 * time.Second
	syncBlurPushInterval  = 5 * time.Second
)

// syncOnFocus 在主窗口获得焦点时拉一轮（限频）：窗口本来就开着、从别的
// 应用点回来时，也先看到其他设备的修改再编辑。主线程调用。
func (a *app) syncOnFocus() {
	if !a.syncEnabledPref() || a.now().Sub(a.syncState.lastFocusPull) < syncFocusPullInterval {
		return
	}
	a.syncState.lastFocusPull = a.now()
	a.notes.SyncNow()
}

// syncOnBlur 在主窗口失焦或隐藏时，若有尚未推送的本机修改就立即保存并推送
// （限频）：换设备时另一端不必等防抖。同步轮内部会先 flush 待保存快照。
func (a *app) syncOnBlur() {
	if !a.syncEnabledPref() || a.now().Sub(a.syncState.lastBlurPush) < syncBlurPushInterval {
		return
	}
	if !a.notes.SyncHasLocalChanges() {
		return
	}
	a.syncState.lastBlurPush = a.now()
	a.notes.SyncNow()
}

// syncPump 消化 Store 发布的同步状态（主线程）：远端或合并内容已写入本地则
// 重载磁盘内容（load 会重排提醒，满足「同步成功应用远端后立即对账提醒」）。
func (a *app) syncPump() {
	st := a.notes.SyncStateSnapshot()
	if a.notes.SyncNeedsReload() && st.LastRemoteApply.After(a.syncState.handledRemoteApply) {
		a.syncState.handledRemoteApply = st.LastRemoteApply
		a.reloadNotes()
		if st.LastMerge.Equal(st.LastRemoteApply) {
			a.showToast(locales.SyncMerged())
		} else {
			a.showToast(locales.SyncRemoteApplied())
		}
	}
	a.refreshSettingsWindow()
}

// syncTokenConnected 报告安全存储里是否有可用凭据（带 5 秒缓存；系统
// 安全存储调用不能进构建帧）。
func (a *app) syncTokenConnected() bool {
	if a.now().Sub(a.syncState.tokenCheckedAt) < 5*time.Second && a.syncState.tokenCheckedAt != (time.Time{}) {
		return a.syncState.tokenConnected
	}
	connected := a.syncState.tokenConnected
	if k := a.systemKeyring(); k != nil {
		_, err := notes.LoadSyncToken(k)
		connected = err == nil
	}
	a.syncState.tokenConnected, a.syncState.tokenCheckedAt = connected, a.now()
	return connected
}

// invalidateTokenState 让凭据缓存立即过期（连接/断开后调用）。
func (a *app) invalidateTokenState() {
	a.syncState.tokenCheckedAt = time.Time{}
}

// connectGitHubSync 发起 Device Flow：请求设备码 → 展示用户码与打开
// 授权页入口 → 按间隔轮询直到完成/拒绝/过期。真实账号连接依赖 GitHub
// App 注册，属待验项。
func (a *app) connectGitHubSync() {
	if a.syncState.connecting && a.now().Sub(a.syncState.connectStartedAt) < connectFlowTimeout {
		return // 正在连接；遗留标志超过时效后允许重新发起
	}
	clientID := a.githubClientID()
	if clientID == "" {
		a.showToast(locales.SyncClientIDMissing())
		return
	}
	k := a.systemKeyring()
	if k == nil {
		a.showToast(locales.SyncKeyringUnavailable())
		return
	}
	flow := a.deviceFlow()
	a.syncState.connecting, a.syncState.connectStartedAt = true, a.now()
	a.refreshSettingsWindow()
	notify := a.settingsWin
	if notify == nil {
		notify = a.win
	}
	go func() {
		start, err := flow.Start(context.Background(), clientID)
		if err != nil {
			a.finishConnect(notify, err)
			return
		}
		// 用户码弹层在独立 goroutine 展示：Dialog.Message 阻塞至关闭，
		// 不能挡住同 goroutine 里的授权轮询。
		go a.showConnectCodeDialog(notify, start.VerificationURI, start.UserCode)
		deadline := start.ExpiresAt
		interval := start.Interval
		for a.now().Before(deadline) {
			time.Sleep(interval)
			tok, state, err := flow.Poll(context.Background(), clientID, start.DeviceCode)
			switch state {
			case notes.FlowDone:
				if err == nil {
					if saveErr := notes.SaveSyncToken(k, tok); saveErr != nil {
						a.finishConnect(notify, saveErr)
						return
					}
					a.finishConnect(notify, nil)
					return
				}
			case notes.FlowSlowDown:
				interval += 5 * time.Second
				continue
			case notes.FlowDenied, notes.FlowExpired:
				a.finishConnect(notify, err)
				return
			}
			if err != nil {
				a.finishConnect(notify, err)
				return
			}
		}
		a.finishConnect(notify, errors.New(locales.SyncConnectTimeout()))
	}()
}

// connectFlowTimeout 是连接流程的时效上限（超过后允许重新发起）。
const connectFlowTimeout = 15 * time.Minute

// showConnectCodeDialog 展示用户码并可选打开授权页（后台线程弹窗，
// MyGo 对话框自行投递主线程）。
func (a *app) showConnectCodeDialog(parent *mygo.Window, uri, code string) {
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  parent,
		Type:    mygo.MessageInfo,
		Title:   locales.SyncConnectTitle(),
		Message: locales.SyncConnectCode(code),
		Detail:  locales.SyncConnectHint(uri),
		Buttons: []string{locales.SyncConnectOpen(), locales.CommonClose()},
	})
	// 打开授权页交给设置页按钮，避免对话框阻塞轮询流程。
}

// finishConnect 收尾连接流程：经存活窗口回主线程复位状态并展示结果；
// 窗口全部关闭时结果丢弃（连接标志有时效，不会锁死按钮）。
func (a *app) finishConnect(notify *mygo.Window, err error) {
	deliver := func() {
		a.syncState.connecting = false
		a.invalidateTokenState()
		if err == nil {
			a.showToast(locales.SyncConnectedToast())
		} else {
			a.showToast(locales.SyncConnectFailed(err.Error()))
		}
		a.refreshSettingsWindow()
		if a.win != nil {
			a.win.Update(func() {})
		}
	}
	if notify != nil {
		notify.Update(deliver)
	}
}

// disconnectGitHubSync 清除系统安全存储中的同步凭据；同步开关保持原值，
// 之后的同步轮会如实报「未连接」。
func (a *app) disconnectGitHubSync() {
	k := a.systemKeyring()
	if k == nil {
		return
	}
	if err := notes.ClearSyncToken(k); err != nil {
		slog.Warn("清除同步凭据失败", "error", err)
		a.showToast(locales.SyncDisconnectFailed(err.Error()))
		return
	}
	a.invalidateTokenState()
	a.showToast(locales.SyncDisconnectedToast())
	a.refreshSettingsWindow()
}

// syncStatusLine 生成设置页状态文案（主线程调用）。
func (a *app) syncStatusLine() string {
	st := a.notes.SyncStateSnapshot()
	if !a.syncEnabledPref() {
		return locales.SyncOffStatus()
	}
	var b strings.Builder
	if st.Running {
		b.WriteString(locales.SyncRunningStatus())
	} else if !st.LastSync.IsZero() {
		b.WriteString(locales.SyncLastSync(st.LastSync.Format("01-02 15:04")))
	} else {
		b.WriteString(locales.SyncIdleStatus())
	}
	if !a.syncTokenConnected() {
		b.WriteString(" · " + locales.SyncNotConnected())
	}
	return strings.TrimSpace(b.String())
}
