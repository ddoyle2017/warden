package mod

/*
A Framework is a special kind of mod that enables the development
of plugins. Frameworks expose tools and code that other mod authors
build with. Because of this, each framework has specific rules around
how its installed and they are handled separately from normal mods.

Examples include:
  - BepInEx (Valheim, Skyrim, etc..)
  - Optifine (closed source Minecraft framework)
  - Fabric (open source Minecraft framework)
  - and more!
*/
type Framework struct {
}
