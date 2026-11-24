# Zakki 发布与更新

## 当前方案

- 仓库与更新分发：公开仓库 `gomeen/Zakki` 的 GitHub Releases，无需额外服务器。
- 应用名与安装包：macOS 为 `Zakki.app` / DMG，Windows 为 `Zakki.exe` / Setup EXE。
  应用标识 `com.gomeen.zakki`，Go module 名 `zakki`；数据目录 macOS 为
  `~/Library/Application Support/Zakki/`，Windows 为 `%APPDATA%\Zakki\`，不改变 JSON 结构。
  应用按全新安装处理，不做旧数据目录迁移。
- 更新器：由 [`go.mod`](../go.mod) 锁定的 MyGo 提供的 `plugins/updater/native`，全原生 UI，无 webview；应用菜单与托盘
  均提供「检查更新…」，默认只手动检查。用户可查看版本说明、下载安装、跳过或稍后重启。
- 发布目标固定为 **darwin/universal**（Intel 与 Apple Silicon）和 **windows/amd64**（Windows 10/11 x64）。
  同一个 Release 分别提供 `update-darwin-universal.json` 与 `update-windows-amd64.json`，两种系统不会混用更新。
  每种架构独立订阅清单，后续不要随意切换目标，否则已有版本会收不到更新；目前不发布 Windows ARM64 原生包。
- Release 附件：两个安装包、两个签名更新归档、两个更新清单，以及从当前 Latest 到
  新版本的签名增量包（`*.delta`）；不生成 SHA256SUMS（完整性由清单内的 Ed25519
  签名保证，校验失败不写入应用）。`mygo.json` 的 `updates.deltas` 为 `1`：
  只为当前 Latest 生成到新版本的增量包，更早版本直接下载完整归档。应用优先使用匹配 delta，补丁缺失、
  无体积优势、校验失败或本地应用被修改时自动回退完整签名归档，兜底不能关闭。
- **没有 Apple 开发者账号**：`macos.signingIdentity` 为 `-`，仅 ad-hoc 签名，不做公证。
  Windows 尚无 Authenticode 代码签名，SmartScreen 可能显示未知发布者。
  两个平台共用 Ed25519 更新签名密钥；它验证更新包来源，不代替 Apple 或 Windows 的系统代码签名。

`.github/workflows/ci.yml` 检查分支与 PR；`release.yml` 在推送 `vX.Y.Z` tag 后检查、打包，
先全量检查，再分别在 macOS 与 Ubuntu runner 构建两个平台；**两个平台都成功**后，统一上传到
**草稿 Release**，不自动公开。任一平台失败时不执行上传 job，另一个成功平台的 Actions 附件仍可用于排查。
配置到位不代表已经发布：仍需设置 Secret、推送版本并公开草稿。

## 首次配置（只需一次）

### 1. 更新签名密钥

本项目的公钥已写入 `mygo.json`。对应私钥在配置时生成，保存在仓库外：

```text
~/Library/Application Support/mygo/update-keys/zakki/mygo-update.key
~/Library/Application Support/mygo/update-keys/zakki/mygo-update.pub
```

私钥权限为 `0600`。将私钥备份到密码管理器或其他安全位置；不要贴到聊天、提交到 Git、
上传到 Release，也不要在日志中输出。丢失私钥后不能给已安装版本发布可信更新；泄露则可能被用来伪造更新。
不要重新运行 keygen 替换现有密钥。

在完成 GitHub CLI 登录后，将**私钥文件内容**写入仓库 Actions Secret（不是文件路径）：

```sh
gh auth login
gh secret set MYGO_UPDATER_PRIVATE_KEY --repo gomeen/Zakki \
  < "$HOME/Library/Application Support/mygo/update-keys/zakki/mygo-update.key"
```

本地配置阶段不执行上述 GitHub 写入。发布工作流使用 GitHub 自带的 `github.token` 上传，
不需要额外 PAT，更不能把 Token 内置进应用。确认仓库允许 Actions 使用工作流声明的 `contents: write` 权限。
如果源码仓库改为私有，应另外提供公开发布仓库并同步 `updates.github`；当前更新器不携带私有仓库凭证。

### 2. 首次提交与远程

远程应为 `git@github.com:gomeen/Zakki.git`，分支为 `main`。
配置工作不会自动提交、打 tag 或推送；审核改动并确认工作区中的其他修改后，再自行提交推送。

## 每次发布

1. 修改 **`mygo.json` 的 `version`**（唯一版本来源），并为 `CHANGELOG.md` 增加 `## X.Y.Z` 章节。
   应用内更新说明来自该章节；文件存在但章节缺失时构建会失败。
