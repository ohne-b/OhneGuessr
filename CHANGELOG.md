# Changelog

## [v0.2.1](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.2.1) — 2026-09-11

- keep maps playable and editable during Map Making App sync, preserving library edits made while downloads are running
- keep all five map-size shortcut keys on one row in Controls
- reorganize frontend and backend code, share sync controls, and simplify map rendering and round preparation for easier maintenance

## [v0.2.0](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.2.0) — 2026-09-11

- extend launcher themes across the gameplay HUD, Street View navigation, plugin windows, and window headerbars, including a new Dark Mode theme
- refine final round cards, add mouse-wheel navigation between rounds, and add a fifth, wider guess-map size
- improve launcher spacing and controls at smaller window sizes
- keep plugin window controls accessible when windows overlap and move Nearby Explorer article actions into the window header
- revalidate plugin downloads to avoid mismatched catalog, manifest, and source versions
- upgrade MapLibre GL JS to v6 and refresh Wails and frontend dependencies

## [v0.1.4](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.1.4) — 2026-08-28

- add signed in-app updates for Linux `.deb` installations
- fix the black clipping artifact behind the guess map on Linux
- restore immediate round initialization

Linux users upgrading from an earlier release need to install this `.deb` once; future `.deb` updates can install in-app.

## [v0.1.3](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.1.3) — 2026-08-27

- add Nearby Explorer to browse Wikipedia places within 10 km of the current location
- add count-up timer mode
- add configurable keybinds for game-window fullscreen, opening the current location in Street View, and placing a guess at the map center

## [v0.1.2](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.1.2) — 2026-08-24

- add the built-in Country Streak mode with persistent current and best streaks
- resolve offshore and coastal locations to the nearest country instead of voiding rounds
- align the streak HUD with the standard round HUD

## [v0.1.1](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.1.1) — 2026-08-21

- add a Hide Street View car display setting and remove the guess-map border
- add Local Radio to play the nearest available station while you explore
- refresh OpenSV for more reliable unofficial panorama tiles and update Wails and frontend dependencies

## [v0.1.0](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.1.0) — 2026-08-17

- add Translator to read text from Street View with OCR.space and translate it with MyMemory
- move bundled plugin sources to TypeScript and check their compiled files and catalog checksums in CI

## [v0.0.11](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.11) — 2026-08-16

- add a downloadable plugin catalog with shared in-game buttons and windows, private settings, and APIs for panoramas, captures, locations, and external links
- add Coverage Info, Screenshot, and PlantNet for panorama metadata, image capture, and plant identification
- load startup data in parallel and sample game rounds on the backend to reduce memory use on large maps
- start a new game when the game window is refreshed
- stabilize panorama capture and refresh the OpenSV viewer

## [v0.0.10](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.10) — 2026-08-13

- theme the local party phone UI and refine the host's reveal controls
- fix checkpoint peeking when the key is pressed and released rapidly
- add signed in-app updates for macOS
- upgrade Wails and refresh frontend dependencies and build tooling

## [v0.0.9](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.9) — 2026-08-07

- add local network multiplayer with phone controls, joined-player cards, and round leaderboards
- add shareable `.ohne` challenges with fixed rounds and rules, and comparisons against the creator's guesses
- add a dedicated launcher tab for built-in plugin settings
- replace filesystem rescanning with map and folder management in the launcher
- add ZIP export for the entire map library, preserving its folder structure
- fix guess-map resizing when hovering over the panel
- switch updates to the native Wails updater

## [v0.0.8](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.8) — 2026-07-29

- fix map-library folder chevrons so collapsed folders point right and expanded folders point down

## [v0.0.7](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.7) — 2026-07-29

- allow deleting folders together with their maps and subfolders
- restore the HUD visibility hotkey
- stabilize inline map deletion confirmations
- refresh app and interface icons
- simplify shared panorama loading and map-storage code

## [v0.0.6](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.6) — 2026-07-27

- add a progress ring to the game timer
- unify HUD panel backgrounds and refine final results styling
- improve compass tick and label alignment
- use Manrope across the game and launcher
- preload Learnable Meta clues during rounds and show clue text without waiting for images

## [v0.0.5](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.5) — 2026-07-26

- add launcher themes, including OhneB
- add F11 to toggle game-window fullscreen
- add drag-and-drop map imports and inline deletion confirmations
- separate sync panels and align launcher controls, fonts, and spacing
- preserve custom round and timer values when switching presets
- show a centered loading indicator when a game opens

## [v0.0.4](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.4) — 2026-07-25

- show available updates in the launcher sidebar, with download progress, restart, and retry controls
- publish signed Windows update metadata so the app can verify downloaded updates

## [v0.0.3](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.3) — 2026-07-25 (prerelease)

- move settings and the map library into a separate launcher window
- move the desktop app to Wails v3
- reject Windows drive and network paths in map-library path validation on every platform

## [v0.0.2](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.2) — 2026-07-24 (prerelease)

- add Linux AppImage and `.deb` packages
- add macOS `.dmg` packages

## [v0.0.1](https://github.com/ohne-b/OhneGuessr/releases/tag/v0.0.1) — 2026-07-23 (prerelease)

- first Windows prerelease, with a portable executable and installer
- add Moving, No Moving, and NMPZ games with configurable rounds, timers, and world- or map-scaled scoring
- add a local map library with JSON imports, folders, search, renaming, and deletion
- add Map Making App and Learnable Meta sync, with Learnable Meta clues after each round
- show guesses, walked paths, and results on the map, with a final summary and individual round selection
- add movement checkpoints, checkpoint peeking, and hold-to-look-behind controls
- add rebindable shortcuts, compass styles, expanded map sizes, zoom-speed controls, and a configurable accent color
