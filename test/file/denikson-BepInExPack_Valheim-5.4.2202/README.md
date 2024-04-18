![BepInEx logo](https://avatars2.githubusercontent.com/u/39589027?s=256)

# BepInExPack for Valheim

This is [BepInEx 5.4.22](https://github.com/BepInEx/BepInEx) pack pre-configured for Valheim. It is maintained by Azumatt, Vapok, and Margmas in the Valheim community. If changes to the pack need to be made but BepInEx itself doesn't need to update typically we will increment the version for Thunderstore. Example: `5.4.2201`, this does not mean that BepInEx itself needed to update, likely it's a small change for the game or something needed to change in the description etc.

BepInEx is a general purpose framework for Unity modding.
BepInEx includes tools and libraries to

* load custom code (hereafter *plugins*) into the game on launch;
* patch in-game methods, classes and even entire assemblies without touching original game files;
* configure plugins and log game to desired outputs like console or file;
* manage plugin dependencies.

BepInEx is currently [one of the most popular modding tools for Unity on GitHub](https://github.com/topics/modding?o=desc&s=stars).

### This pack's contents

This pack is preconfigured and usable for Valheim modding.  
In particular, the changes from base BepInEx releases are:

* Added preconfigured `BepInEx.cfg` with console enabled.
* Added scripts necessary to run both game and dedicated server on Linux machines

### Installation (game, automated)

This is the recommended way to install BepInEx on the game.

1. Download and install [Thunderstore Mod Manager](https://www.overwolf.com/app/Thunderstore-Thunderstore_Mod_Manager) or [r2modman](https://valheim.thunderstore.io/package/ebkr/r2modman/)
2. Click **Install with Mod Manager** button on top of the page
3. Run the game via the mod manager

### Installation (server, automated)

There are some managed dedicated server that support simple and automatic BepInEx installation.  
Here is a list of known ones (list updated as more is known).  
*Note: Some of the links may be affiliate links*

[![ValheimServerHosting](https://i.imgur.com/w21YSPf.jpg)](https://valheimserverhosting.com/?via=bepinex)

[![XGamingServer](https://i.imgur.com/sRUEoy0.png)](https://link.xgamingserver.com/BepInEx)


### Installation (manual)

If you are installing this manually, do the following

1. Extract the archive into a folder. **Do not extract into the game folder.**
2. Move the contents of `BepInExPack_Valheim` folder into `<Steam Location>\steamapps\common\Valheim`.
3. Check that you have installed it correctly. 
    If done correctly, your folder will look as follows

    ![BepInEx, winhttp.dll, and doorstop_config.ini in Valheim folder.](https://i.imgur.com/SVi9BFQ.png)
4. Follow either Windows or Linux game running instructions below:

#### Configuration (Windows)

No need to configure. Simply run the game. If everything is correct, you will see a console pop up.

#### Configuration (Linux, game)

1. Make `start_game_bepinex.sh` executable with `chmod u+x start_game_bepinex.sh`.
2. In Steam, go to game's properties and set game's launch arguments to
    
	```
	./start_game_bepinex.sh %command%
	```
	
	Note to advanced users: You can change `./start_game_bepinex.sh` to full path to the script. This way you can install this pack entirely outside of game folder.
3. Run the game via Steam.

At this moment you will not see any clear indication if BepInEx works.
It is suggested to test by installing a simple plugin such as [MessageCenter from BepInEx.Utility](https://github.com/BepInEx/BepInEx.Utility).

#### Configuration (Linux, server)

This guide assumes you have enough knowledge working with Linux.  
If not, it is suggested you use a server host that supports it out of the box.

1. Make `start_server_bepinex.sh` executable with `chmod u+x start_server_bepinex.sh`.
2. Edit `start_server_bepinex.sh` to change the launch parameter like you would with Valheim's own launch script.
3. Run `start_server_bepinex.sh` to start the server.

If done correctly, you will see BepInEx bootstrap messages in the terminal.


### Checking BepInEx version and load status

If BepInEx was loaded successfully into the game, you should see BepInEx version info in top left corner of the main menu:

![BepInEx version in top-left corner of main menu](https://i.imgur.com/Yha5IkQ.png)

### Useful links

* [BepInEx: writing basic plugin walkthrough](https://docs.bepinex.dev/articles/dev_guide/plugin_tutorial/index.html)
* [BepInEx: useful plugins for modding](https://docs.bepinex.dev/articles/dev_guide/dev_tools.html)
* [BepInEx: patching game methods at runtime](https://docs.bepinex.dev/articles/dev_guide/runtime_patching.html)

## Issues, questions, etc.
**DO NOT ASK FOR HELP in the _Official BepInEx Discord_**

The Valheim community runs a customized version of BepInEx and the official BepInEx folks are unable to help.

If you are having problems with BepInEx in general, mods running on Valheim, or unsure between the two, please seek assistance from ANY of the Discords listed below.

### Current BepInEx for Valheim Maintainers:
* [Azumatt](https://discord.gg/pdHgy6Bsng)
* [Margmas](https://discord.gg/DdUt6g7gyA)
* [Vapok](https://discord.gg/zUa3efgPxm)
