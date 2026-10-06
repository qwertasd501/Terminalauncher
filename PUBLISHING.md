# 发布到 GitHub 的注意事项

这份 fork 是 [telecter/cmd-launcher](https://github.com/telecter/cmd-launcher)（MIT）的改动版。
下面按「发之前 → 发的时候 → 发之后」排。带 ⚠️ 的是会出事的那种。

## 一、发之前：绝不能进仓库的东西

⚠️ **最危险的一条：不要把 `D:\pcl\cmd-launcher\` 这个成品目录整个传上去。**
它里面有：

| 路径 | 里面是什么 | 后果 |
|---|---|---|
| `config\accounts.json` | 登录信息。现在是 `"type": "offline"`（离线账号，含 UUID 与角色名）；**一旦登录正版，这里会写入 `refresh_token`** | 泄漏账号，能被用来冒充登录 |
| `minecraft\` | assets / libraries / instances，几百 MB | 仓库爆掉，GitHub 单文件超 100 MB 直接拒收 |
| `cmd-launcher.exe` | UPX 加壳的二进制 | 二进制不该进源码仓库 |
| `portable.txt` / `启动.cmd` | 面向你这台机器的便携配置 | 别人 clone 下来会被误导 |

要发的是**源码目录** `F:\cmd-launcher-main`，不是部署目录。

`F:\cmd-launcher-main` 在本次已加 `.gitignore`，覆盖了上面这些以及构建产物、`*.bak` 之类。
**提交前务必跑一次 `git status`，逐行看一眼有没有意外文件。**

另外：真正的账号数据从来没进过源码目录（配置在 `D:\pcl\cmd-launcher\config\`），
所以这个仓库目前是干净的 —— 但**换台机器、换目录继续开发时同样要小心**。

## 二、署名与许可（MIT 的硬要求）

- `LICENSE` 是 **MIT 双版权**：`2024-2025 telecter`（原程序）+ `2026 qwertasd501`（本 fork 的改动）。
  ⚠️ **这条上游版权声明不能删、不能只写自己名字** —— MIT 明确要求「保留版权声明」，
  抹掉上游署名就不是合规 fork 了。
- ⚠️ **分发二进制时必须带上 `LICENSE`。** 便携包里已经放了（`cmd-launcher-portable.zip` 含
  `LICENSE`），**GitHub Release 的附件里也要带一份**。只传 exe 不传 LICENSE 是违反 MIT 的。
- README 顶部的 fork 声明（第 12 行起）和结尾的 License 段落已经写清楚了，保留。
- 程序内 `about` 已补 fork 署名（2026-10-06）：六语 `launcher.copyright` 现为
  「Copyright 2024-2025 telecter; fork maintained by qwertasd501」，与 LICENSE / README 一致。
- `docs/icon.png` 已换成自绘图标（终端 T + 光标 + >），不再使用上游图标；`docs/icon.ico`
  经 `rsrc_windows_amd64.syso` 嵌入 Windows exe。注意：`.syso` 文件名带 `-windows_amd64`，
  **只对 windows/amd64 构建生效**（CI 的 linux/macOS 产物没有图标，属正常）。换图标时：
  重生成 `docs/icon.ico` 后重跑
  `go run github.com/akavel/rsrc@latest -ico docs/icon.ico -arch amd64 -o rsrc_windows_amd64.syso`。
- 仓库名已定 `Terminalauncher`，与上游 `cmd-launcher` 天然区分。

## ⚠️ 二点五、便携包 zip 里含你的账号文件，不能直接传 Release

`cmd-launcher-portable.zip` 是**本机使用**的包，里面有 `config\accounts.json`（当前是离线账号，
但登录正版后会含 `refresh_token`）。**传 GitHub Release 前必须先去掉它**。
已生成干净的发布包：`cmd-launcher-portable-release.zip`（同内容但不含 accounts.json，7 项）。
自检方法：打包后 `python -c "import zipfile;print(zipfile.ZipFile('xxx.zip').namelist())"`
逐行看一遍再上传。

## 三、README 里指向上游的链接（发到自己的仓库后就是错的）

已处理。仓库定为 **`qwertasd501/Terminalauncher`**，两份 README 里指向上游的 6 处全部改完：

- `README.md`：build badge、go-mod badge 换成新仓库；`pkg.go.dev` badge **删掉**（它跟随 Go
  module 路径，而 module 路径没改，留着会指向上游文档）；`nightly.link` 与 `git clone` 换新仓库；
  `go install` 小节改写为「本 fork 不提供 go install，请用 Release 二进制或自行构建」。
- `README_de.md`（上游那份德语 README）：badge 与 `git clone` 同样换掉，`go install` 段落改写。

仍然出现 `telecter` 的 4 处是**故意保留**的：README 顶部的 fork 来源声明、结尾 License 段的
两处署名、以及 go install 说明里提到的 module 路径。以后改动别顺手把它们也换掉。

## 四、`go.mod` 的 module 路径（一个需要先定的事）

`go.mod` 第 1 行是 `module github.com/telecter/cmd-launcher`，和上游一致。

- **保留不动**：编译、`go test` 全都正常，代价是 `go install <你的仓库>@latest` 不成立，
  README 里那条 `go install` 得删掉，只留「Release 下二进制」和「clone 后自己 build」两种方式。
- **改成 `github.com/<你的用户名>/<仓库名>`**：`go install` 才好用。代价是**全仓库 import 都要改**
  （70 个 `.go` 文件里的 `github.com/telecter/cmd-launcher/...` 前缀），改完必须重跑一遍测试。

建议先不动（省事、不影响任何人用 Release 的二进制），需要 `go install` 时再改。

## 五、仓库怎么建

- **用 GitHub 的 Fork 功能**（推荐）：会显示「forked from telecter/cmd-launcher」，
  来源清楚，方便日后 `git remote add upstream` 同步上游改动。
- **新建独立仓库**：更自由（能改默认分支名、能设私有），但要在 README 里把来源写清楚
  （已经有了）。
- 无论哪种，本地 `F:\cmd-launcher-main` 已 `git init -b main`（2026-10-06 完成，**尚未提交**）。
  下一步：`git add .` → `git status` 确认干净 → 首次提交 →
  `git remote add origin https://github.com/qwertasd501/Terminalauncher` →
  `git push -u origin main`。
  本机 `git` 不在 PATH，在 `C:\Users\zhuyi\.workbuddy\binaries\PortableGit\versions\1.2.0\cmd`。

## 六、二进制与 Release

`.github/workflows/release.yml` 会在你**创建 Release 时自动构建 4 个平台**
（linux amd64/arm64、windows amd64、darwin arm64）并上传，不用自己编译。

注意两点：

- 它构建的是**未加壳**的原始二进制，**不含** `启动.cmd`、`README.txt`、`LICENSE`。
  你要发的那个便携包（UPX 瘦身 + 中文说明 + LICENSE）需要**手动作为附件上传**。
- ⚠️ **UPX 加壳的 exe 容易被杀软 / 浏览器报毒**（已知问题）。建议 Release 里**同时提供
  未加壳版本**，或在说明里注明「如报毒可用 `upx -d` 还原 / 下载未加壳版」。
- `.github/workflows/build.yml` 用的是 `go-version: stable`；`go.mod` 写的是 `go 1.25.5`，
  当前 stable 满足。日后若 stable 落后于 `go.mod` 的版本，CI 会失败。

## 七、网络与凭据（国内环境）

- 从 2021 年起 GitHub 不再接受账号密码推送，https 方式要先用 **Personal Access Token**。
- 直接 `git push` 到 GitHub 在国内通常超时，需要走代理（在 Git 里配
  `git config --global http.proxy` 或让代理软件接管）。
- 仓库体积：源码约 2 万行 Go，很快；**但历史里一旦提交过大文件就很难彻底移除**，
  所以第一提交之前的 `git status` 检查是最省事的时机。

## 八、发之后的维护

- 同步上游：`git remote add upstream https://github.com/telecter/cmd-launcher` →
  `git fetch upstream` → 合并。改动集中在 `internal/cli/output/lang_*.go`（六语词条）
  和 `internal/cli/`，上游若动了同一片区域会有冲突，词条文件最容易冲突。
- 版本号写在 `internal/cli/cli.go:25`，目前还是上游的 `1.6.1`。发自己的 Release 时建议
  标成 `1.6.1-zh.1` 之类，避免和上游版本号混淆。
- 建议加 `.gitattributes` 固定换行（`.go`/`.md` 用 LF，`.cmd`/`.bat` 用 CRLF），
  免得 Windows 上 clone 下来整个仓库都是 CRLF 差异。

## 快速自查（发之前逐条过）

- [x] `git status` 里没有 `config/`、`minecraft/`、`*.exe`、`*.bak`、`portable.txt`
      （2026-10-06 验证：只有 12 项源码，7 个危险项全部被 .gitignore 拦住）
- [ ] `LICENSE` 双版权完整，没有被改成只剩自己
- [ ] Release 附件里带了 `LICENSE`
- [x] README 的 badge / `go install` / `clone` 地址已不再是上游的
- [x] 仓库名 `Terminalauncher` 与上游 `cmd-launcher` 不同，README 第一屏有 fork 声明
- [ ] `go test ./... -timeout 900s` 全绿（含真下载集成测试）
