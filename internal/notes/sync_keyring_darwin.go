//go:build darwin

// macOS：经 security CLI 存取登录钥匙串。-w 传参对同机进程可见是已知
// 取舍（令牌不出本机）；security CLI 本身不写日志，命令输出不含机密。

package notes

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// keychainKeyring 用登录钥匙串的通用密码项存取凭据字节。
type keychainKeyring struct {
	service string
	account string
}

// SystemKeyring 返回 macOS 钥匙串实现；dataDir 未使用（接口统一）。
func SystemKeyring(dataDir string) (Keyring, error) {
	if _, err := exec.LookPath("security"); err != nil {
		return nil, errors.New("未找到 security 命令，无法访问钥匙串")
	}
	return keychainKeyring{service: KeyringService, account: KeyringAccount}, nil
}

func (k keychainKeyring) run(args ...string) (string, error) {
	cmd := exec.Command("security", args...)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		// security 以退出码 44 表示「条目不存在」；其余原样上抛。
		if strings.Contains(out.String(), "could not be found") || strings.Contains(out.String(), "SecKeychainSearch") {
			return out.String(), ErrKeyringEmpty
		}
		return out.String(), fmt.Errorf("security %s: %w（%s）", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

func (k keychainKeyring) Load() ([]byte, error) {
	out, err := k.run("find-generic-password", "-s", k.service, "-a", k.account, "-w")
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(out, "\n")), nil
}

func (k keychainKeyring) Store(value []byte) error {
	_, err := k.run("add-generic-password", "-U", "-s", k.service, "-a", k.account, "-w", string(value))
	return err
}

func (k keychainKeyring) Delete() error {
	_, err := k.run("delete-generic-password", "-s", k.service, "-a", k.account)
	if errors.Is(err, ErrKeyringEmpty) {
		return nil // 已不存在视为删除成功
	}
	return err
}
