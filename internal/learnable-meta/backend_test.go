package learnablemeta

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ohne-b/OhneGuessr/internal/plugintest"
)

func TestLifecycleSyncAndClues(t *testing.T) {
	host := plugintest.NewHost(t)
	var version atomic.Int32
	var failLocations atomic.Bool
	version.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/locations"):
			if got := r.Header.Get("Authorization"); got != "Bearer secret-lm" {
				t.Errorf("authorization = %q", got)
			}
			if failLocations.Load() {
				w.WriteHeader(http.StatusBadRequest)
				plugintest.WriteJSON(t, w, map[string]string{"error": "temporary"})
				return
			}
			latitude := float64(version.Load())
			plugintest.WriteJSON(t, w, map[string]any{"customCoordinates": []map[string]any{
				{"lat": latitude, "lng": 2, "panoId": "pano", "heading": 90},
				{"lat": latitude, "lng": 2, "panoId": "pano"},
				{"lat": 1000, "lng": 2, "panoId": "bad"},
			}})
		case r.URL.Path == "/api/userscript/location":
			plugintest.WriteJSON(t, w, map[string]any{
				"country": "DE", "metaName": "Bollard", "note": "note", "footer": "footer",
				"images": []any{"one", 3, "two"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	service := New(host, filepath.Join(t.TempDir(), "lm.json"))
	service.baseURL, service.client = upstream.URL, upstream.Client()
	if _, err := service.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.saveKey("secret-lm"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.addMap("demo", "Demo Map"); err != nil {
		t.Fatal(err)
	}
	status := service.publicStatus()
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "secret-lm") {
		t.Fatal("public status exposed the API key")
	}
	manifest := host.Snapshot()
	if len(manifest.Maps) != 1 || manifest.Maps[0].ID != learnableEntryID("demo") || manifest.Maps[0].Count != 1 {
		t.Fatalf("manifest = %#v", manifest.Maps)
	}
	if _, err := service.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	config := service.loadConfigLocked()
	service.mu.Unlock()
	if config.Enabled || config.APIKey != "secret-lm" || len(config.Maps) != 1 || config.Maps[0].MapID != "demo" {
		t.Fatalf("config = %#v", config)
	}
	if _, err := service.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	clue, err := service.getClue("demo", "pano")
	if err != nil {
		t.Fatal(err)
	}
	images := clue["images"].([]string)
	if clue["metaName"] != "Bollard" || len(images) != 2 {
		t.Fatalf("clue = %#v", clue)
	}
	if _, err := service.renameMap("demo", "Renamed"); err != nil {
		t.Fatal(err)
	}
	version.Store(2)
	if _, err := service.start(); err != nil {
		t.Fatal(err)
	}
	plugintest.WaitUntil(t, func() bool { return !service.publicStatus()["running"].(bool) })
	if result := service.publicStatus()["lastResult"].(map[string]any); result["updated"] != 1 || result["failed"] != 0 {
		t.Fatalf("sync result = %#v", result)
	}
	manifest = host.Snapshot()
	if len(manifest.Maps) != 1 || manifest.Maps[0].Name != "Renamed" || !strings.Contains(manifest.Maps[0].File, "Renamed-") {
		t.Fatalf("renamed manifest = %#v", manifest.Maps)
	}
	mapBytes, err := os.ReadFile(filepath.Join(host.Directory(), filepath.FromSlash(manifest.Maps[0].File)))
	if err != nil || !strings.Contains(string(mapBytes), `"lat":2`) {
		t.Fatalf("updated map = %s, %v", mapBytes, err)
	}
	failLocations.Store(true)
	if _, err := service.start(); err != nil {
		t.Fatal(err)
	}
	plugintest.WaitUntil(t, func() bool { return !service.publicStatus()["running"].(bool) })
	if result := service.publicStatus()["lastResult"].(map[string]any); result["failed"] != 1 {
		t.Fatalf("failed sync result = %#v", result)
	}
	retained, err := os.ReadFile(filepath.Join(host.Directory(), filepath.FromSlash(manifest.Maps[0].File)))
	if err != nil || !strings.Contains(string(retained), `"lat":2`) {
		t.Fatalf("last good map was not retained: %s, %v", retained, err)
	}
	if _, err := service.removeMap("demo"); err != nil {
		t.Fatal(err)
	}
	if manifest = host.Snapshot(); len(manifest.Maps) != 0 {
		t.Fatalf("map was not removed: %#v", manifest.Maps)
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()
	locations, err := normalizeLearnableLocations([]map[string]any{
		{"lat": 1.0, "lng": 2.0, "panoid": "one", "zoom": 3.0},
		{"lat": math.NaN(), "lng": 2.0, "panoId": "bad"},
	})
	if err != nil || len(locations) != 1 || locations[0]["panoId"] != "one" {
		t.Fatalf("locations = %#v, %v", locations, err)
	}
	if _, err := cleanLearnableMapID("bad/id"); err == nil {
		t.Fatal("invalid map ID was accepted")
	}
}
