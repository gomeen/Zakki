# 性能历史基线与复现工具

这些数据来自旧版本，不代表当前性能。结论与场景说明见归档报告 [reports/2026-10-18-performance-review.md](../reports/2026-10-18-performance-review.md)
与 [reports/2026-10-18-startup-shutdown-review.md](../reports/2026-10-18-startup-shutdown-review.md)。

| 文件 | 用途 |
|---|---|
| `summary.json` | 源码高亮、预览帧、保存争用、撤销堆等微基准的中位数与环境信息 |
| `native-lifecycle-probe.py` | 临时 bundle 原生生命周期探针（阶段打点、正文校验） |
| `summarize-native-lifecycle.py` | 从 JSONL 重算分场景中位数（输入文件需本地保留或重新采集） |

原生探针依赖已移除的 `internal/app/startup_jobs.go`。要复现当时的实验，请在独立工作树打开
`8d1fc3edfb3dc7dd1df696fcf0632b98a31863c6`（`b3cc495` 的父提交），使用该版本的脚本采集样本。
原始 JSONL 未保留在当前分支；汇总脚本需要另备相应版本的样本。

当前微基准见 `internal/app/*_bench_test.go` 与 `internal/notes/save_performance_test.go`。
当前 macOS 原生窗口生命周期回归见 [`TestNativeDockReopen`](../../internal/app/lifecycle_native_test.go)：

```bash
ZAKKI_NATIVE_TEST=1 MYGO_LOCALE=zh-Hans go test ./internal/app -run '^TestNativeDockReopen$' -count=1 -v
```

该测试不提供当前签名包的 Metal 冷启动性能数据。
