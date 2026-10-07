<img src="docs/icon.png" width="180">

# Terminalauncher

A minimal command line Minecraft launcher
EN | [ZH](README_zh.md)

[![Build](https://github.com/qwertasd501/Terminalauncher/actions/workflows/build.yml/badge.svg)](https://github.com/qwertasd501/Terminalauncher/actions/workflows/build.yml)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/qwertasd501/Terminalauncher)

> This repository is a modified fork of [telecter/cmd-launcher](https://github.com/telecter/cmd-launcher),
> which is MIT licensed. The original copyright notice is kept in [LICENSE](LICENSE), next to the one
> for the changes made here. See [License](#license) below.

- [Installation](#installation)
  - [Binaries](#binaries)
  - [Via go install](#via-go-install)
  - [Building from source](#building-from-source)
- [What this fork adds](#what-this-fork-adds)
- [Usage](#usage)

This project use AI

[API Documentation](docs/API.md)

## Installation

### Binaries
You can download prebuilt binaries from the Releases tab here on GitHub.  
For Windows, `Terminalauncher-windows-amd64-portable.zip` is the portable package: unzip it
anywhere and run `Terminalauncher.exe`, nothing is installed and no `PATH` entries are created.  
Builds of the latest commit are available at [nightly.link](https://nightly.link/qwertasd501/Terminalauncher/workflows/build/main).

### Via go install

```sh
go install github.com/qwertasd501/Terminalauncher@latest
```

### Building from source

1. Clone the repository: `git clone https://github.com/qwertasd501/Terminalauncher`
2. In the source directory, run `go run .` to compile and run the launcher.
3. Once you are ready, compile the executable with `go build .`

## What this fork adds

- **Content manager** : mods, resourcepacks, shaderpacks, datapacks and modpacks, with
  browsing and installation from Modrinth and a batch-update command. Reachable both globally and
  from a version's settings page.
- **Six languages**: English, German, Chinese, French, Russian and Spanish. Switch at runtime with
  `settings language <code>`; the system locale is used when nothing is configured.
- **Guided `download version` wizard**, which asks only for what is missing and names instances the
  way PCL does, e.g. `1.20.1-Forge_47.4.16-LTSC`.
- **Instances are complete the moment they are created** - libraries, assets and the Java runtime
  are fetched up front, so `start` does not stall on a first-run download.
- **Faster downloads**: concurrent transfers (`settings download_threads`, default 16) with retries
  on `429`/`5xx` and checksum mismatches.
- **`clear memory`**: trims the launcher's working set back to the page file.
- **Portable mode**: a `portable.txt` file (or `--portable`) next to the executable keeps all data
  in the launcher folder, so it can be copied to a USB stick.

## Usage

Use the `--help` flag to get the usage information of any command.

There are some commads

| Command | Description |
|---|---|
| `list versions` | List all instances |
| `list mods` | List the mods, resource packs, shader packs or data packs of an instance |
| `list modpacks` | List the instances installed from a modpack |
| `select versions` | Select an instance to operate on |
| `select users` | Select the active account with the arrow keys |
| `select java` | Select the Java executable with the arrow keys |
| `select mods` | Select one of them with the arrow keys, to operate on it |
| `deselect` | Clear the current instance selection |
| `list users` | List all accounts |
| `create users` | Add an account |
| `delete users` \| `del users` | Remove an account |
| `download version` \| `create instance` | Create an instance (PCL calls this downloading a version); with no arguments the options are asked one by one, and a name that is left out is built from the version and loader |
| `download mods` | Search Modrinth and install what you pick |
| `download modpacks` | Install a modpack from Modrinth, or from a local `.mrpack` file |
| `enable mods` | Switch a disabled mod or pack back on |
| `disable mods` | Switch a mod or pack off without deleting it |
| `delete mods` | Delete a mod or pack |
| `rename mods` | Rename a mod or pack |
| `import mods` | Copy mods or packs from a file or folder into the instance |
| `update mods` | Look for newer versions on Modrinth and install them |
| `search mods` | Search Modrinth and list what matches, without installing |
| `set versions` | Edit the settings of an instance |
| `set global` | Edit the settings shared by every instance |
| `settings` \| `options` \| `prefs` | Change the launcher-wide settings, including the language |
| `info` | Show information about the current selection |
| `info mods` | Show everything known about one mod or pack |
| `open` | Open one of an instance's folders |
| `cd` | Change the game directory |
| `pwd` | Print the game directory |
| `delete version` | Delete an instance |
| `rename version` | Rename an instance |
| `start` \| `launch` | Start the specified instance |
| `search` | Search versions |
| `auth login` | Log in to an account |
| `auth logout` | Log out of an account |
| `about` | Display launcher version and about |
| `help` \| `?` | Show this help |
| `clear` \| `cls` | Clear the screen; with `memory` the pages the running programs are not using are moved to the page file |
| `exit` \| `quit` | Leave the shell |

Use `help <command>` to show the full syntax of a command.

## License

MIT. The full text is in [LICENSE](LICENSE).

The original `cmd-launcher` was written by [telecter](https://github.com/telecter/cmd-launcher), and
Terminalauncher is a fork of it. Both notices are kept in the license file:

- `Copyright (c) 2024-2025 telecter` covers the original program.
- `Copyright (c) 2026 qwertasd501` covers the changes made in this fork.

Redistributing a binary of this build therefore means shipping the `LICENSE` file with it, which is
what the portable package does.

This software is not an official Minecraft product, is not approved by or associated with Mojang or Microsoft.