2. 按下述工作流的规则校验根模块与 `core` 并运行 `make check`，同步 README 与行为变化说明。
3. 提交代码，确认待发布提交无遗漏，然后打同号 tag。以下命令属于主动发布操作，配置阶段不要执行：

   ```sh
   version="$(jq -r .version mygo.json)"
   git push origin main
   git tag "v$version"
   git push origin "v$version"
   ```

4. 查看 Actions 的 `release` 工作流。它会：
   - 核对 tag、版本、仓库与私钥 Secret，并要求新版本严格高于当前 Latest；缺私钥或版本倒退直接失败，不生成不完整的发行包。
   - 在根目录执行 `go mod verify`，只豁免本地目录替换（`replace zakki/core => ./core`）的已知 `missing ziphash` 误报；其余校验失败均拦截发布。随后执行 `go -C core mod verify` 和完整 `make check`，两者必须通过。
   - macOS runner 构建 universal 包，Ubuntu runner 安装 NSIS 并交叉构建 Windows x64 安装器。
   - 检查安装器、清单版本/URL、归档大小；额外核对 macOS 双架构与 ad-hoc 签名、Windows PE x64 可执行文件。
   - 读取当前 Latest 版本，要求两个平台的清单和附件均包含该版本到新版本的有效 delta，并核对签名字段与实际文件大小。
   - 保留两个平台的 Actions 附件；全部成功后汇总、再次校验，再上传至草稿 Release，两个更新清单最后上传。
   - 拒绝覆盖已公开的同号 Release；修复已公开版本必须递增版本号。
5. 从草稿下载测试，在另一台 Mac/干净账户与真实 Windows 10/11 x64 上检查安装、托盘、全局快捷键、中文输入法、保存与重启。
   交叉编译不等于完成 Windows 运行验收。首次接入更新器的版本需要手动安装；之后两个平台各做一次“已安装旧版 → 新版”的真实更新回归。
6. 确认草稿包含下列全部文件，在 GitHub 点击 **Publish release**，确保设为 **Latest**，不要设成 prerelease。
   公开后，用户的「检查更新」才会看到该版本。Release 网页说明可在草稿中编辑，应用内说明来自 CHANGELOG。

## 构建与产物

普通本地构建（不上传）：

```sh
make build            # macOS universal
make build-windows    # Windows x64
```

macOS 生成 Windows 安装器需先安装 NSIS（`brew install nsis`），Linux 为 `sudo apt-get install nsis`；
在 Windows 上运行 MyGo CLI，缺 NSIS 时会下载并校验其工具链。只需要原生 UI，无需 WebView2。
本地没有 NSIS 时 MyGo 会警告并跳过安装器，但仍可交叉生成 EXE 与签名更新文件；发布工作流强制要求安装器存在。

带更新签名的本地构建（同样不上传，Windows 将最后一行换成 `make build-windows`）：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(< "$HOME/Library/Application Support/mygo/update-keys/zakki/mygo-update.key")" \
  make build
