package notes

// 同步凭据的系统安全存储抽象：macOS 走登录钥匙串（security CLI），
// Windows 走 DPAPI 用户级加密文件；载荷统一为 JSON envelope。令牌绝不
// 写入偏好、zakki.json、日志或 Git（R7）；测试注入内存 Keyring。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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

// AIKeyringService / AIKeyringAccount 是 AI 助理 API 密钥在系统存储中的
// 标识；与同步凭据互不影响。
const (
	AIKeyringService = "Zakki AI"
	AIKeyringAccount = "apikey"
)

// SystemAIKeyring 返回 AI 密钥的系统安全存储实现。
func SystemAIKeyring(dataDir string) (Keyring, error) {
	return NewServiceKeyring(AIKeyringService, AIKeyringAccount, dataDir)
}

// aiKeyEnvelope 是 AI 密钥在安全存储中的载荷；Version 便于将来轮换格式。
type aiKeyEnvelope struct {
	Version int    `json:"version"`
	APIKey  string `json:"apiKey"`
}

// SaveAIKey 把 API 密钥写入系统安全存储；空密钥拒绝保存。
func SaveAIKey(k Keyring, key string) error {
	if k == nil {
		return errors.New("安全存储不可用")
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("API 密钥为空，拒绝保存")
	}
	raw, err := json.Marshal(aiKeyEnvelope{Version: 1, APIKey: key})
	if err != nil {
		return err
	}
	return k.Store(raw)
}

// LoadAIKey 从系统安全存储读取 API 密钥。
func LoadAIKey(k Keyring) (string, error) {
	if k == nil {
		return "", errors.New("安全存储不可用")
	}
	raw, err := k.Load()
	if err != nil {
		return "", err
	}
	var env aiKeyEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("安全存储中的 AI 密钥无法解析：%w", err)
	}
	if env.APIKey == "" {
		return "", ErrKeyringEmpty
	}
	return env.APIKey, nil
}

// ClearAIKey 从系统安全存储删除 AI 密钥。
func ClearAIKey(k Keyring) error {
	if k == nil {
		return errors.New("安全存储不可用")
	}
	return k.Delete()
}

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
// 刷新会轮换 refresh token，用旧令牌重刷必得 invalid_grant，所以
// 「读取 → 刷新 → 回存」整体串行；刷新请求跟随调用方 ctx，可被同步轮
// 取消或超时中止。
func TokenProvider(k Keyring, clientID string, flow *DeviceFlow, nowFn func() time.Time) func(context.Context) (string, error) {
	if nowFn == nil {
		nowFn = time.Now
	}
	var mu sync.Mutex
	return func(ctx context.Context) (string, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		mu.Lock()
		defer mu.Unlock()
		tok, err := LoadSyncToken(k)
		if err != nil {
			return "", err
		}
		if tok.ExpiresAt.IsZero() || nowFn().Before(tok.ExpiresAt.Add(-time.Minute)) {
			return tok.AccessToken, nil
		}
		if tok.RefreshToken == "" {
			return "", errors.New("访问令牌已过期且无法刷新，请重新连接 GitHub")
		}
		if flow == nil {
			return "", errors.New("授权流程不可用，请重新连接 GitHub")
		}
		fresh, err := flow.Refresh(ctx, clientID, tok.RefreshToken)
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
