# discord-rpc-bridge

A bridge to update Discord status when using Flatpak applications.

I recently switched over to Linux and noticed that Flatpak Steam and Flatpak Discord have trouble communicating due to the Flatpak app sandboxing. 
I tried to symlink the Discord socket and tweak Flatpak permissions, but gave up.
It seems like this is due to Discord being sandboxed and not able to read `/proc` of the host.

So this is a tiny bridge that scans `/proc` on an interval to set your Discord activity status to what you're playing on Steam.

## Limitations

- systemd only
- This "should" support both native and proton-enabled games. but this was a Sunday project so I definitely missed things.
  - I only tested with Rimworld, Balatro, and SHENZHEN IO on Fedora 43 Kinoite. But, it works for my use case so far.
- The activity status is missing all the fancy stuff and instead sets the activity details to your distro.
- This only works for Steam games, but could potentially scan for other processes (KiCad, VSCode, Neovim, etc.)
- This doesn't work for multiple games. Maybe there's a way to smartly determine "focus", but I'm not doing that right now.

## Installation

```sh
# install as systemd service
git clone https://github.com/barrettotte/discord-rpc-bridge && cd discord-rpc-bridge
make build
make install

# verify
systemctl --user status discord-rpc-bridge

# view logs
journalctl --user -u discord-rpc-bridge -f

# uninstall
make uninstall
```

## Configuration

```js
{
  // how often to rescan /proc
  "scan_interval_seconds": 15,

  // Discord API version to use in game list download
  // ex: https://discord.com/api/v10/applications/detectable
  "discord_api_version": 10,

  // how often to invalidate the Discord game list cache
  "game_cache_ttl_days": 7,
  
  // game names to ignore that have steamapps/common in path.
  "ignored_games": [
    "SteamLinuxRuntime_soldier",
    "SteamLinuxRuntime_sniper",
    "SteamLinuxRuntime",
    "SteamControllerConfigs",
    "Proton - Experimental",
    "Proton Experimental",
    "Proton 7.0",
    "Proton 8.0",
    "Proton 9.0",
    "Proton Hotfix",
    "shader_compiler",
    "reaper"
  ]
}
```

## Discord Detectable Applications JSON

```sh
curl https://discord.com/api/v10/applications/detectable -o data/games.json

jq 'length' data/games.json
# 19551

jq '.[] | select(.name == "Balatro")' data/games.json
# {
#   "id": "1209665818464358430",
#   "name": "Balatro",
#   "executables": [
#     {
#       "name": "balatro/balatro.exe",
#       "os": "win32"
#     }
#   ]
# }
```
