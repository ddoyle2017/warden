<div align="center">
  <img 
    alt="Warden"
    src="./images/warden-logo.png"/>
</div>

<h3 align="center">
	A CLI mod mod manager for Valheim
</h3>

## Overview
![overview-header](./images/mislands-gjall.jpeg)

Warden features a simple command-line interface for managing mods on a Valheim dedicated server, hosted on Linux (Windows support coming soon<sup>TM</sup>!).

Mods are currently sourced from [Thunderstore.io](https://thunderstore.io/), with added support for [Nexus Mods](https://www.nexusmods.com/) planned for the near future<sup>TM</sup>. Warden also automatically resolves dependencies for mod installs and updates, including [BepInEx](https://github.com/BepInEx/BepInEx).

Warden stores data in 2 different files:
- A YAML configuration file at `$HOME/.warden.yaml`.
- A lightweight, database storage file at `$HOME/.warden.db`

The YAML file stores the following configuration values for the app:
- `valheim-directory` - Where the Valheim dedicated server is installed. By default, Warden uses the default location [SteamCMD](https://developer.valvesoftware.com/wiki/SteamCMD) installs Valheim servers into.
- `mod-directory` - Where mods (also called 'plugins') are installed. This is expected to be a child folder of `valheim-directory`. By default, Warden uses `/BepinEx/plugins` which is the folder that BepInEx loads mods from when the server is started.

The DB file stores metadata about each mod managed by the app, including things like: author, version, where its installed, etc..

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
- `remove`
    - Removes the targetted mod
    - `all`
        - A sub-command for removing *every* installed mod. A clean slate :)
- `config`
    - Lists the current configuration values for Warden + where the config file is located
    - `get`
        - Fetch a specific configuration value
    - `set`
        - Update a configuration value

## Installation
![installation-banner](./images/mistlands-exploration.png)
Proper install process coming soon <sup>TM</sup>.

## License

The Warden project is licensed under an [MIT License](./LICENSE)