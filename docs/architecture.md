# Architecture and test ownership

OhneGuessr is a Wails desktop application with a Svelte frontend. `main.go`
embeds the built frontend and calls `internal/desktop.Run`, which creates the
backend, registers services, and opens the application windows.

## Directory map

```text
frontend/
|-- src/
|   |-- main.ts                  imports app/bootstrap.ts
|   |-- vite-env.d.ts
|   |-- app/
|   |   |-- bootstrap.ts         route selection and lazy startup
|   |   |-- route.ts
|   |   |-- app.css              shared stylesheet import order
|   |   |-- launcher/            window shell and feature composition
|   |   `-- game/                GameApp.svelte and setup.ts
|   |-- features/
|   |   |-- game/                session, input, runtime, deck, scoring, HUD
|   |   |-- map-library/         library UI, imports, folders, source policies
|   |   |-- settings/            preferences, validation, controls, keybindings
|   |   |-- updates/             update availability and action UI
|   |   |-- challenges/
|   |   |-- country-streak/
|   |   |-- local-party/
|   |   |-- map-making-app/
|   |   |-- learnable-meta/
|   |   `-- map-sync/            shared synchronization controls
|   |-- extensions/              downloadable-plugin UI, API host, runtime
|   |-- rendering/
|   |   |-- map/                 MapLibre, providers, result layers
|   |   `-- panorama/            OpenSV, capture, metadata, car mask
|   |-- platform/               desktop services, HTTP, asset URLs
|   |-- components/             reusable controls and shared plugin windows
|   |-- styles/                 global themes, base styles, accent application
|   `-- shared/                 geographic types and small DOM lookup helper
|-- test/                       flat, prefixed frontend test files
|-- bindings/                   generated, committed Wails bindings
|-- public/                     stable static assets
`-- dist/                       generated frontend; ignored by Git

internal/
|-- backend/                    map storage, HTTP routes, sync coordination
|-- challenges/                 challenge files and desktop service
|-- desktop/                    Wails composition, windows, native services
|-- learnable-meta/              Learnable Meta client and synchronization
|-- local-party/                 party service, sessions, and HTTP server
|-- map-making-app/              Map Making App client and synchronization
|-- pluginmanager/              downloadable-plugin catalog and installation
|-- pluginhost/                 map integration contracts
|-- plugintest/                 shared helpers for Go integration tests
|-- httpjson/                   HTTP JSON and response limits
|-- mapfile/                    map file and atomic write helpers
`-- updates/                    platform update handling

plugins/                        published downloadable-plugin catalog
|-- <id>/                       source, manifest, compiled index.js, tests
|-- types/ohneguessr.d.ts        published frontend plugin API
`-- registry.json               generated catalog with source checksums

build/
|-- Taskfile.yml                shared frontend/bindings build tasks
|-- windows/                    executable metadata and NSIS packaging
|-- linux/                      build task, release packages, package smoke check
|-- darwin/                     app bundle task and release packages
`-- release/                    updater signing and release file validation
```

## Frontend ownership

`app/` connects features. The launcher owns its titlebar, navigation, plugin
page, sync layout, map actions, and file dispatch. The map library receives
actions and file handlers as props; it does not choose which game modes or
file integrations the launcher offers. `app/launcher/events.ts` is the small
navigation-request contract that features may import without importing the shell.

Game responsibilities are split within `features/game/`:

| File | Responsibility |
| --- | --- |
| `session.ts` | Round progression, guesses, scoring, results, session effects. |
| `runtime.ts` | Renderer instances, map/compass setup, live display settings. |
| `input.ts` | Keyboard and compass actions, held-key cleanup. |
| `deck.ts`, `round-preparation.ts` | Sampling, location replacement, panorama preparation and cancellation. |
| `state.svelte.ts`, `ui.svelte.ts` | Game state and game UI state. |
| `game-mode.svelte.ts` | Contract and active mode used by built-in game modes. |

`app/game/setup.ts` connects these pieces to Challenges, Learnable Meta, and
downloadable plugins. Preserve startup order: load initial data and integrations,
create the panorama, activate extensions, create maps, attach settings/input
listeners, then activate the requested game. Optional Learnable Meta startup
failure must not prevent the game from starting.

Features may reuse another feature's established contracts, such as `GameMode`
and the map-source registry. Keep named integration registration in `app/`.
Rendering modules should depend on their own definitions, `platform/`, and
small shared types, without importing application shells or feature state.
`components/`, `shared/`, and `styles/` should not become alternate homes for
feature-specific code. Import implementations directly; no forwarding barrels
are needed for the former root paths.

Types and configuration live with their owners. Game rules and view options
are in `features/game/`; preferences and keyboard defaults are in
`features/settings/`; map providers and zoom defaults are in `rendering/map/`;
theme definitions and accent application are in `styles/theme.ts`. Use
`import type` for renderer contracts. Importing map configuration must not load
MapLibre or construct a renderer.

## Appearance and state lifetime

