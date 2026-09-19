package mapmakingapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/plugintest"
)

func TestSyncAllowsLibraryEditsDuringNetworkRequests(t *testing.T) {
	host := plugintest.NewHost(t)
	paused, resume := make(chan string), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case paused <- r.URL.Path:
		case <-r.Context().Done():
			return
		}
		select {
		case <-resume:
		case <-r.Context().Done():
			return
		}
		if r.URL.Path == "/api/maps" {
			plugintest.WriteJSON(t, w, []mmaRemoteMap{{ID: 1, Name: "Remote", Type: "locations", Storage: "active", LocationCount: 1}})
		} else {
			plugintest.WriteJSON(t, w, []map[string]any{{"lat": 1, "lng": 2}})
		}
	}))
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := New(host, filepath.Join(t.TempDir(), "mma.json"))
	service.baseURL, service.client = upstream.URL, upstream.Client()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		_, err := service.syncMaps(ctx, "key")
		done <- err
	}()
	defer func() {
		cancel()
		<-done
	}()
	for _, endpoint := range []string{"/api/maps", "/api/maps/1/locations"} {
		select {
		case got := <-paused:
			if got != endpoint {
				t.Fatalf("request = %s, want %s", got, endpoint)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("request did not start")
		}
		edited := make(chan error, 1)
		go func() {
			edited <- host.WithLibrary(func(library Library) error {
				manifest, err := library.Manifest()
				if err != nil {
					return err
				}
				if endpoint == "/api/maps" {
					manifest.Folders = append(manifest.Folders, "Local")
					manifest.Maps = append(manifest.Maps, Entry{ID: "local", Name: "Local", File: "Local/local.json", Count: 1})
				} else {
					manifest.Maps = append(manifest.Maps, Entry{
						ID: "mma:1", Name: "My name", File: "map-making-app/My folder/My name.json",
						Source: map[string]any{"type": "map-making-app", "mapId": 1, "nameOverride": true, "folderOverride": true},
					})
				}
				return library.Save(manifest)
			})
		}()
		select {
		case err := <-edited:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("network request blocked library edits")
		}
		resume <- struct{}{}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sync did not finish")
	}
	manifest := host.Snapshot()
	if len(manifest.Maps) != 2 || manifest.Maps[0].ID != "local" || len(manifest.Folders) != 1 || manifest.Folders[0] != "Local" {
		t.Fatalf("local edits lost: %#v", manifest)
	}
	entry := manifest.Maps[1]
	if entry.Name != "My name" || entry.File != "map-making-app/My folder/My name.json" {
		t.Fatalf("map overrides lost: %#v", entry)
	}
	if _, err := os.Stat(filepath.Join(host.Directory(), filepath.FromSlash(entry.File))); err != nil {
		t.Fatal(err)
	}
}

func TestAPIRejectsTrailingJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[] {}`))
	}))
	defer upstream.Close()
	service := New(plugintest.NewHost(t), filepath.Join(t.TempDir(), "mma.json"))
	service.baseURL, service.client = upstream.URL, upstream.Client()
	var catalog []mmaRemoteMap
	if err := service.apiGetJSON(context.Background(), "/api/maps", "key", &catalog); err == nil {
		t.Fatal("accepted multiple JSON values")
	}
}

func TestSyncPartialFailureAndRedaction(t *testing.T) {
	host := plugintest.NewHost(t)
	var mode atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "API secret-mma" {
			t.Errorf("authorization = %q", got)
		}
		switch r.URL.Path {
		case "/api/user":
			plugintest.WriteJSON(t, w, map[string]any{"id": 42, "username": "mapper"})
		case "/api/maps":
			if mode.Load() == 2 {
				plugintest.WriteJSON(t, w, []any{})
				return
			}
			plugintest.WriteJSON(t, w, []map[string]any{
				{"id": 1, "name": "One", "folder": "World", "type": "locations", "storage": "active", "archivedAt": nil, "locationCount": 1},
				{"id": 2, "name": "Two", "folder": nil, "type": "locations", "storage": "active", "archivedAt": nil, "locationCount": 1},
				{"id": 3, "name": "Archived", "type": "locations", "storage": "active", "archivedAt": "now", "locationCount": 1},
			})
		case "/api/maps/1/locations":
			if mode.Load() == 1 {
				w.WriteHeader(http.StatusBadRequest)
				plugintest.WriteJSON(t, w, map[string]any{"message": "temporary failure"})
				return
			}
			plugintest.WriteJSON(t, w, []map[string]any{{"lat": 1, "lng": 2}})
		case "/api/maps/2/locations":
			w.WriteHeader(http.StatusBadRequest)
			plugintest.WriteJSON(t, w, map[string]any{"message": "map unavailable"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	service := New(host, filepath.Join(t.TempDir(), "mma.json"))
	service.baseURL, service.client = upstream.URL, upstream.Client()
	if _, err := service.saveKey("secret-mma"); err != nil {
		t.Fatal(err)
	}
	plugintest.WaitUntil(t, func() bool { return !service.publicStatus()["running"].(bool) })
	status := service.publicStatus()
	if status["phase"] != "complete" {
		t.Fatalf("status = %#v", status)
	}
	result := status["lastResult"].(map[string]any)
	if result["updated"] != 1 || result["failed"] != 1 || result["total"] != 2 {
		t.Fatalf("result = %#v", result)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-mma") {
		t.Fatal("public status exposed the API key")
	}
	manifest := host.Snapshot()
	if len(manifest.Maps) != 1 || manifest.Maps[0].ID != "mma:1" || manifest.Maps[0].File != "map-making-app/World/One.json" {
		t.Fatalf("manifest = %#v", manifest.Maps)
	}
	if managed, _ := manifest.Maps[0].Source["managed"].(bool); !managed {
		t.Fatalf("managed source metadata = %#v", manifest.Maps[0].Source)
	}
	if _, err := os.Stat(filepath.Join(host.Directory(), filepath.FromSlash(manifest.Maps[0].File))); err != nil {
		t.Fatal(err)
	}

	if err := host.DeleteMap("mma:1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.start(); err != nil {
		t.Fatal(err)
	}
	plugintest.WaitUntil(t, func() bool { return !service.publicStatus()["running"].(bool) })
	if manifest = host.Snapshot(); len(manifest.Maps) != 1 || manifest.Maps[0].ID != "mma:1" {
		t.Fatalf("sync did not restore deleted MMA map: %#v", manifest.Maps)
	}

	mode.Store(1)
	if _, err := service.start(); err != nil {
		t.Fatal(err)
	}
	plugintest.WaitUntil(t, func() bool { return !service.publicStatus()["running"].(bool) })
	if manifest = host.Snapshot(); len(manifest.Maps) != 1 || manifest.Maps[0].ID != "mma:1" {
		t.Fatalf("failed refresh did not retain last good map: %#v", manifest.Maps)
	}

	mode.Store(2)
	if _, err := service.start(); err != nil {
		t.Fatal(err)
	}
	plugintest.WaitUntil(t, func() bool { return !service.publicStatus()["running"].(bool) })
	if result := service.publicStatus()["lastResult"].(map[string]any); result["removed"] != 1 {
		t.Fatalf("stale result = %#v", result)
	}
	if manifest = host.Snapshot(); len(manifest.Maps) != 0 {
		t.Fatalf("stale map was not removed: %#v", manifest.Maps)
	}
	if _, err := service.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	config := service.loadConfigLocked()
	service.mu.Unlock()
	if config.Enabled || config.APIKey != "secret-mma" || config.Username != "mapper" {
		t.Fatalf("config = %#v", config)
	}
}

func TestCancellation(t *testing.T) {
	host := plugintest.NewHost(t)
	started := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/maps":
			plugintest.WriteJSON(t, w, []map[string]any{{
				"id": 1, "name": "Slow", "type": "locations", "storage": "active", "archivedAt": nil, "locationCount": 1,
			}})
		case "/api/maps/1/locations":
			once.Do(func() { close(started) })
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	service := New(host, filepath.Join(t.TempDir(), "mma.json"))
	service.baseURL, service.client = upstream.URL, upstream.Client()
	service.mu.Lock()
	if err := service.saveConfigLocked(mmaConfig{Version: 1, Enabled: true, APIKey: "key"}); err != nil {
		service.mu.Unlock()
		t.Fatal(err)
	}
	service.mu.Unlock()
	if _, err := service.start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not start")
	}
	service.cancel()
	plugintest.WaitUntil(t, func() bool { return !service.publicStatus()["running"].(bool) })
	if phase := service.publicStatus()["phase"]; phase != "cancelled" {
		t.Fatalf("phase = %v", phase)
	}
}
