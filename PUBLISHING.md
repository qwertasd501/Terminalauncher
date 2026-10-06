# 发布到 GitHub 的注意事项

这份 fork 已改名为 **Terminalauncher**，源出 [telecter/cmd-launcher](https://github.com/telecter/cmd-launcher)（MIT）。
仓库：https://github.com/qwertasd501/Terminalauncher 。下面按「发之前 → 发的时候 → 发之后」排，带 ⚠️ 的是会出事的那种。

## 一、发之前：绝不能进仓库的东西

⚠️ **最危险的一条：不要把 `D:\pcl\cmd-launcher\` 这个成品目录整个传上去。**
它里面有：

| 路径 | 里面是什么 | 后果 |
|---|---|---|
| `config\accounts.json` | 登录信息。现在是 `"type": "offline"`（离线账号，含 UUID 与角色名）；**一旦登录正版，这里会写入 `refresh_token`** | 泄漏账号，能被用来冒充登录 |
| `minecraft\` | assets / libraries / instances，几百 MB | 仓库爆掉，GitHub 单文件超 100 MB 直接拒收 |
| `Terminalauncher.exe` | UPX 加壳的二进制 | 二进制不该进源码仓库 |
| `portable.txt` / `启动.cmd` | 面向你这台机器的便携配置 | 别人 clone 下来会被误导 |

要发的是**源码目录** `F:\cmd-launcher-main`，不是部署目录。
该目录的 `.gitignore` 已覆盖上面这些以及构建产物、`*.bak` 之类。
**提交前务必跑一次 `git status --short`，逐行看一眼有没有意外文件。**

## 二、署名与许可（MIT 的硬要求）

- `LICENSE` 是 **MIT 双版权**：`2024-2025 telecter`（原程序）+ `2026 qwertasd501`（本 fork 的改动）。
  ⚠️ **这条上游版权声明不能删、不能只写自己名字** —— MIT 明确要求「保留版权声明」。
- ⚠️ **分发二进制时必须带上 `LICENSE`。** 便携 zip 与 GitHub Release 附件里都要有。
- README 顶部的 fork 声明、结尾 License 段落保留（提到上游 `cmd-launcher` 属**故意保留**）。
- 程序内 `about` 六语 `launcher.copyright` = 「Copyright 2024-2025 telecter; fork maintained by
  qwertasd501」，`launcher.fork` = 「Terminalauncher 是 cmd-launcher 的修改版分支…」。
- `docs/icon.png` / `docs/icon.ico` 为自绘图标，经 `rsrc_windows_amd64.syso` 嵌入 Windows exe。
  ⚠️ `.syso` 文件名带 `-windows_amd64`，**只对 windows/amd64 构建生效**，CI 的 linux/macOS 产物无图标。
  换图标：重生成 `docs/icon.ico` 后重跑
  `go run github.com/akavel/rsrc@latest -ico docs/icon.ico -arch amd64 -o rsrc_windows_amd64.syso`。

## ⚠️ 三、便携包 zip 里含账号文件，不能直接传 Release

本机用的 `cmd-launcher-portable.zip` 里有 `config\accounts.json`（登录正版后会含 `refresh_token`）。
**传 Release 前必须先去掉它**，已生成干净的发布包（不含 accounts.json）。
自检：`python -c "import zipfile;print(zipfile.ZipFile('xxx.zip').namelist())"` 逐行看一遍再上传。

## 四、改名记录（2026-10-06 完成）

上游名 `cmd-launcher` → 本 fork 名 **`Terminalauncher`**，全仓库已改：

| 位置 | 旧 | 新 |
|---|---|---|
| `go.mod` module | `github.com/telecter/cmd-launcher` | `github.com/qwertasd501/Terminalauncher` |
| 全部 `.go` import（47 个文件 112 处） | 同上 | 同上 |
| `internal/cli/cli.go` | `name = "cmd-launcher"`, `version = "1.6.1"` | `name = "Terminalauncher"`, `version = "1.0.0"` |
| shell 提示符 | `cmd-launcher [inst]_[acc]>` | `Terminalauncher [inst]_[acc]>` |
| `pkg/env.go` 配置目录 | `%APPDATA%\cmd-launcher` | `%APPDATA%\Terminalauncher`（旧目录仍会读一次做迁移） |
| `pkg/env.go` 便携环境变量 | `CMD_LAUNCHER_HOME` | `TERMINAL_LAUNCHER_HOME`（旧变量名仍兼容） |
| Modrinth User-Agent | `telecter/cmd-launcher …` | `qwertasd501/Terminalauncher …` |
| README / README_de / docs/API.md | `cmd-launcher` | `Terminalauncher`（上游链接与 fork 声明除外） |

**仍然出现 `telecter` / `cmd-launcher` 的地方是故意保留的**：`LICENSE` 的上游版权行、README 的 fork
来源声明与 License 段、六语 `launcher.fork` 词条里的「cmd-launcher 的修改版分支」。以后改动别顺手换掉。

## 五、二进制与 Release

`.github/workflows/release.yml` 在你**创建 Release 时**自动构建 4 个平台
（linux amd64/arm64、windows amd64、darwin arm64）并上传，另用 `upx-ucl` 打出
`Terminalauncher-windows-amd64-portable.zip`（含 exe + LICENSE + README.md）。

注意：

- ⚠️ `release` 事件跑的 workflow **取 tag 所在提交的版本**，所以**改完 workflow 要打新 tag 才生效**。
- CI 的 `gh release upload --clobber` 会**覆盖同名附件**；本地包与 CI 包同名时会被换掉。
- ⚠️ UPX 加壳的 exe 容易被杀软 / 浏览器误报。Release 说明里已注明「报毒可下载未加壳版本或 `upx -d` 还原」。
- 本地发版（可选）：`upx --best --lzma` 后打 zip；发版脚本见
  `~/.workbuddy/skills/github-push-release-behind-watt-mitm/scripts/gh_release.py`。
- `.github/workflows/build.yml` 用 `go-version: stable`；`go.mod` 写 `go 1.25.5`，当前 stable 满足。
  **CI 不跑 `go test`**：测试会真下载整份 Minecraft（300s+），不适合 CI。

## 六、网络与凭据（国内环境）

- 本机 hosts 把 github 指向 127.0.0.1（Watt Toolkit / Steam++ 的 GitHub 加速做 TLS 中转），
  git 必须走 **openssl 后端 + 合并 CA 包**：
  `http.sslBackend=openssl`、`http.sslCAInfo=C:/Users/zhuyi/.workbuddy/watt-mitm-ca-bundle.crt`。
  schannel 会卡 `CRYPT_E_NO_REVOCATION_CHECK`，且 `http.schannelCheckRevoke=false` 对它无效。
- 凭据用 `credential.helper=manager`（Windows 凭据管理器里的 `gho_` token 可用
  `git credential fill` 取出给 API 用，**不要落盘**，用完删临时文件）。
- 仓库体积：源码约 2 万行 Go；**历史里一旦提交过大文件就很难彻底移除**，首次提交前的检查最省事。

## 七、发之后的维护

- 同步上游：`git remote add upstream https://github.com/telecter/cmd-launcher` → `git fetch upstream` → 合并。
  改动集中在 `internal/cli/output/lang_*.go` 与 `internal/cli/`，词条文件最容易冲突。
- 版本号在 `internal/cli/cli.go:25`（现为 `1.0.0`）。本 fork 版本线与上游解耦，不用再跟 `1.6.x`。
- 建议加 `.gitattributes` 固定换行（`.go`/`.md` 用 LF，`.cmd`/`.bat` 用 CRLF），
  免得 Windows 上 clone 下来整个仓库都是 CRLF 差异。

## 快速自查（发之前逐条过）

- [x] `git status` 里没有 `config/`、`minecraft/`、`*.exe`、`*.bak`、`portable.txt`
- [x] `LICENSE` 双版权完整，没有被改成只剩自己
- [x] Release 附件里带了 `LICENSE`（便携 zip 内含）
- [x] README 的 badge / `go install` / `clone` 地址都已指向本仓库
- [x] 全仓库改名完成（模块路径、程序名、版本 1.0.0、环境变量、六语词条）
- [ ] `go test ./... -timeout 900s` 全绿（含真下载集成测试）
