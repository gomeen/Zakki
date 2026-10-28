// Package binding 只为 gomobile bind 存在：本模块的职责是持有 go.mod——
// 以 replace 引入同仓的 zakki/core，并用 tool directive 固定
// golang.org/x/mobile 工具链版本。绑定目标是 zakki/core/zakkicore，
// 本包不出现在任何绑定产物里。
package binding
