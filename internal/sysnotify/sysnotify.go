// Package sysnotify 投递一次性系统通知；何时发由 app 层调度。
package sysnotify

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

type Notification struct {
	ID    string
	Title string
	Body  string
}

type Notifier interface{ Notify(Notification) error }

func New() Notifier {
	switch runtime.GOOS {
	case "darwin":
		return darwinNotifier{}
	case "windows":
		return windowsNotifier{}
	default:
		return noopNotifier{}
	}
}

type noopNotifier struct{}

func (noopNotifier) Notify(Notification) error { return nil }

const appleScript = `on run argv
    display notification (item 2 of argv) with title (item 1 of argv)
end run`

type darwinNotifier struct{}

func notificationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

const (
	// notifyTitleMaxRunes / notifyBodyMaxRunes 限制单条通知文本：Windows 的
	// 环境块总量上限是 32767 字符，把整段笔记正文塞进去会让 CreateProcess
	// 直接失败，通知静默丢掉；两平台的通知中心也展示不了长段落。
	notifyTitleMaxRunes = 100
	notifyBodyMaxRunes  = 300
)

// Sanitize 返回投递前的净化副本：应用层改用 MyGo 原生通知投递时同样需要
// 这道处理（控制符与长度上限不随投递通道改变）。
func Sanitize(n Notification) Notification { return sanitizeNotification(n) }

// sanitizeNotification 截断超长文本并剔除投递环节不接受的字符：toast 的
// XmlDocument 要求 XML 1.0 字符集，粘贴残留的 C0 控制符会让整条通知报错。
// 换行与制表符保留（通知中心按行展示）。
func sanitizeNotification(n Notification) Notification {
	n.Title = clampNotificationText(n.Title, notifyTitleMaxRunes)
	n.Body = clampNotificationText(n.Body, notifyBodyMaxRunes)
	return n
}

func clampNotificationText(s string, maxRunes int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if !isNotificationRune(r) {
			continue
		}
		if n == maxRunes {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func isNotificationRune(r rune) bool {
	switch r {
	case '\n', '\t':
		return true
	}
	// C0、DEL、C1 与 Unicode 非字符：XML 1.0 不接受，通知中心也无意义。
	return r >= 0x20 && r != 0x7f && !(r >= 0x80 && r <= 0x9f) && r != 0xFFFE && r != 0xFFFF
}

func darwinCommand(ctx context.Context, n Notification) *exec.Cmd {
	n = sanitizeNotification(n)
	return exec.CommandContext(ctx, "osascript", "-e", appleScript, n.Title, n.Body)
}

func (darwinNotifier) Notify(n Notification) error {
	ctx, cancel := notificationContext()
	defer cancel()
	return darwinCommand(ctx, n).Run()
}

const powershellAUMID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`
const powershellPath = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

// 标题和正文仅通过环境变量传入；CreateTextNode 避免脚本字面量和 XML 注入。
const toastScript = `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = [Windows.Data.Xml.Dom.XmlDocument]::new()
$xml.LoadXml('<toast><visual><binding template="ToastGeneric"><text></text><text></text></binding></visual></toast>')
$text = $xml.GetElementsByTagName('text')
$text.Item(0).AppendChild($xml.CreateTextNode($env:ZAKKI_NOTIFY_TITLE)) | Out-Null
$text.Item(1).AppendChild($xml.CreateTextNode($env:ZAKKI_NOTIFY_BODY)) | Out-Null
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('` + powershellAUMID + `').Show($toast)
`

func encodedPowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	bytes := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(bytes[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

type windowsNotifier struct{}

func windowsCommand(ctx context.Context, n Notification) *exec.Cmd {
	n = sanitizeNotification(n)
	cmd := exec.CommandContext(ctx, powershellPath, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedPowerShell(toastScript))
	cmd.Env = append(os.Environ(), "ZAKKI_NOTIFY_TITLE="+n.Title, "ZAKKI_NOTIFY_BODY="+n.Body)
	hideNotificationWindow(cmd)
	return cmd
}

func (windowsNotifier) Notify(n Notification) error {
	ctx, cancel := notificationContext()
	defer cancel()
	return windowsCommand(ctx, n).Run()
}

// 保留兼容测试入口；通知脚本已不依赖任何字符串转义。
func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "'", "&apos;", `"`, "&quot;").Replace(s)
}
