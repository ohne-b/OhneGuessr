<p align="center">
  <img src="frontend/public/images/ohneguessr-logo.svg" width="128" alt="OhneGuessr logo">
</p>

<h1 align="center">OhneGuessr</h1>

<p align="center">A free, lean, local GeoGuessr alternative.</p>

<p align="center">
  <a href="LICENSE.md"><img src="https://img.shields.io/badge/license-PolyForm_Noncommercial_1.0.0-22c55e" alt="License: PolyForm Noncommercial 1.0.0"></a>
</p>

## Download

Download the latest version from [GitHub Releases](https://github.com/ohne-b/OhneGuessr/releases).

See the [changelog](CHANGELOG.md) for release history.

## Features

- Moving, No Moving, and NMPZ games with configurable rounds and timers.
- World- or map-scaled scoring with per-round or final-only results.
- Optional Country Streak mode with persistent current and best streaks.
- Shareable `.ohne` challenges with exact rounds and challenger comparisons.
- Rebindable controls, configurable compass, map size, zoom speed, and accent color.
- Optional Map Making App and Learnable Meta synchronization.

## Data

OhneGuessr keeps maps and settings in:

```text
Windows: %LOCALAPPDATA%\OhneGuessr\
Linux:   $XDG_DATA_HOME/ohneguessr/
         or ~/.local/share/ohneguessr/ when XDG_DATA_HOME is unset

OhneGuessr/
|-- maps/
|   |-- maps.json             authoritative map index
|   |-- map-making-app/       synchronized Map Making App maps
|   |-- Learnable Meta/       synchronized Learnable Meta maps
|   `-- any folders you add
|-- plugin-data/
|   |-- map-making-app.json   private sync settings and API key
|   `-- learnable-meta.json   private sync settings, map IDs, and API key
`-- webview/                  UI settings, keybindings, and window storage
```

Your data stays in this folder when you update, move, or uninstall the app.

> [!CAUTION]
> Deleting a local map or folder permanently removes its JSON files. Use **Export all maps** first if you may need them again.

## Maps

The app opens on **Maps** in the launcher. Select a folder before importing to place the map there, or use **New folder**, drag-and-drop, and **Move to** to organize maps without leaving the app. **Export all maps** creates a portable ZIP of every map while preserving that folder structure; extract it before importing its JSON files again.

The launcher owns the `maps/` directory and `maps.json` index. Files added, moved, or renamed there outside the app are not imported or reconciled; use the launcher controls instead. The export excludes sync settings, API keys, and other internal metadata.

Supported formats are a JSON array or an object containing `customCoordinates`:

```json
[
  { "lat": 48.8584, "lng": 2.2945, "panoid": "...", "heading": 0, "pitch": 0, "zoom": 1 }
]
```

Every location needs finite `lat` and `lng` values. Panorama ID, heading, pitch, and zoom are optional.

## Plugins

The **Plugins** launcher tab manages the built-in Country Streak, Challenges, Map Making App Sync, and Learnable Meta plugins. Its **Additional** tab installs and updates curated plugins from this repository's GitHub-hosted marketplace. Sync configuration stays beside Maps and is shown only while its plugin is enabled.

Additional plugins are authored in `plugins/<id>/src/index.ts` and built into trusted ES modules that default-export an `activate(api)` function. The API in `plugins/types/ohneguessr.d.ts` supplies the shared in-game window and HUD button styles plus private game capabilities; plugins otherwise have normal access to the renderer's browser APIs and DOM.

## Country Streak

Enable **Country Streak** under Plugins, then use the flag action beside any map. A round extends the streak when the guess and panorama are in the same country; a wrong guess or timeout resets it. The mode keeps the normal round, timer, movement, and scoring settings and saves current and best streaks locally.

Country lookup runs entirely in the game window using the bundled [Natural Earth 1:50m country boundaries](https://github.com/nvkelso/natural-earth-vector/releases/tag/v5.1.2); coordinates outside every boundary resolve to the nearest country edge instead of being discarded. It does not call a reverse-geocoding service. Country flag SVGs live in `frontend/public/flags/` and are available app-wide at `/flags/<ISO_A2>.svg`.

## Challenges

Enable **Challenges** under Plugins, finish a game, and select **Create challenge**. The resulting `.ohne` file contains the exact ordered rounds, locked movement/timer/scoring rules, and the creator's guesses. Open one with the Maps `+` button, drop it onto the map library, or double-click it after installing OhneGuessr. Challenge files are played directly and are not added to the map library.

`.ohne` v1 is plain UTF-8 JSON. Scores and distances are recalculated rather than stored:

```json
{
  "format": "ohneguessr.challenge",
  "version": 1,
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "mapName": "A Community World",
  "rules": { "movement": "moving", "timerSeconds": null, "scoreScaleKm": 14916.862 },
  "rounds": [{
    "lat": 48.8584,
    "lng": 2.2945,
    "panoId": "...",
    "heading": 120,
    "pitch": 0,
    "zoom": 1,
    "challengerGuess": { "lat": 48.86, "lng": 2.31 }
  }]
}
```

`challengerGuess` is `null` for a timeout. Version 1 uses Google Street View by definition; panorama IDs are preferred and coordinates are the fallback.

## Map synchronization

### Map Making App sync

1. Create an API key at [map-making.app/keys](https://map-making.app/keys).
2. Enable **Map Making App Sync** under Plugins, then save the key beside Maps.
3. The first sync starts immediately; use **Sync now** for later updates.

Active, non-empty location maps are downloaded with up to ten concurrent requests. Downloads leave the map library available for games and edits; publication uses the latest library state to preserve edits made during synchronization. Archived maps are skipped. Failed downloads retain the last good local file. Renaming or moving a synchronized map in the launcher creates a local name or folder override.

### Learnable Meta sync

1. Give a personal map a unique **GeoGuessr ID** in [Learnable Meta](https://learnablemeta.com/personal).
2. Create a key at [Learnable Meta profile -> API token](https://learnablemeta.com/profile/token).
3. Enable **Learnable Meta** under Plugins, then save the key beside Maps.
4. Add a local name and the same map ID.

Each configured map is validated and downloaded immediately. **Sync now** fetches later changes. Learnable Meta clues appear after each round, and their layout is saved in the native WebView.

API keys stay only in `plugin-data/`; they are never included in `maps.json` or returned to the frontend. Disabling sync or forgetting a key keeps downloaded maps playable.

## Controls

| Input | Action |
| --- | --- |
| <kbd>Space</kbd> | Submit, continue, or replay |
| _Unbound_ | Place a guess at the map center |
| <kbd>E</kbd> / <kbd>Q</kbd> | Zoom in / out |
| <kbd>N</kbd> | Face north; press again to look down |
| <kbd>R</kbd> | Reset the view; in Moving, return to the start |
| <kbd>C</kbd> | Set or return to a checkpoint |
| Hold <kbd>V</kbd> | Peek at the checkpoint |
| Hold <kbd>B</kbd> | Look behind |
| <kbd>M</kbd> | Pin / unpin the expanded map |
| <kbd>F</kbd> | Toggle the fullscreen map |
| <kbd>F11</kbd> | Toggle game-window fullscreen |
| _Unbound_ | Open the current location in Google Street View |
| <kbd>1</kbd> / <kbd>2</kbd> / <kbd>3</kbd> / <kbd>4</kbd> / <kbd>5</kbd> | Select expanded map size |
| <kbd>H</kbd> | Hide / show the interface |
| <kbd>Esc</kbd> | Focus the launcher |
| Click map | Place or move a guess |
| <kbd>Shift</kbd> + click map | Place and submit a guess |
| Double-click map | Toggle the fullscreen map |

Gameplay bindings are rebindable under **Controls** in the launcher.

## Development

Backend: Go 1.26 and Wails v3.0.0-beta.16. Frontend: Svelte 5, Vite, and TypeScript.

Install Go 1.26 and Node.js 20.19+, 22.12+, or 24+. The Wails CLI is tracked in `go.mod`, so no global installation is needed.

Start the development app with:

```powershell
go tool wails3 dev
```

Run `npm --prefix plugins run build` after changing a plugin's TypeScript source or manifest.

Development uses `%LOCALAPPDATA%\OhneGuessr` on Windows.

Build the portable EXE with:

```powershell
go tool wails3 build
```

For the Setup EXE, install [NSIS](https://nsis.sourceforge.io/) and run:

```powershell
go tool wails3 task windows:package VERSION=0.0.0
```

Builds go to `bin/`. To build only the frontend, run:

```powershell
npm --prefix frontend run build
```

This creates the ignored `frontend/dist/` directory. The **Check** workflow runs source checks on pushes and pull requests; its temporary Windows package is manual. The **Release** workflow builds Windows, Linux, and macOS files and uploads them to a draft release.

See [Architecture and test ownership](docs/architecture.md) for the source map, dependency rules, state and styling contracts, and release script inputs.

Frontend tests stay flat in `frontend/test/`, grouped by filename prefix, and import source through `@/` (for example, `@/features/game/deck.js`). Downloadable plugins keep their tests in `plugins/<id>/`; Go tests stay beside their packages. Run frontend and downloadable-plugin tests with `npm --prefix frontend test`, plugin build checks with `npm --prefix plugins run check`, and backend checks with `go vet ./...` and `go test ./...`.

### Repository structure

```text
OhneGuessr/
|-- .github/workflows/     GitHub Actions
|-- build/                 Wails tasks and platform release scripts
|-- docs/                  architecture and test ownership
|-- frontend/
|   |-- src/               Svelte and TypeScript source
|   |-- test/              frontend tests, grouped by filename prefix
|   |-- bindings/          generated, committed Wails bindings
|   |-- public/            static assets, country flags, and vendored OpenSV
|   |-- dist/              generated frontend (ignored)
|   `-- package.json       frontend dependencies and scripts
|-- internal/
|   |-- backend/           map storage and local HTTP backend
|   |-- challenges/        challenge files and desktop service
|   |-- desktop/           Wails runtime and window service
|   |-- learnable-meta/    Learnable Meta synchronization
|   |-- local-party/       party service and server
|   |-- map-making-app/    Map Making App synchronization
|   |-- pluginmanager/     additional-plugin installation and state
|   `-- updates/           cross-platform update service
|-- plugins/               published additional-plugin catalog and sources
|-- main.go                embedded frontend and executable entry point
|-- go.mod / go.sum        Go dependencies
`-- Taskfile.yml           Wails build tasks
```

## Troubleshooting

### Windows blocks the executable

The release is unsigned. Verify that it came from this repository, optionally compare its SHA-256 checksum, then use **More info -> Run anyway** in SmartScreen.

### The app window is blank

Download the release executable rather than an individual source file. `go tool wails3 dev` and `go tool wails3 build` generate `frontend/dist/` automatically; build it first with `npm --prefix frontend run build` only when running Go directly. Windows 10 also needs the Microsoft WebView2 Runtime; the installer includes its official bootstrapper and offers to install it when missing.

### An update fails

Check the internet connection and restart OhneGuessr to check again. On Windows, macOS, and Linux installations made from the `.deb`, an available version appears in the launcher footer. Linux downloads the signed `.deb` and asks for administrator authentication before APT installs it; AppImage updates remain manual. OhneGuessr refuses any update whose SHA-256 digest or Ed25519 signature does not match the release metadata.

### Panoramas are missing, blurry, or black

Check the internet connection and try another location. Some Street View panoramas are removed or temporarily unavailable; high-resolution tiles can also take a moment to sharpen.

## License

Copyright (c) 2026 OhneB

Released under the [PolyForm Noncommercial License 1.0.0](LICENSE.md). This license covers only this project's own code and assets.
