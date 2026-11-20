//go:build !darwin && !windows

// 其他平台暂无安全存储实现：云同步在这些平台明确不可用并如实上报，
// 绝不回退到明文文件（R7）。

package notes

import "errors"

// SystemKeyring 在无实现的平台返回明确错误。
func SystemKeyring(dataDir string) (Keyring, error) {
	return nil, errors.New("当前平台暂无同步凭据安全存储实现，云同步不可用")
}

// NewServiceKeyring 在无实现的平台同样返回明确错误（含 AI 密钥）。
func NewServiceKeyring(service, account, dataDir string) (Keyring, error) {
	return nil, errors.New("当前平台暂无凭据安全存储实现")
}
