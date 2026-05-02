# discord-rpc-bridge

A bridge to update Discord Rich Presence status with your current Steam game on Linux.

This works with both native and Flatpak Steam, and supports native, Flatpak, and Snap Discord.
It scans `/proc` on an interval to detect running Steam games (native and Proton) and sets your Discord activity status via IPC.

![assets/balatro-status.png](assets/balatro-status.png)

## Limitations

- Linux only, systemd only
- Supports both native and Proton games. Game detection works by matching `steamapps/common` in process paths.
- Only detects Steam games. Could potentially scan for other processes (KiCad, VSCode, Neovim, etc.)
- Only tracks one game at a time (first match in `/proc`).
- Activity status shows your distro name instead of game-specific rich presence assets.

## Installation

Download the latest release and install as a systemd service:

```sh
curl -fsSL https://raw.githubusercontent.com/barrettotte/discord-rpc-bridge/master/scripts/download.sh | bash
```

### Build from source

```sh
git clone https://github.com/barrettotte/discord-rpc-bridge && cd discord-rpc-bridge
make build
make install
```

### Updating

Re-run the install script. This stops the service and updates the binary; an existing `config.json` is preserved (so your `manual_mappings` and other customizations survive).

```sh
systemctl --user stop discord-rpc-bridge
curl -fsSL https://raw.githubusercontent.com/barrettotte/discord-rpc-bridge/master/scripts/download.sh | bash
```

### Verify / Logs / Uninstall

```sh
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

  // extra steamapps/common folder names to ignore during game detection.
  // any name starting with "SteamLinuxRuntime" or "Proton" is auto-ignored,
  // so you only need to list other false-positive folders here.
  "ignored_games": [
    "SteamControllerConfigs",
    "shader_compiler"
  ],

  // process exe basenames to skip entirely during /proc scanning.
  // prevents Steam launcher/wrapper processes from false-detecting games
  // via their command line arguments.
  "ignored_processes": [
    "gamescopereaper",
    "reaper",
    "steam-launch-wrapper",
    "pressure-vessel-wrap"
  ],

  // override the Discord client ID lookup for a given Steam folder name.
  // useful when Discord's detectable name doesn't match the folder name
  // (e.g. "Yakuza Kiwami 3 & Dark Ties" vs Steam's "YakuzaKiwami3").
  // keys are exact Steam folder names; values are Discord application IDs.
  "manual_mappings": {
    "YakuzaKiwami3": "1464821189921996860"
  }
}
```

### Manual mappings

When automatic name matching fails (Discord's detectable name differs from the Steam folder), add an entry to `manual_mappings`.
The cache at `~/.cache/discord-rpc-bridge/games.json` already has every detectable game, so you don't need to re-download anything.

```sh
# 1. find the Steam folder name the bridge sees for your running game.
#    (a "(ID: 000000000000000000)" line means automatic lookup failed and
#    you need a manual mapping for that folder.)
journalctl --user -u discord-rpc-bridge | grep -oP 'Connected to game \K.+' | sort -u

# 2. search Discord's detectable list for matching client IDs
#    (case-insensitive substring search against the cached game list)
jq '.[] | select(.name | test("yakuza kiwami"; "i")) | {id, name}' \
    ~/.cache/discord-rpc-bridge/games.json

# 3. one-shot: print a ready-to-paste manual_mappings entry. pass the
#    Steam folder name and a unique Discord-name fragment as --arg values.
jq --arg s "YakuzaKiwami3" --arg q "yakuza kiwami 3" \
    '[.[] | select(.name | test($q; "i"))] | map({($s): .id}) | add' \
    ~/.cache/discord-rpc-bridge/games.json
```

After editing `config.json`, restart the service: `systemctl --user restart discord-rpc-bridge`.

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
