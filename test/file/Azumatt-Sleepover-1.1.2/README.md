# Description

## Remake of Sleepover by kinghfb, with minimal changes.

`Client Only mod` - This mod is a remake of the original Sleepover mod by kinghfb. I have made minimal changes to the
mod, and I will be maintaining it from now on until a kind soul takes it from me.
I used my modding template to remake the mod so that it is easier to maintain and update in the future. Just took
patches from the original mod and applied them to my template.

### Original Links:

Original author of Sleepover: [kinghfb on Thunderstore](https://thunderstore.io/c/valheim/p/kinghfb/)

Original author code: [Sleepover on GitHub](https://github.com/timneill/ValheimMods/tree/main/Skald/Sleepover)

Original mod link: [Sleepover on Thunderstore](https://thunderstore.io/c/valheim/p/kinghfb/Sleepover/)

### Current Code:

Current author code: [Sleepover on GitHub](https://github.com/AzumattDev/Sleepover)

Having issues? Need help? Join my [Discord](https://discord.gg/pdHgy6Bsng) or report a bug on
GitHub [here](https://github.com/AzumattDev/Sleepover/issues)

---

This mod adds toggleable options for each of the checks for bed use/sleeping, as well as a couple of small useful
additions such as ignoring spawnpoint creation and allowing people to bunk together.

Ability to disable/enable:

* Wet check
* Fire check
* Walls/Roof check
* Enemies nearby
* Sleep without setting spawnpoint
* Sleep in a bed that isn't yours
* Sleep in a bed without claiming it
* Sleep any time of day
* Let multiple simultaneous users in one bed (not well-tested)

Known issues/quirks
-------------------

* Sharing a bed does not make them cuddle side-by-side. They will be attached to the bed at the same location, resulting
  in some clipping.
* Each connected client (and the server) needs to have the mod installed for the "sleep anytime" component to work. If
  this is causing problems, simply disable it in the mod.
* This mod is incompatible/untested with other mods that significantly alter bed and/or sleep behaviour.

Config
------

| Config name                | Description                                                           | Values   | Default |
|----------------------------|-----------------------------------------------------------------------|----------|---------|
| `ModEnabled`               | Whether or not the game should enable extra bed functions.            | `Toggle` | `On`    |
| `EnableMultipleBedfellows` | Allow multiple people per bed.                                        | `Toggle` | `On`    |
| `SleepAnyTime`             | Sleep at any time of day (May be buggy. Disable if there are issues.) | `Toggle` | `On`    |
| `IgnoreEnemies`            | Ignore nearby enemies when sleeping.                                  | `Toggle` | `On`    |
| `IgnoreExposure`           | Ignore roof and wall requirements for beds. Sleep under the stars!    | `Toggle` | `On`    |
| `IgnoreFire`               | Ignore fire requirement before sleeping.                              | `Toggle` | `On`    |
| `IgnoreWet`                | Ignore wet status when sleeping.                                      | `Toggle` | `On`    |
| `SleepWithoutSpawnpoint`   | Sleep without first setting a spawnpoint.                             | `Toggle` | `On`    |
| `SleepWithoutClaiming`     | Sleep without first claiming a bed.                                   | `Toggle` | `On`    |

<details>
<summary><b>Installation Instructions</b></summary>

***You must have BepInEx installed correctly! I can not stress this enough.***

### Manual Installation

`Note: (Manual installation is likely how you have to do this on a server, make sure BepInEx is installed on the server correctly)`

1. **Download the latest release of BepInEx.**
2. **Extract the contents of the zip file to your game's root folder.**
3. **Download the latest release of Sleepover from Thunderstore.io.**
4. **Extract the contents of the zip file to the `BepInEx/plugins` folder.**
5. **Launch the game.**

### Installation through r2modman or Thunderstore Mod Manager

1. **Install [r2modman](https://valheim.thunderstore.io/package/ebkr/r2modman/)
   or [Thunderstore Mod Manager](https://www.overwolf.com/app/Thunderstore-Thunderstore_Mod_Manager).**

   > For r2modman, you can also install it through the Thunderstore site.
   ![](https://i.imgur.com/s4X4rEs.png "r2modman Download")

   > For Thunderstore Mod Manager, you can also install it through the Overwolf app store
   ![](https://i.imgur.com/HQLZFp4.png "Thunderstore Mod Manager Download")
2. **Open the Mod Manager and search for "Sleepover" under the Online
   tab. `Note: You can also search for "Azumatt" to find all my mods.`**

   `The image below shows VikingShip as an example, but it was easier to reuse the image.`

   ![](https://i.imgur.com/5CR5XKu.png)

3. **Click the Download button to install the mod.**
4. **Launch the game.**

</details>

<br>
<br>

`Feel free to reach out to me on discord if you need manual download assistance.`

# Author Information

### Azumatt

`DISCORD:` Azumatt#2625

`STEAM:` https://steamcommunity.com/id/azumatt/

For Questions or Comments, find me in the Odin Plus Team Discord or in mine:

[![https://i.imgur.com/XXP6HCU.png](https://i.imgur.com/XXP6HCU.png)](https://discord.gg/qhr2dWNEYq)
<a href="https://discord.gg/pdHgy6Bsng"><img src="https://i.imgur.com/Xlcbmm9.png" href="https://discord.gg/pdHgy6Bsng" width="175" height="175"></a>