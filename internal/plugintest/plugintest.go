package plugintest

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

type Host struct {
	dir      string
	mapMu    sync.Mutex
	manifest pluginhost.Manifest
	jobMu    sync.Mutex
	jobName  string
	cancel   context.CancelFunc
}

func NewHost(t testing.TB) *Host {
	t.Helper()
	return &Host{
		dir:      t.TempDir(),
		manifest: pluginhost.Manifest{Folders: []string{}, Maps: []pluginhost.Entry{}},
	}
}

func (h *Host) WithLibrary(run func(pluginhost.Library) error) error {
	h.mapMu.Lock()
	defer h.mapMu.Unlock()
	return run(library{h})
}

func (h *Host) AcquireSync(name string) (context.Context, func(), error) {
	h.jobMu.Lock()
	defer h.jobMu.Unlock()
	if h.jobName != "" {
		return nil, nil, fmt.Errorf("%s synchronization is running", h.jobName)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.jobName, h.cancel = name, cancel
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			h.jobMu.Lock()
			h.jobName, h.cancel = "", nil
			h.jobMu.Unlock()
		})
	}, nil
}

func (h *Host) CancelSync(name string) bool {
	h.jobMu.Lock()
	defer h.jobMu.Unlock()
	if h.jobName != name || h.cancel == nil {
		return false
	}
	h.cancel()
	return true
}

func (h *Host) Directory() string { return h.dir }

func (h *Host) Snapshot() pluginhost.Manifest {
	h.mapMu.Lock()
	defer h.mapMu.Unlock()
	return cloneManifest(h.manifest)
}

func (h *Host) DeleteMap(id string) error {
	h.mapMu.Lock()
	defer h.mapMu.Unlock()
	for i, entry := range h.manifest.Maps {
		if entry.ID != id {
			continue
		}
		_ = os.Remove(filepath.Join(h.dir, filepath.FromSlash(entry.File)))
		h.manifest.Maps = slices.Delete(h.manifest.Maps, i, i+1)
		return nil
	}
	return fmt.Errorf("map not found")
}

type library struct{ host *Host }

func (l library) Directory() string { return l.host.dir }

func (l library) Manifest() (pluginhost.Manifest, error) {
	return cloneManifest(l.host.manifest), nil
}

func (l library) Resolve(path string) (string, error) {
	return filepath.Join(l.host.dir, filepath.FromSlash(path)), nil
}

func (l library) Save(manifest pluginhost.Manifest) error {
	l.host.manifest = cloneManifest(manifest)
	return nil
}

func cloneManifest(manifest pluginhost.Manifest) pluginhost.Manifest {
	result := pluginhost.Manifest{
		Folders: slices.Clone(manifest.Folders),
		Maps:    make([]pluginhost.Entry, len(manifest.Maps)),
	}
	for i, entry := range manifest.Maps {
		entry.Source = maps.Clone(entry.Source)
		result.Maps[i] = entry
	}
	return result
}

func WaitUntil(t testing.TB, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for background synchronization")
}

func WriteJSON(t testing.TB, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}
