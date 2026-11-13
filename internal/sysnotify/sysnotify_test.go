package sysnotify

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

func TestAppleScriptUsesArguments(t *testing.T) {
	if !strings.Contains(appleScript, "on run argv") || !strings.Contains(appleScript, "item 2 of argv") || strings.Contains(appleScript, "%q") {
		t.Fatalf("AppleScript 未从参数读取正文: %q", appleScript)
	}
	for _, input := range []string{`" \\ '`, "👨‍💻", "弯引号‘’", "\n\t"} {
		if strings.Contains(appleScript, input) {
			t.Fatalf("用户输入不应出现在脚本: %q", input)
		}
	}
}

func TestToastScriptUsesTextNodesAndFixedAUMID(t *testing.T) {
	for _, part := range []string{"CreateTextNode", "$env:ZAKKI_NOTIFY_TITLE", "$env:ZAKKI_NOTIFY_BODY", powershellAUMID} {
		if !strings.Contains(toastScript, part) {
			t.Fatalf("脚本缺少 %q", part)
		}
	}
	if strings.Contains(toastScript, "zakki-reminder-") || !strings.Contains(powershellPath, `WindowsPowerShell\v1.0\powershell.exe`) {
		t.Fatal("通知 ID 不应作为 AUMID，PowerShell 路径必须固定")
	}
}

func TestEncodedPowerShellUTF16LE(t *testing.T) {
	input := "👨‍💻 弯引号‘’ <&'\""
	encoded := encodedPowerShell(input)
	bytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(bytes)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(bytes[i*2:])
	}
	if got := string(utf16.Decode(units)); got != input {
		t.Fatalf("解码 = %q, want %q", got, input)
	}
}

func TestNotificationCommandArgumentsAndTimeout(t *testing.T) {
	ctx, cancel := notificationContext()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if remaining := time.Until(deadline); !ok || remaining <= 9*time.Second || remaining > 10*time.Second {
		t.Fatalf("通知超时 = %v, 存在=%v, want 10s", remaining, ok)
	}
	n := Notification{Title: "标题 ‘'\"", Body: "👨‍💻\u200b\u00ad <&>\n\t“’"}
	mac := darwinCommand(ctx, n)
	if !slices.Equal(mac.Args, []string{"osascript", "-e", appleScript, n.Title, n.Body}) || mac.Cancel == nil {
		t.Fatalf("macOS 参数或取消路径错误: %+v", mac.Args)
	}
	win := windowsCommand(ctx, n)
	want := []string{powershellPath, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedPowerShell(toastScript)}
	if !slices.Equal(win.Args, want) || win.Cancel == nil {
		t.Fatalf("Windows 参数或取消路径错误: %+v", win.Args)
	}
	if !slices.Contains(win.Env, "ZAKKI_NOTIFY_TITLE="+n.Title) || !slices.Contains(win.Env, "ZAKKI_NOTIFY_BODY="+n.Body) {
		t.Fatal("Windows 未原样通过环境变量传值")
	}
}

func TestEscapeXML(t *testing.T) {
	if got := escapeXML(`<&'"`); got != `&lt;&amp;&apos;&quot;` {
		t.Fatalf("转义 = %q", got)
	}
}

// envValue 从 Cmd.Env 取指定键的值。
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

// TestNotificationSanitizesAndTruncates 锁定通知文本的净化与截断：粘贴
// 残留的控制符会让 toast 的 XmlDocument 解析失败，超长正文会撞上 Windows
// 环境块上限（32767 字符）而让整条通知静默丢掉。
func TestNotificationSanitizesAndTruncates(t *testing.T) {
	long := strings.Repeat("买", notifyBodyMaxRunes+50)
	n := Notification{
		Title: "标\x00题\x07",
		Body:  "\x07买\x00牛\u007f奶 " + long,
	}
	win := windowsCommand(context.Background(), n)
	body := envValue(win.Env, "ZAKKI_NOTIFY_BODY")
	title := envValue(win.Env, "ZAKKI_NOTIFY_TITLE")
	if title != "标题" {
		t.Fatalf("标题未净化: %q", title)
	}
	if !strings.HasPrefix(body, "买牛奶 ") {
		t.Fatalf("正文首段被破坏: %q", body)
	}
	if strings.ContainsAny(body, "\x00\x07\u007f") {
		t.Fatalf("正文仍含不可投递字符: %q", body)
	}
	if got := utf8.RuneCountInString(body); got != notifyBodyMaxRunes+1 {
		t.Fatalf("正文长度 = %d，期望 %d（含省略号）", got, notifyBodyMaxRunes+1)
	}
	if !strings.HasSuffix(body, "…") {
		t.Fatalf("截断后应带省略号: %q", body)
	}
	mac := darwinCommand(context.Background(), n)
	if mac.Args[3] != title || mac.Args[4] != body {
		t.Fatalf("macOS 未使用同一净化结果: %+v", mac.Args[3:])
	}
}