Reusable controls keep their styles in their `.svelte` files. Feature layout
styles stay with the feature; shell layouts stay in `app/launcher/`. The import
order in `app/app.css` preserves the existing global cascade.

DOM-based plugins use the global classes supplied by select, range, icon-button,
and spinner components. Their eager imports in `app/bootstrap.ts` make these
styles available in every window, including windows that do not render those
Svelte controls. Keep the class names and public asset paths stable.

`features/settings/store.svelte.ts` loads preferences and applies the theme
when imported. Its explicit synchronization setup handles desktop events and
browser storage changes. Preserve this initialization timing when moving code.
The additional-plugin state owner stays mounted across Core/Additional tab
changes so catalog state, drafts, and in-flight actions survive navigation.
Other page drafts likewise retain their existing lifetime.

## Go ownership and compatibility

`internal/backend` remains one package. `backend.go` owns construction and
shutdown; `config.go` resolves the data directory; `routes.go` owns HTTP routing;
`sync.go` coordinates background synchronization. `manifest.go` owns map-index
validation and persistence, while `storage.go`, `folders.go`, `paths.go`,
`sampling.go`, and `export.go` own their respective map operations.

Map integrations use the existing `pluginhost.Host` contract. Library access
through `WithLibrary` is valid only inside its callback. Preserve locking,
atomic writes, rollback behavior, and cancellation when changing storage or
sync code. File organization does not require new packages or exported helpers.

`internal/pluginmanager` also remains one package. `service.go` constructs the
service; `catalog.go` fetches the curated catalog; `install.go` installs, removes,
and reads installed modules; `state.go` owns enabled state and private settings;
`manifest.go` owns payload types, validation, and checksums. Keep the Wails
service names, public methods, JSON fields, limits, and validation behavior stable.

Built-in frontend functionality lives in `features/`. Its Go services live in
packages directly under `internal/`, such as `challenges/` and `local-party/`,
with tests beside their implementations. Moving a Go service package changes
its generated Wails binding IDs; regenerate bindings and rebuild the frontend
together with the backend when changing those paths.

Root `plugins/` is the published downloadable catalog.
Released clients fetch `/plugins/registry.json`, `/plugins/<id>/manifest.json`,
and `/plugins/<id>/index.js`. Keep these paths, IDs, API version, and checksum
format compatible. See [the plugin guide](../plugins/README.md).

## Test ownership

| Code under test | Test location |
| --- | --- |
| Frontend application and built-in features | `frontend/test/*.test.ts`; flat names with `app-`, `game-`, `maps-`, `feature-`, or `plugin-` prefixes. |
| Downloadable plugins | `plugins/<id>/*.test.js`, beside the owning plugin. |
| Go packages and built-in services | `*_test.go` beside the implementation in the same package directory. |

Frontend tests import source through `@/`, for example
`@/features/game/deck.js`. Vitest discovers both flat frontend tests and
downloadable-plugin tests. Keep Go tests colocated so they can exercise package
internals without exporting implementation details for a centralized test folder.
Reuse `internal/plugintest` where its existing helpers fit.

## Release scripts

`.github/workflows/release.yml` owns job ordering, runner setup, version and
artifact names, secrets, and upload to the existing draft release. It checks out
the requested tag wherever repository scripts are needed. Scripts run from the
repository root and receive the workflow's existing environment values:

| Script | Inputs | Result |
| --- | --- | --- |
| `build/linux/package-release.sh` | Built `bin/OhneGuessr`; `RELEASE_VERSION`, `APPIMAGE_NAME`, `DEB_NAME`, `LINUXDEPLOY_SHA256`, `GITHUB_WORKSPACE`. | Staged Debian/AppDir trees and verified `.deb`/AppImage files in `release/`. |
| `build/linux/smoke-release.sh` | Downloaded Linux artifacts; `RELEASE_VERSION`, `APPIMAGE_NAME`, `DEB_NAME`, `GITHUB_WORKSPACE`; fresh Linux runner. | DEB upgrade checks and AppImage launch check. |
| `build/darwin/package-release.sh` | Signed universal `bin/OhneGuessr.app`; `DMG_NAME`, `UPDATE_NAME`, `RUNNER_TEMP`. | Verified DMG and updater archive in `release/`. |
| `build/release/sign.ps1` | Downloaded artifacts; `RELEASE_TAG`, `RELEASE_VERSION`, `SETUP_NAME`, `PORTABLE_NAME`, `MAC_UPDATE_NAME`, `DEB_NAME`, `RUNNER_TEMP`, updater key pair. Requires PowerShell 7 and OpenSSL. | `latest.json` with signed SHA-256 digests and `SHA256SUMS.txt`. |
| `build/release/validate.sh` | Completed `release/`; `RELEASE_VERSION`; Bash and jq. | Exact artifact-set and Linux updater metadata checks. |

The workflow supplies `UPDATE_PRIVATE_KEY` from its secret and checks it against
`UPDATE_PUBLIC_KEY`. Preserve updater metadata fields, including legacy setup
and portable entries, when editing the signing script. Shell scripts use LF
line endings through `.gitattributes` and are invoked explicitly with Bash.
