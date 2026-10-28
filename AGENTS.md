# AGENTS.md

Zakki：基于 [MyGo](https://github.com/egoist/mygo) 原生 UI 的 macOS/Windows Markdown 便签应用，纯 Go 单二进制，界面语言英文 + 简体中文（MyGo i18n）。

## 必读

1. [docs/engineering-standard.md](docs/engineering-standard.md)：工程与架构标准、布局与依赖规则。
2. [docs/architecture.md](docs/architecture.md)：各模块职责、线程模型、持久化生命周期。
3. [.agents/skills/mygo-maintenance/SKILL.md](.agents/skills/mygo-maintenance/SKILL.md)：改 UI 前必读。
4. [docs/configuration.md](docs/configuration.md)：改偏好/设置相关代码前必读。
5. [docs/release.md](docs/release.md)：改命名、发布或更新流程前必读。

## 布局

- 根目录：只有入口 `main.go`（调 `internal/app.Run()`）与工程化文件。
- `internal/app`：MyGo UI 与应用装配（窗口、菜单、托盘、视图、实时预览、标签、设置），唯一允许依赖 MyGo 的 internal 包。
- `internal/` 其余：纯逻辑与持久化（`markdown`、`codehl`、`notes`、`prefs`、`atomicfile`），禁止依赖 MyGo 与 `internal/app`。
- `locales/`：i18n 目录（`en.json` 源 + `zh-Hans.json`），`go tool mygo generate` 生成 `catalog_gen.go`；中英键位一致性有测试。改用户可见文案一律经 `locales.*`，不得写死字面量。
- `vendor/` + `patches/`：MyGo 原生区间样式补丁；改依赖前读 [patches/README.md](patches/README.md)，用 `make vendor` 刷新，不能用 `-mod=mod` 绕过补丁。
- `mobile/`：`mobile/binding` 独立 Go module（绑定构建，`require zakki/core` 并 `replace => ../../core`，唯一允许依赖 golang.org/x/mobile 的模块）+ `mobile/ios` iOS 工程；不参与桌面 `make check` 的 Go 构建与测试。

## 硬规则

- 纯逻辑包（`markdown`/`codehl`/`notes`/`prefs`/`atomicfile`/`fonts`）禁止 import `github.com/egoist/mygo` 及其子包，也禁止 import `internal/app`；internal 包之间只允许 [engineering-standard.md](docs/engineering-standard.md) 第 3 节列出的依赖。
- 禁止往 `app` 顶层新增字段；新状态放进对应的领域子结构体或 Store。
- 禁止把 `ui.Element`、`*ui.Context` 存入状态或传给后台；持久身份用 `ui.Handle`。
- 后台 goroutine 只碰不可变快照，回主线程只用 `Window.Update`；锁只存在于 Store 类型内部。
- 不改 `zakki.json`、`preferences.json` 的格式与迁移语义，除非任务明确要求（需递增版本并补迁移测试）。
- 持久化失败必须如实上报，不得吞掉错误。
- 测试绝不写真实用户目录：用 `newTestApp(t)` / `t.TempDir()` 和注入的时钟。
- 结构迁移不夹带行为变更；测试只能迁移，不能删除或放宽。
- 未经用户允许不做 lint 类修改。
- 文件迁移或职责变化时，在同一次提交里更新 `docs/architecture.md`。
- 自有非测试文件超过 800 行必须拆分（`vendor/` 保留上游布局）；禁止 `util`/`common`/`helpers` 包名。
- 注释用中文，只写代码本身表达不出的约束。

## 命令

```bash
make check          # 提交前必须全绿：fmt-check + vet（含 mygo vet）+ test + race + arch-check
make test           # CGO_ENABLED=0 go test ./... -count=1，含 MyGo vendor 补丁测试
make race           # go test -race ./... -count=1，含 MyGo vendor 补丁测试
make vet            # go vet ./... && go tool mygo vet .
make build          # go tool mygo build，产物在 build/
make vendor         # 重新生成依赖副本并应用 MyGo 补丁
go list -f '{{.Dir}}' github.com/egoist/mygo/ui   # 查看实际参与构建的 MyGo UI 源码
```

## 提交

- 中文 commitlint：`git commit -m "<type>: <描述>"`，type 取 `feat` / `fix` / `refactor` / `style` / `docs` / `chore` / `test` / `perf`。
- 一次提交只做一件事；纯结构迁移用 `refactor:`。
- 禁止提交 `build/`、`.mygo/`、根目录 `go build` 产生的二进制 / `build/Zakki*` 产物，以及更新签名私钥。

## Agent skills

### Issue tracker

Issue 追踪在 gomeen/Zakki 的 GitHub Issues（用 gh CLI 读写，PR 不作为 triage 请求面）。见 `docs/agents/issue-tracker.md`。

### Triage labels

使用 5 个默认 triage 标签：needs-triage、needs-info、ready-for-agent、ready-for-human、wontfix。见 `docs/agents/triage-labels.md`。

### Domain docs

单上下文布局：根目录 `GLOSSARY.md` + `docs/adr/`。见 `docs/agents/domain.md`。
