package app

// 在系统默认浏览器打开外部授权页（Device Flow 的 verification URI）。
// 只接受 https 地址：命令行参数里塞进以 '-' 开头的字符串会被 open /
// rundll32 当成选项解析，必须先由 url.Parse 约束 scheme 与 host。

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// openURLCommand 构造打开浏览器的命令（不执行），便于单测锁定参数。
func openURLCommand(raw string) (*exec.Cmd, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("授权页地址为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("授权页必须是 https 地址")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u.String()), nil
	case "windows":
		// rundll32 的 URL 处理器按协议注册表打开默认浏览器，不需要 shell 引号。
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u.String()), nil
	}
	return nil, errors.New("当前平台不支持打开外部浏览器")
}

// openExternalURL 在默认浏览器打开授权页；失败时调用方负责提示，浏览器
// 本身的生命周期与 Zakki 无关，不等待其退出。
func openExternalURL(raw string) error {
	cmd, err := openURLCommand(raw)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
