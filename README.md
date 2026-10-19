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

Mods are currently sourced from [Thunderstore.io](https://thunderstore.io/). [Nexus Mods](https://www.nexusmods.com/) is not implemented. Warden fetches the latest release when installing a direct mod dependency and prompts to install [BepInEx](https://github.com/BepInEx/BepInEx) when its installation record is missing. Dependency versions are not pinned and dependency resolution is not recursive; see the [installation lifecycle](docs/install-lifecycle.md) for ordering and limitations.

Warden stores data in 2 different files:
- A YAML configuration file at `$HOME/.warden.yaml`.
- A lightweight, database storage file at `$HOME/.warden.db`

When no configuration file exists, Warden saves the defaults before initializing the database or running a command. If saving fails, startup stops and reports the underlying write error. Existing valid configuration files are loaded without rewriting them.

The YAML file stores the following configuration values for the app:
- `valheim-directory` - Where the Valheim dedicated server is installed. Warden selects a platform-specific Steam installation path by default; on Linux, it resolves `$HOME/.steam/SteamApps/common/Valheim dedicated server` to an absolute path before saving a new configuration. Set this to the actual server directory before managing mods.
- `platform` - The operating system recorded when configuration is created (`linux`, `darwin`, or `windows`). This selects the default server directory when `valheim-directory` is missing, and the vanilla server script.

Plugins are installed under `BepInEx/plugins` inside `valheim-directory`; there is no separate configurable `mod-directory`. `config set` currently accepts only `valheim-directory`.

Explicitly configured paths are used as written, without `~` or environment-variable expansion. If an older configuration contains a literal `~`, use `warden config set valheim-directory "/absolute/path/to/Valheim dedicated server"` to replace it.

If an existing configuration supplies an unknown or empty `platform` and omits `valheim-directory`, startup stops because no default directory can be selected. Edit `$HOME/.warden.yaml` directly to correct `platform` or set `valheim-directory` to the server's path; `config set` cannot run until configuration loading succeeds.

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
    - Repeating the same add can [finish missing direct dependencies](docs/install-lifecycle.md#retrying-an-add) without reinstalling the root or updating recorded dependencies
- `update`
    - Updates the mod when a newer release is available; see the [version policy](docs/install-lifecycle.md#release-version-ordering)
    - `all`
        - A sub-command for updating *all* installed mods
    - `bepinex`
        - Updates BepInEx while preserving existing plugins and `BepInEx/config` files
- `remove`
    - Removes the targetted mod
    - `all`
        - A sub-command for removing *every* installed mod. A clean slate :)
    - `bepinex`
        - Removes BepInEx, its plugin files, and matching installation records
- `config`
    - Lists the current configuration values for Warden + where the config file is located
    - `get`
        - Fetch a specific configuration value
    - `set`
        - Update `valheim-directory`
- `start`
    - Runs the server startup script for `vanilla` or `modded`

Example syntax, after setting the intended server directory:

```sh
warden config set valheim-directory /absolute/path/to/valheim
warden add --namespace Azumatt --mod Where_You_At
warden update --namespace Azumatt --mod Where_You_At
warden remove --namespace Azumatt --mod Where_You_At
warden update all
warden update bepinex
warden start modded
```

`add` and `remove` require both `--namespace` and `--mod`. For `update`,
`warden update --mod Where_You_At` still works when exactly one installed
record has that name. If multiple installed mods share a name, Warden stops
before downloading or changing files and lists the matching `namespace/name`
choices; specify `--namespace` to select the intended author's package.
Duplicate records for one exact identity are reported separately and cannot
be resolved by namespace qualification; see the
[identity and recovery guidance](docs/install-lifecycle.md#package-release-and-framework-identity).

These are real installation commands, not a smoke test. Back up an existing server before changing its mods. Even `--help` initializes Warden's configuration and database; use the [isolated CLI procedure](CONTRIBUTING.md#isolated-cli-smoke-check) for development.

Confirmation prompts allow two answers. A deliberate negative answer cancels the
operation without a success message. Declining BepInEx installation also stops
the requested mod add before it fetches or installs the mod. An unchanged version
is reported as unchanged, not as a completed update. End-of-input and input
failures remain errors; see [confirmation behavior and limitations](docs/install-lifecycle.md#confirmations-and-backups).

Installation, update, and removal requests can complete with cleanup warnings.
A completed BepInEx bootstrap warning does not stop the requested mod add, but
does not establish that the mod itself was installed. An incomplete request
instead reports failure details, including recovery paths or database-verification
guidance; see [recovery and cleanup outcomes](docs/install-lifecycle.md#confirmations-and-backups)
before retrying or using leftover backup files.

Commands exit with status `0` on success, deliberate cancellation, known
no-change or already-installed outcomes, and completed requests with cleanup
warnings. Warnings still need attention even when the exit status is `0`.
Runtime failures and rejected input exit with status `1` and report the failure
once on standard error. Runtime failures do not print command usage. Cobra flag
and argument validation errors, such as missing required flags or an invalid
game type, include usage. An unknown configuration key is reported without usage.

A nonzero status does not mean that no files or records changed. In particular,
a successful BepInEx bootstrap does not turn a later mod failure into success.
Keep the reported recovery paths and database-verification guidance when
investigating an incomplete request.

## Installation
![installation-banner](./images/mistlands-exploration.png)
Proper install process coming soon <sup>TM</sup>.

For a local source build, follow [Contributing](CONTRIBUTING.md#prerequisites), then run `go build -o warden .`. The SQLite driver requires CGO and a C compiler. Run the local binary as `./warden`, or place it on your `PATH` to use the example commands as written.

## Contributing

See [Contributing](CONTRIBUTING.md) for setup, checks, and safe local execution.
Agent guidance starts in [the repository instructions](.github/copilot-instructions.md), with path-specific coding rules and reusable command/lifecycle skills.

## License

The Warden project is licensed under an [MIT License](./LICENSE)