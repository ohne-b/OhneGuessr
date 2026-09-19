package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

type Backend struct {
	maps         *mapStore
	coordinator  *syncCoordinator
	mapPlugins   []pluginhost.MapPlugin
	shutdownOnce sync.Once
	shutdownErr  error
}

func New(dataDir string, factories ...pluginhost.MapPluginFactory) (*Backend, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	maps, err := newMapStore(filepath.Join(dataDir, "maps"))
	if err != nil {
		return nil, err
	}
	if err := maps.initialize(); err != nil {
		maps.Close()
		return nil, fmt.Errorf("load map library: %w", err)
	}
	pluginData := filepath.Join(dataDir, "plugin-data")
	if err := os.MkdirAll(pluginData, 0o700); err != nil {
		maps.Close()
		return nil, fmt.Errorf("create plugin data directory: %w", err)
	}

	coordinator := &syncCoordinator{}
	a := &Backend{
		maps:        maps,
		coordinator: coordinator,
	}
	host := &pluginHost{maps: maps, coordinator: coordinator}
	for _, factory := range factories {
		plugin := factory(host, pluginData)
		a.mapPlugins = append(a.mapPlugins, plugin)
		maps.registerMapPolicy(plugin.MapPolicy())
	}
	return a, nil
}

func (a *Backend) Shutdown(ctx context.Context) error {
	a.shutdownOnce.Do(func() {
		if err := a.coordinator.shutdown(ctx); err != nil {
			if a.shutdownErr == nil {
				a.shutdownErr = err
			}
		}
		if err := a.maps.Close(); err != nil && a.shutdownErr == nil {
			a.shutdownErr = err
		}
	})
	return a.shutdownErr
}

func (a *Backend) HasMap(id string) bool {
	a.maps.mu.Lock()
	defer a.maps.mu.Unlock()
	manifest, err := a.maps.loadManifestLocked()
	if err != nil {
		return false
	}
	for _, entry := range manifest.Maps {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func (a *Backend) ExportMaps(filename string) error {
	return a.maps.exportZIP(filename)
}
