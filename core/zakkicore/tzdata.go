package zakkicore

// 移动端（gomobile）产物运行在无系统时区库的环境：iOS 容器内没有
// /usr/share/zoneinfo，Go time 包找不到 IANA 时区，SetLocation 会全部
// 报 unknown time zone（真机首启闪退的根因，模拟器借用宿主系统库不复现）。
// 空导入 time/tzdata 把时区库嵌入二进制（约 +450KB）；桌面端系统库可用时
// Go 优先系统库，不受影响。
import _ "time/tzdata"
