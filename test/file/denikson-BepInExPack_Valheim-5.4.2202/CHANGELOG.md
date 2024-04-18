## Changelog

#### 5.4.2202
* ReadMe Updated to remove links to BepInEx Discord
* Preloader version incremented.
* Update made to Preloader to ensure assembly's are being patched correctly. 
  * This should remove the warning message that shows when BepInEx starts.
* **Modders:** Unstripped Corlibs are still removed. If you are having issues with System depenencies, please include those dependencies directly into your project.

#### 5.4.2201
* As of Valheim 0.217.24, There is no longer a need for the unstripped corlibs to be shipped with BepInEx.
  * This version removes the corlibs and instructs doorstop not to include them.
  * For modders, this is important, as you'll want to make sure you are referencing Unity from the Game Folder now.

<details>
<summary><b>Changelog History</b> (<i>click to expand</i>)</summary>

#### 5.4.22
* Update for Valheim 0.217.22 and upgrade BepInEx to 5.4.22

#### 5.4.2105
* Updating Thunderstore version in Preloader.DLL

#### 5.4.2104
* For Unix installs, including dedicated servers, the ForceBepInExTTYDriver config setting was removed in a prior update. This is needed for servers to shut down correctly.

#### 5.4.2103
* Log the BepInExPack Valheim version the user is using right before the preloader completes. This is to help troubleshoot issues by stating what version of the pack you are running.

#### 5.4.2102
* Updated to force the Assembly entry point by default. This should prevent some issues with users having older config files.

#### 5.4.2101
* Updated to support Valheim 0.214.3

#### 5.4.2100
* Updated to BepInEx 5.4.21

#### 5.4.1902
* Updated to support Valheim 0.214.2

#### 5.4.1901
* Updated to support Valheim 0.209.5

#### 5.4.1900
* Updated to BepInEx 5.4.19 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.19))

#### 5.4.1700
* Updated to BepInEx 5.4.17 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.17))

#### 5.4.1601
* Updated unstripped DLLs for Unity 2019.4.31

#### 5.4.1600
* Updated to BepInEx 5.4.16 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.16))

#### 5.4.1502
* Adjusted `start_game_bepinex.sh` to handle cmdline args better

#### 5.4.1501
* Updated Valheim.DisplayBepInExInfo to 2.0.0([changelog](https://github.com/Valheim-Modding/Valheim.DisplayBepInExInfo/releases/tag/v2.0.0))

#### 5.4.1500
* Updated to BepInEx 5.4.15 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.15))

#### 5.4.1400

* Updated to BepInEx 5.4.14 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.14))
* Updated *nix start script for games to account for new Steam game bootstrapper

#### 5.4.1100

* Updated to BepInEx 5.4.11 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.11))

#### 5.4.1001

* Updated unstripped DLLs for Unity 2019.4.24

#### 5.4.1000

* Updated to BepInEx 5.4.10 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.10))
* Updated Valheim.DisplayBepInExInfo to 1.1.0 ([changelog](https://github.com/Valheim-Modding/Valheim.DisplayBepInExInfo/releases/tag/v1.1.0))

#### 5.4.901

* Updated README with some dedicated servers that support BepInEx by default

#### 5.4.900

* Updated to BepInEx 5.4.9 ([changelog](https://github.com/BepInEx/BepInEx/releases/tag/v5.4.9))
* Updated Valheim.DisplayBepInExInfo to 1.0.1 ([changelog](https://github.com/Valheim-Modding/Valheim.DisplayBepInExInfo/releases))
* Set `PreventClose` to `true` by default. This prevents console from being closed (and thus unsaved game being closed by accident)

#### 5.4.800

* Updated to BepInEx 5.4.8
* Added [Valheim.DisplayBepInExInfo](https://github.com/Valheim-Modding/Valheim.DisplayBepInExInfo) plugin

#### 5.4.701

* Updated screenshot of example installation

#### 5.4.700

* Updated to BepInEx 5.4.7

#### 5.4.603

* Updated BepInEx 5.4.6 to a newer build
* Added `--enable-console true|false` command-line option to enable or disable BepInEx console
* Added `--doorstop-dll-search-override` command-line option to behave the same way as config's `dllSearchPathOverride` option

#### 5.4.602

* Updated BepInEx 5.4.6 to a newer build
* Update config to write Unity logs to LogOutput.log by default
* Added preconfigured scripts and files to run the game under Linux

#### 5.4.601

* Updated unstripped DLLs for Unity 2019.4.20

#### 5.4.600

* Adjusted README
* Adjusted versioning to account for inter-version changes

#### 5.4.6

* Initial release with BepInEx 5.4.6

</details>