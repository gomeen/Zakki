package notes

// 同步凭据的系统安全存储抽象：macOS 走登录钥匙串（security CLI），
// Windows 走 DPAPI 用户级加密文件；载荷统一为 JSON envelope。令牌绝不
// 写入偏好、zakki.json、日志或 Git（R7）；测试注入内存 Keyring。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrKeyringEmpty 表示安全存储中没有同步凭据（未连接或已断开）。
var ErrKeyringEmpty = errors.New("安全存储中没有 GitHub 同步凭据")

// Keyring 抽象系统安全存储：实现只存取字节，不解释内容。
type Keyring interface {
	Load() ([]byte, error) // 无条目时返回 ErrKeyringEmpty
	Store(value []byte) error
	Delete() error
}

// KeyringService / KeyringAccount 是同步凭据在系统存储中的标识。
const (
	KeyringService = "Zakki Sync"
	KeyringAccount = "github"
)

// tokenEnvelope 是安全存储中的载荷；Version 便于将来轮换格式。
type tokenEnvelope struct {
	Version      int       `json:"version"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

// SaveSyncToken 把令牌集合写入系统安全存储。
func SaveSyncToken(k Keyring, tok TokenSet) error {
	if k == nil {
		return errors.New("安全存储不可用")
	}
	if tok.AccessToken == "" {
		return errors.New("访问令牌为空，拒绝保存")
	}
	raw, err := json.Marshal(tokenEnvelope{Version: 1, AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, ExpiresAt: tok.ExpiresAt})
	if err != nil {
		return err
	}
	return k.Store(raw)
}

// LoadSyncToken 从系统安全存储读取令牌集合。
func LoadSyncToken(k Keyring) (TokenSet, error) {
	if k == nil {
		return TokenSet{}, errors.New("安全存储不可用")
	}
	raw, err := k.Load()
	if err != nil {
		return TokenSet{}, err
	}
	var env tokenEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return TokenSet{}, fmt.Errorf("安全存储中的凭据无法解析：%w", err)
	}
	if env.AccessToken == "" {
		return TokenSet{}, ErrKeyringEmpty
	}
	return TokenSet{AccessToken: env.AccessToken, RefreshToken: env.RefreshToken, ExpiresAt: env.ExpiresAt}, nil
}

// ClearSyncToken 从系统安全存储删除同步凭据。
func ClearSyncToken(k Keyring) error {
	if k == nil {
		return errors.New("安全存储不可用")
	}
	return k.Delete()
}

// TokenProvider 返回同步轮使用的取令牌闭包：未过期直接返回访问令牌；
// 已过期且有 refresh token 则刷新一次并回存（轮换后的 refresh token
// 必须落安全存储）；刷新失败如实报错，由同步轮发布状态。
func TokenProvider(k Keyring, clientID string, flow *DeviceFlow, now func() time.Time) func(context.Context) (string, error) {
	return func(_ context.Context) (string, error) {
		tok, err := LoadSyncToken(k)
		if err != nil {
			return "", err
		}
		if now == nil {
			now = time.Now
		}
		if tok.ExpiresAt.IsZero() || now().Before(tok.ExpiresAt.Add(-time.Minute)) {
			return tok.AccessToken, nil
		}
		if tok.RefreshToken == "" {
			return "", errors.New("访问令牌已过期且无法刷新，请重新连接 GitHub")
		}
		fresh, err := flow.Refresh(context.Background(), clientID, tok.RefreshToken)
		if err != nil {
			return "", fmt.Errorf("刷新 GitHub 令牌失败（请重新连接）：%w", err)
		}
		if fresh.RefreshToken == "" {
			fresh.RefreshToken = tok.RefreshToken
		}
		if err := SaveSyncToken(k, fresh); err != nil {
			return "", fmt.Errorf("刷新后的令牌保存失败：%w", err)
		}
		return fresh.AccessToken, nil
	}
}
