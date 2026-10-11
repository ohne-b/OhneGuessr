# Additional plugins

This directory is the published catalog for optional renderer plugins. Released
OhneGuessr clients download `registry.json`, each plugin's `manifest.json`, and
its compiled `index.js` directly from this GitHub path, so the directory layout
is a compatibility boundary.

Plugins are trusted ES modules that default-export `activate(api)`. The API in
`types/ohneguessr.d.ts` supplies shared in-game window and HUD button styles plus
private game capabilities; plugins also have normal browser API and DOM access.

Edit `*/src/index.ts` and `*/manifest.json`, then run:

```sh
npm --prefix plugins run build
```

The build updates the committed `index.js` files and checksum-bearing
`registry.json`. Built-in game modes and map integrations have their frontend
code in `frontend/src/features/` and their Go services in individual packages
directly under `internal/`.
The app's downloadable-plugin catalog UI and renderer host live in
`frontend/src/extensions/`; UI shared with built-in features lives in
`frontend/src/components/`.
