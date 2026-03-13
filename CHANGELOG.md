# Changelog

## 0.1.1

- Fixed false game detection from lingering Steam wrapper processes (e.g. `gamescopereaper`, `reaper`) whose command line args contain game paths after the game has exited
- New `ignored_processes` config option to skip Steam launcher/wrapper processes during `/proc` scanning
- Moved `reaper` from `ignored_games` to `ignored_processes` (it's a Steam binary, not a `steamapps/common` directory)

## 0.1.0

Initial release.
