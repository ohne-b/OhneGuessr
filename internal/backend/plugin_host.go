package backend

import (
	"context"
	"maps"

	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

// pluginHost is the narrow boundary between plugins and core-owned map storage.
// Plugins own synchronization; core owns locking and manifest validation.
type pluginHost struct {
	maps        *mapStore
	coordinator *syncCoordinator
}

func (h *pluginHost) WithLibrary(run func(pluginhost.Library) error) error {
	h.maps.mu.Lock()
	defer h.maps.mu.Unlock()
	return run(pluginLibrary{h.maps})
}

func (h *pluginHost) AcquireSync(name string) (context.Context, func(), error) {
	return h.coordinator.acquire(name)
}

func (h *pluginHost) CancelSync(name string) bool {
	return h.coordinator.cancelJob(name)
}

type pluginLibrary struct{ store *mapStore }

func (l pluginLibrary) Directory() string { return l.store.dir }

func (l pluginLibrary) Manifest() (pluginhost.Manifest, error) {
	manifest, err := l.store.loadManifestLocked()
	if err != nil {
		return pluginhost.Manifest{}, err
	}
	return pluginhost.Manifest{
		Folders: append([]string(nil), manifest.Folders...),
		Maps:    cloneEntries(manifest.Maps),
	}, nil
}

func (l pluginLibrary) Resolve(path string) (string, error) {
	return l.store.resolve(path)
}

func (l pluginLibrary) Save(manifest pluginhost.Manifest) error {
	return l.store.saveManifestLocked(mapManifest{
		Version: manifestVersion,
		Folders: append([]string(nil), manifest.Folders...),
		Maps:    cloneEntries(manifest.Maps),
	})
}

func cloneEntries(entries []pluginhost.Entry) []pluginhost.Entry {
	cloned := make([]pluginhost.Entry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry
		cloned[i].Source = maps.Clone(entry.Source)
	}
	return cloned
}