```

`build/darwin-universal/` 中的文件：

| 文件 | 用途 |
|---|---|
| `Zakki.app` | 本地运行的应用包，不直接上传 Release |
| `zakki-<version>.dmg` | 用户首次安装或手动更新 |
| `zakki-<version>-darwin-universal.tar.gz` | 应用内更新所下载的签名归档 |
| `update-darwin-universal.json` | 版本、说明、URL、大小、签名等更新清单 |
| `*.delta`（有可用旧版本时） | 从上一 Latest 到新版本的增量更新，失败自动回退完整归档 |

`build/windows-amd64/` 中的文件：

| 文件 | 用途 |
|---|---|
| `Zakki.exe` 与资源文件 | 本地运行，Release 以安装器分发，不单独上传裸 EXE |
| `zakki-setup-<version>-amd64.exe` | 按用户安装器；默认安装到 `%LOCALAPPDATA%\Programs\Zakki`，无需管理员权限 |
| `zakki-<version>-windows-amd64.tar.gz` | Windows 应用内更新的签名归档，不含 Setup 与卸载器 |
| `update-windows-amd64.json` | Windows x64 的版本、说明、下载信息与签名 |
| `*.delta`（有可用旧版本时） | Windows 增量更新，失败自动回退完整归档 |

增量包以「当前 Latest → 新版本」为基线；应用优先使用匹配 delta，但补丁缺失、
无体积优势、校验失败或本地应用被修改时，会自动回退完整签名归档，这一安全兜底
不能关闭。下载与解包都以更新清单中的 Ed25519 签名校验，校验失败时放弃安装并提示。

MyGo 缺私钥时只警告并跳过 macOS/Windows 更新文件，所以本地普通构建仍可用，但不能拿其安装包当作完整的更新发布。
工作流会检查私钥与产物，公私钥不匹配时构建失败。生成 DMG 需要 macOS；Windows 可从 macOS/Linux 交叉构建，安装器需要 NSIS。

更新清单入口：

```text
https://github.com/gomeen/Zakki/releases/latest/download/update-darwin-universal.json
https://github.com/gomeen/Zakki/releases/latest/download/update-windows-amd64.json
```

只上传源码或打 tag、只有 DMG、草稿未公开、未设 Latest，均不能形成可用的正式更新。
当前配置不会检查 prerelease，也不要用正式渠道的 tag 测试预发布版本。

## 用户安装与更新

- **macOS 首次安装**：下载 DMG，把 `Zakki.app` 拖到 `/Applications` 或自己的 `~/Applications`。
  不能直接在只读 DMG 内自更新；安装目录必须可写。开发模式也不能自更新。
- **Windows 首次安装**：下载 `zakki-setup-<version>-amd64.exe`，按用户安装到默认目录。
  自定义目录也必须可写，不建议装入需要管理员权限的 Program Files。
  数据位于 `%APPDATA%\Zakki\`，不随应用更新替换；不要把数据目录选成程序安装目录（卸载器会删除安装目录）。
- **更早版本用户**：先保存并完整退出旧版，再单独备份旧版数据。Zakki 不会扫描或自动迁移更名前的旧数据目录；安装新版后应确认使用的是预期数据文件，且不要同时启动两版写同一份数据。
- **以后更新**：Zakki → 检查更新… → 查看说明 → 下载并安装 → 重启。
  更新替换应用包，不删除当前版本的笔记和偏好；重启经过退出最终保存，保存失败会取消退出并显示错误。
- **Windows 安全提示**：当前未配置 Authenticode，可能出现 SmartScreen/未知发布者提示；确认下载来源后再决定是否运行。
  以后可另行配置 Windows 代码签名证书，与 Apple 开发者账号及现有更新密钥独立。
- **网络或权限问题**：更新窗口显示失败或不可用；排查 GitHub 访问、安装目录写权限，必要时完整退出后重新运行新版安装器或覆盖应用。
- **macOS 安全提示**：当前未获 Developer ID 信任与公证，首次打开可能被 Gatekeeper 阻止。
  确认来源后通过系统「隐私与安全性」允许打开；不要全局关闭 Gatekeeper。
  更新签名不保证消除系统拦截，正式对外发布前仍建议办理开发者账号并补齐签名、公证。

## 发布验收

- [ ] `mygo.json` 与 tag 同号，CHANGELOG 有对应章节，根模块校验符合上述规则，且 `go -C core mod verify` 与 `make check` 通过。
- [ ] Secret 对应当前公钥，私钥有独立安全备份，Git 与附件中均无私钥。
- [ ] macOS 与 Windows 构建 job 均成功，草稿包含 DMG、Setup EXE、两份签名归档、两份更新清单与两平台的 delta，无多余附件。
- [ ] Intel / Apple Silicon 安装可启动；对真实下载文件验证 Gatekeeper 行为，不以本机无隔离标记的包代替。
- [ ] Windows 10/11 x64 按用户安装、启动、中文输入法、托盘、快捷键与退出通过实机验收，确认 SmartScreen 提示；不能仅凭交叉编译宣布已验证。
- [ ] 旧版检查更新可下载安装并重启；两个平台的清单均包含从上一 Latest 版本出发的 delta，附件存在且大小一致。
- [ ] 更新前后笔记与偏好保留；保存失败不静默重启。
- [ ] 公开 Release 并设为 Latest 后，清单和归档均可匿名下载。
