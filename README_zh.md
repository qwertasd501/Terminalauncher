<img src="docs/icon.png" width="180">

# Terminalauncher

一个极简的命令行 Minecraft 启动器，内置 PCL 风格的内容管理器。  
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
  - [实例](#实例)
  - [启动游戏](#启动游戏)
  - [内容管理器](#内容管理器)
  - [设置](#设置)
  - [账号认证](#账号认证)
  - [实例配置](#实例配置)
  - [搜索](#搜索)

[API 文档](docs/API.md)

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

- **内容管理器**（PCL 风格）：模组、资源包、光影包、数据包与整合包，支持从 Modrinth 浏览安装，
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

### 实例

**创建实例**  
使用 `inst create` 命令来创建新实例。  
可以用 `--loader, -l` 参数指定模组加载器，支持 Forge、NeoForge、Fabric 和 Quilt。如果想指定某个
加载器版本，用 `--loader-version` 参数；否则会选择最新可用的版本。

用 `--version, -v` 参数指定游戏版本。如果不提供值，则使用最新的正式版。也可以填 `release` 或
`snapshot` 来表示两者各自的最新版。

启动游戏时，启动器会尝试从 Mojang 下载一个 Java 运行环境。如果找不到合适的，你需要在实例配置里
手动指定一个。

```sh
Terminalauncher inst create -v 1.21.8 -l fabric CoolInstance
```

版本会创建在标准目录 `<游戏目录>/versions/<名称>` 下，也就是 PCL 和官方启动器存放版本的地方；
它的游戏数据也随版本一起保留（版本隔离默认开启，可从版本自身的设置里修改）。旧版构建创建在
`<游戏目录>/instances/<名称>` 下的版本，仍然能被正常发现、启动和删除。

**删除实例**  
如果要删除实例，使用 `inst delete` 命令，后面跟上实例名称。

### 启动游戏

要启动 Minecraft，直接运行 `start` 命令并跟上你想启动的实例名称即可。

```bash
Terminalauncher start CoolInstance
```

要设置游戏选项并覆盖实例配置，可以在 `start` 命令上加具体参数，这些参数可在帮助文本里查看。

**日志详细程度**  
要提高启动器的日志详细程度，使用 `--verbosity` 参数。它可以是：

- `info` —— 默认，无额外日志
- `extra` —— 启动游戏时输出更多信息
- `debug` —— 用于调试启动器的调试信息

### 内容管理器

在 shell 内，`mods`、`resourcepacks`、`shaderpacks`、`datapacks` 和 `modpacks` 会打开当前实例的
选择器，可从中从 Modrinth 添加条目、批量更新或移除。同一组界面也能从 `versions` → 某个版本 →
内容管理器进入。

```bash
Terminalauncher mods          # 当前选中实例的内容
Terminalauncher versions      # 先选一个版本，再管理它的内容
```

### 设置

启动器全局选项存放在游戏目录旁边，既可在命令行里改，也能在 shell 里改：

```bash
Terminalauncher settings                    # 列出当前值
Terminalauncher settings language zh        # 切换界面语言
Terminalauncher settings download_threads 32
```

### 账号认证

如果想以在线模式游戏，需要添加一个微软账号。

使用 `auth login` 命令来完成。作为微软 OAuth2 流程的一部分，会打开默认网页浏览器完成认证；
也可加 `--no-browser` 参数避免打开浏览器。只要存在账号，启动器就会自动尝试以在线模式启动游戏。

想以离线模式游戏，只需在 `start` 命令上传入 `-u, --username <用户名>` 参数设置用户名，游戏就会
自动以离线模式启动。

可以用 `auth logout` 命令登出。

### 实例配置

要修改某个实例的配置值，进入实例目录并打开 `instance.toml` 文件。

可配置的值有：

- 游戏版本
- 模组加载器及版本（非原版时）
- 窗口分辨率
- Java 可执行文件路径（留空则下载 Mojang 提供的 Java 运行环境）
- 自定义 JAR 路径（取代正常下载的客户端 JAR）
- 额外的 Java 参数
- 最小与最大内存

如前所述，这些值也可以用命令行参数覆盖。

**`instance.toml` 文件示例**

```toml
game_version = '1.21.8'
mod_loader = 'fabric'
mod_loader_version = '0.16.14'

[config]
# Java 可执行文件路径。留空则会下载 Mojang 提供的 JVM。
java = '/usr/bin/java'
# 传给 JVM 的额外参数
java_args = ''
# 自定义 JAR 路径，取代正常的 Minecraft 客户端 JAR
custom_jar = ''
# 最小游戏内存，单位 MB
min_memory = 512
# 最大游戏内存，单位 MB
max_memory = 4096

# 游戏窗口分辨率
[config.resolution]
width = 1708
height = 960

```

### 搜索

`search` 命令可以搜索 Minecraft 或模组加载器的版本。默认搜索游戏版本，也可以用来搜索 Fabric、
Quilt 和 Forge 版本。

```bash
Terminalauncher search [<查询>] [--kind {versions, fabric, quilt, forge}]
```

## 许可证

MIT。完整文本见 [LICENSE](LICENSE)。

原始的 `cmd-launcher` 由 [telecter](https://github.com/telecter/cmd-launcher) 编写，Terminalauncher
是它的一个分支。两份版权声明都保留在许可文件里：

- `Copyright (c) 2024-2025 telecter` 覆盖原始程序。
- `Copyright (c) 2026 qwertasd501` 覆盖本分支的改动。

因此，重新分发本构建的二进制文件意味着要随附 `LICENSE` 文件——这正是便携包的做法。

本软件非minecraft官方产品,未经mojang或microsoft批准,不与mojang及microsoft关联
