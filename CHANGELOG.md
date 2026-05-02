# Changelog

## 0.1.2

- New `manual_mappings` config option to override Discord client ID lookup for games whose Steam folder name doesn't match Discord's detectable name (e.g. Steam's `YakuzaKiwami3` vs Discord's `Yakuza Kiwami 3 & Dark Ties`)
- Auto-ignore any folder whose name starts with `SteamLinuxRuntime` or `Proton` — covers `SteamLinuxRuntime_4` and any future numbered runtime/Proton variants without needing to enumerate them in `ignored_games`
- Added graceful shutdown: clears Discord activity on SIGINT/SIGTERM instead of leaving the status stuck until Discord notices the broken pipe
- Added `--version` flag
- Fixed `ignored_processes` bypass when `/proc/<pid>/exe` readlink failed — `scanCmdline` now also checks `argv[0]`
- Fixed potential cache poisoning: `games.json` is no longer overwritten unless the Discord API returns HTTP 200 with a non-empty list
- `install.sh` and `download.sh` no longer overwrite an existing `config.json` on update — preserves `manual_mappings` and other user customizations, and stops clobbering symlinked configs managed via dotfiles
- Capped IPC payload allocation at 1 MiB to prevent runaway allocation from a malformed header

## 0.1.1

- Fixed false game detection from lingering Steam wrapper processes (e.g. `gamescopereaper`, `reaper`) whose command line args contain game paths after the game has exited
- New `ignored_processes` config option to skip Steam launcher/wrapper processes during `/proc` scanning
- Moved `reaper` from `ignored_games` to `ignored_processes` (it's a Steam binary, not a `steamapps/common` directory)

## 0.1.0

Initial release.
