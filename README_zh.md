<img src="docs/icon.png" width="180">

# Terminalauncher

一个极简的命令行 Minecraft 启动器 
[EN](README.md) | ZH

[![Build](https://github.com/qwertasd501/Terminalauncher/actions/workflows/build.yml/badge.svg)](https://github.com/qwertasd501/Terminalauncher/actions/workflows/build.yml)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/qwertasd501/Terminalauncher)

> 本仓库是 [telecter/cmd-launcher](https://github.com/telecter/cmd-launcher) 的修改版分支（fork），
> 采用 MIT 许可。原始的版权声明保留在 [LICENSE](LICENSE) 中，与本分支的改动版权声明并列。
> 详见下方 [许可证](#许可证)。

- [安装](#安装)
  - [预编译二进制](#预编译二进制)
  - [通过 go install](#通过-go-install)
  - [从源码构建](#从源码构建)
- [本分支新增了什么](#本分支新增了什么)
- [使用](#使用)

[API 文档](docs/API.md)

此项目使用AI

## 安装

### 预编译二进制

可以从 GitHub 这里的 Releases 标签页下载预编译的二进制文件。  
Windows 平台下，`Terminalauncher-windows-amd64-portable.zip` 是便携包：解压到任意位置后运行
`Terminalauncher.exe` 即可，不会安装任何东西，也不会写入任何 `PATH` 项。  
最新一次提交的构建可在 [nightly.link](https://nightly.link/qwertasd501/Terminalauncher/workflows/build/main) 获取。

### 通过 go install

```sh
go install github.com/qwertasd501/Terminalauncher@latest
```

### 从源码构建

1. 克隆仓库：`git clone https://github.com/qwertasd501/Terminalauncher`
2. 在源码目录下运行 `go run .` 编译并启动启动器。
3. 准备就绪后，用 `go build .` 编译出可执行文件。

## 本分支新增了什么

- **内容管理器**：模组、资源包、光影包、数据包与整合包，支持从 Modrinth 浏览安装，
  并提供批量更新命令。既可在全局使用，也能从某个版本的设置页进入。
- **六种语言**：英语、德语、中文、法语、俄语、西班牙语。运行时用 `settings language <代码>` 切换；
  未配置时跟随系统区域。
- **引导式 `download version` 向导**：只询问缺失的项，并像 PCL 那样为实例命名，
  例如 `1.20.1-Forge_47.4.16-LTSC`。
- **实例在创建的那一刻就是完整的** —— 库文件、资源与 Java 运行时会预先下载好，
  因此 `start` 不会卡在首次运行的下载上。
- **更快的下载**：并发传输（`settings download_threads`，默认 16），并对 `429`/`5xx` 与校验和
  不匹配自动重试。
- **`clear memory`**：把启动器的工作集（working set）压缩回页面文件。
- **便携模式**：可执行文件同目录下放一个 `portable.txt`（或传 `--portable`），所有数据都留在
  启动器文件夹内，因此可以整个复制到 U 盘里带走。

## 使用

使用 `--help` 参数可以查看任意命令的用法说明。



| 命令 | 说明 |
|---|---|
| `list versions` | 列出所有实例 |
| `list mods` | 列出某个实例的模组、资源包、光影包或数据包 |
| `list modpacks` | 列出通过整合包安装的实例 |
| `select versions` | 选择一个要操作的实例 |
| `select users` | 使用方向键选择当前账户 |
| `select java` | 使用方向键选择 Java 可执行文件 |
| `select mods` | 使用方向键选择其中一个，以便对其进行操作 |
| `deselect` | 清除当前实例选择 |
| `list users` | 列出所有账户 |
| `create users` | 添加一个账户 |
| `delete users` \| `del users` | 删除一个账户 |
| `download version` \| `create instance` | 创建一个实例（PCL 称之为下载版本）；不带参数时会逐项询问选项，未填写的名称会根据版本和加载器自动生成 |
| `download mods` | 搜索 Modrinth 并安装你选择的内容 |
| `download modpacks` | 从 Modrinth 或本地 `.mrpack` 文件安装整合包 |
| `enable mods` | 重新启用已禁用的模组或资源包 |
| `disable mods` | 在不删除的情况下禁用模组或资源包 |
| `delete mods` | 删除模组或资源包 |
| `rename mods` | 重命名模组或资源包 |
| `import mods` | 从文件或文件夹复制模组或资源包到实例中 |
| `update mods` | 在 Modrinth 上查找更新版本并安装 |
| `search mods` | 搜索 Modrinth 并列出匹配结果，但不安装 |
| `set versions` | 编辑某个实例的设置 |
| `set global` | 编辑所有实例共享的设置 |
| `settings` \| `options` \| `prefs` | 更改启动器全局设置，包括语言 |
| `info` | 显示当前选择的信息 |
| `info mods` | 显示某个模组或资源包的全部已知信息 |
| `open` | 打开实例的某个文件夹 |
| `cd` | 切换游戏目录 |
| `pwd` | 打印游戏目录 |
| `delete version` | 删除一个实例 |
| `rename version` | 重命名一个实例 |
| `start` \| `launch` | 启动指定的实例 |
| `search` | 搜索版本 |
| `auth login` | 登录账户 |
| `auth logout` | 退出账户 |
| `about` | 显示启动器版本及关于信息 |
| `help` \| `?` | 显示此帮助 |
| `clear` \| `cls` | 清屏；带 `memory` 时会将正在运行的程序未使用的页面移到页面文件中 |
| `exit` \| `quit` | 离开 shell |

提示: 用 'help <命令>' 查看命令的完整语法。

## 许可证

MIT。完整文本见 [LICENSE](LICENSE)。

原始的 `cmd-launcher` 由 [telecter](https://github.com/telecter/cmd-launcher) 编写，Terminalauncher
是它的一个分支。两份版权声明都保留在许可文件里：

- `Copyright (c) 2024-2025 telecter` 覆盖原始程序。
- `Copyright (c) 2026 qwertasd501` 覆盖本分支的改动。

因此，重新分发本构建的二进制文件意味着要随附 `LICENSE` 文件——这正是便携包的做法。


本软件非minecraft官方产品,未经mojang或microsoft批准,不与mojang及microsoft关联
