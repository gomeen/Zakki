//go:build tools

package binding

// 依赖锚点：go mod tidy 会清掉没有任何导入的模块，这里用一个只对 tools
// 构建标签可见的文件同时保住两类依赖——
//   - zakki/core：gomobile bind 的目标包 zakki/core/zakkicore 所在模块；
//   - golang.org/x/mobile：gomobile/gobind 工具链（版本即模块版本）。
//
// 本文件不参与任何构建输出，也不进入桌面 make check 的构建图
// （mobile/binding 是独立 module，根模块的 ./... 不会匹配进来）。
import (
	_ "golang.org/x/mobile/bind"

	_ "zakki/core/zakkicore"
)
