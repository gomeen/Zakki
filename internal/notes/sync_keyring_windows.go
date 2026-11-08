//go:build windows

// Windows：凭据以 DPAPI 当前用户级加密后存入数据目录的独立文件
// （sync-token.bin）。其他 Windows 账户无法解密；文件不经由偏好或
// 笔记文件，也不进入 Git 备份。PowerShell 经 EncodedCommand 调用，
// 避免 shell 注入与命令行泄密。

package notes

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type dpapiKeyring struct {
	file string
}

// SystemKeyring 返回 Windows DPAPI 实现；密文存 dataDir/sync-token.bin。
func SystemKeyring(dataDir string) (Keyring, error) {
	if dataDir == "" {
		return nil, errors.New("缺少数据目录，无法初始化安全存储")
	}
	return dpapiKeyring{file: filepath.Join(dataDir, "sync-token.bin")}, nil
}

func (k dpapiKeyring) script(op string) (string, error) {
	path, err := psQuote(k.file)
	if err != nil {
		return "", err
	}
	switch op {
	case "load":
		return "$b=[IO.File]::ReadAllBytes(" + path + ");" +
			"$p=[Text.Encoding]::UTF8.GetString([Security.Cryptography.ProtectedData]::Unprotect($b,$null,[Security.Cryptography.DataProtectionScope]::CurrentUser));" +
			"[Console]::Out.Write($p)", nil
	case "store":
		return "$v=[Console]::In.ReadToEnd();" +
			"$b=[Text.Encoding]::UTF8.GetBytes($v);" +
			"$p=[Security.Cryptography.ProtectedData]::Protect($b,$null,[Security.Cryptography.DataProtectionScope]::CurrentUser);" +
			"[IO.File]::WriteAllBytes(" + path + ",$p)", nil
	case "delete":
		return "if (Test-Path -LiteralPath " + path + ") { Remove-Item -LiteralPath " + path + " -Force }", nil
	}
	return "", fmt.Errorf("未知的 DPAPI 操作 %q", op)
}

// psQuote 用单引号字面量转义 PowerShell 字符串（' 翻倍）。
func psQuote(s string) (string, error) {
	if strings.ContainsAny(s, "\x00\n\r") {
		return "", errors.New("路径包含非法字符")
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

func (k dpapiKeyring) run(op string, stdin string) (string, error) {
	script, err := k.script(op)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if op == "load" && (strings.Contains(msg, "Cannot find path") || os.IsNotExist(errors.Unwrap(err))) {
			return msg, ErrKeyringEmpty
		}
		return msg, fmt.Errorf("安全存储操作失败：%w（%s）", err, msg)
	}
	return out.String(), nil
}

func (k dpapiKeyring) Load() ([]byte, error) {
	if _, err := os.Lstat(k.file); errors.Is(err, os.ErrNotExist) {
		return nil, ErrKeyringEmpty
	}
	out, err := k.run("load", "")
	if err != nil {
		if errors.Is(err, ErrKeyringEmpty) {
			return nil, err
		}
		return nil, err
	}
	return []byte(out), nil
}

func (k dpapiKeyring) Store(value []byte) error {
	if err := os.MkdirAll(filepath.Dir(k.file), 0o700); err != nil {
		return err
	}
	_, err := k.run("store", string(value))
	return err
}

func (k dpapiKeyring) Delete() error {
	_, err := k.run("delete", "")
	return err
}
