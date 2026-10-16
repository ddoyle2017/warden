<div align="center">
  <img 
    alt="Warden"
    src="./images/warden-logo.png"/>
</div>

<h3 align="center">
	A CLI mod manager for Valheim servers
</h3>

## Overview
![overview-header](./images/mislands-gjall.jpeg)

Warden features a simple command-line interface for managing mods on a Valheim dedicated server. Linux is the primary target. macOS and Windows paths exist in the code, but complete support is not yet established.

Mods are currently sourced from [Thunderstore.io](https://thunderstore.io/). [Nexus Mods](https://www.nexusmods.com/) is not implemented. Warden installs the latest releases of direct mod dependencies and prompts to install [BepInEx](https://github.com/BepInEx/BepInEx) when its installation record is missing. Dependency versions are not pinned and dependency resolution is not recursive; see the [installation lifecycle](docs/install-lifecycle.md) for ordering and limitations.

Warden stores data in 2 different files:
- A YAML configuration file at `$HOME/.warden.yaml`.
- A lightweight, database storage file at `$HOME/.warden.db`

The YAML file stores the following configuration values for the app:
- `valheim-directory` - Where the Valheim dedicated server is installed. Warden selects a platform-specific Steam installation path by default; set this to the actual server directory before managing mods.
- `platform` - The operating system recorded when configuration is created (`linux`, `darwin`, or `windows`). This selects the vanilla server script.

Plugins are installed under `BepInEx/plugins` inside `valheim-directory`; there is no separate configurable `mod-directory`. `config set` currently accepts only `valheim-directory`.

The DB file stores metadata about managed mods and frameworks, including author, version, and installation details. Metadata and files can diverge after a partial failure; `list` does not inspect the installed files.

Warden was built with:
- [Go](https://github.com/golang/go) - Everyone's favorite open-source programming language
- [Cobra](https://github.com/spf13/cobra) - CLI library for Go
- [Viper](https://github.com/spf13/viper) - Configuration library for Go
- [SQLite](https://www.sqlite.org/) - Lightweight SQL database engine

## Usage
![usage-banner](./images/ashlands-drakkar.png)

### :bangbang: CLI USAGE SUBJECT TO CHANGE FOR NOW :bangbang:

Warden supports the following commands:
- `list`
    - Prints a list of all installed mods
- `add`
    - Downloads and installs the specified mod
- `update`
    - Updates the mod to latest version
    - `all`
        - A sub-command for updating *all* installed mods
    - `bepinex`
        - Updates BepInEx while preserving existing plugins and `BepInEx/config` files
- `remove`
    - Removes the targetted mod
    - `all`
        - A sub-command for removing *every* installed mod. A clean slate :)
    - `bepinex`
        - Removes BepInEx and plugin files; currently leaves mod records in the database
- `config`
    - Lists the current configuration values for Warden + where the config file is located
    - `get`
        - Fetch a specific configuration value
    - `set`
        - Update `valheim-directory`
- `start`
    - Runs the server startup script for `vanilla` or `modded` from `valheim-directory`, so relative executables and resources resolve inside the server directory

Use an absolute `valheim-directory` to start the same server from any working directory. A relative setting is resolved against the directory where you invoke Warden. `start` waits for the script to exit before returning its combined output; it does not detach the server.

Example syntax, after setting the intended server directory:

```sh
warden config set valheim-directory /absolute/path/to/valheim
warden add --namespace Azumatt --mod Where_You_At
warden update --mod Where_You_At
warden remove --namespace Azumatt --mod Where_You_At
warden update all
warden update bepinex
warden start modded
```

These are real installation commands, not a smoke test. Back up an existing server before changing its mods. Even `--help` initializes Warden's configuration and database; use the [isolated CLI procedure](CONTRIBUTING.md#isolated-cli-smoke-check) for development.

## Installation
![installation-banner](./images/mistlands-exploration.png)
Proper install process coming soon <sup>TM</sup>.

For a local source build, follow [Contributing](CONTRIBUTING.md#prerequisites), then run `go build -o warden .`. The SQLite driver requires CGO and a C compiler. Run the local binary as `./warden`, or place it on your `PATH` to use the example commands as written.

## Contributing

See [Contributing](CONTRIBUTING.md) for setup, checks, and safe local execution.
Agent guidance starts in [the repository instructions](.github/copilot-instructions.md), with path-specific coding rules and reusable command/lifecycle skills.

## License

The Warden project is licensed under an [MIT License](./LICENSE)