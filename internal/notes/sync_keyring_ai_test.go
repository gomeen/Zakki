package notes

// AI 密钥存取：与同步凭据同一 Keyring 抽象、不同的 service 条目，互不影响。

import (
	"errors"
	"testing"
)

func TestAIKeySaveLoadClear(t *testing.T) {
	k := &memKeyring{}
	if _, err := LoadAIKey(k); !errors.Is(err, ErrKeyringEmpty) {
		t.Fatalf("空存储应报 ErrKeyringEmpty，got %v", err)
	}
	if err := SaveAIKey(k, "  "); err == nil {
		t.Fatal("空白密钥应拒绝保存")
	}
	if err := SaveAIKey(k, "sk-ai-123"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAIKey(k)
	if err != nil || got != "sk-ai-123" {
		t.Fatalf("读回 got %q err %v", got, err)
	}
	if err := ClearAIKey(k); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAIKey(k); !errors.Is(err, ErrKeyringEmpty) {
		t.Fatalf("清除后应报 ErrKeyringEmpty，got %v", err)
	}
}

func TestAIKeyNilKeyring(t *testing.T) {
	if _, err := LoadAIKey(nil); err == nil {
		t.Fatal("nil 安全存储应报错")
	}
	if err := SaveAIKey(nil, "k"); err == nil {
		t.Fatal("nil 安全存储应报错")
	}
	if err := ClearAIKey(nil); err == nil {
		t.Fatal("nil 安全存储应报错")
	}
}
