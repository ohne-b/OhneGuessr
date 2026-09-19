package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
	learnablemeta "github.com/ohne-b/OhneGuessr/internal/learnable-meta"
	mapmakingapp "github.com/ohne-b/OhneGuessr/internal/map-making-app"
)

func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	a, err := New(t.TempDir(), mapmakingapp.NewPlugin, learnablemeta.NewPlugin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	})
	return a
}

func localRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func perform(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestHTTPMapsAndInternalRouting(t *testing.T) {
	a := newTestBackend(t)
	handler := a.Handler()

	manifest := perform(handler, localRequest(http.MethodGet, "/data/maps.json", ""))
	if manifest.Code != http.StatusOK || manifest.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("manifest = %d %#v", manifest.Code, manifest.Header())
	}

	create := perform(handler, localRequest(http.MethodPost, "/api/maps", `{"name":"Test","locations":[{"lat":1,"lng":2},{"location":{"lat":3,"lng":4},"flags":1,"panoId":"nested"},{"location":{"lat":5,"lng":6},"flags":2}]}`))
	if create.Code != http.StatusOK {
		t.Fatalf("create = %d %s", create.Code, create.Body.String())
	}
	var entry mapEntry
	if err := json.Unmarshal(create.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	data := perform(handler, localRequest(http.MethodGet, "/data/"+entry.File, ""))
	if data.Code != http.StatusOK || !strings.Contains(data.Body.String(), `"lat":1`) {
		t.Fatalf("data = %d %s", data.Code, data.Body.String())
	}
	rounds := perform(handler, localRequest(
		http.MethodPost,
		"/api/maps/"+entry.ID+"/rounds",
		`{"count":10}`,
	))
	var sample mapSample
	if rounds.Code != http.StatusOK || json.Unmarshal(rounds.Body.Bytes(), &sample) != nil {
		t.Fatalf("rounds = %d %s", rounds.Code, rounds.Body.String())
	}
	if sample.LocationCount != 2 || len(sample.Locations) != 2 || sample.MapDiagonalKM <= 0 {
		t.Fatalf("sample = %#v", sample)
	}
	byIndex := map[int]sampledLocation{}
	for _, location := range sample.Locations {
		byIndex[location.SourceIndex] = location
	}
	if byIndex[0].Lat != 1 || byIndex[1].Panoid == nil || *byIndex[1].Panoid != "nested" {
		t.Fatalf("sample locations = %#v", byIndex)
	}
	excluded := perform(handler, localRequest(
		http.MethodPost,
		"/api/maps/"+entry.ID+"/rounds",
		`{"count":1,"exclude":[0,1]}`,
	))
	if excluded.Code != http.StatusOK || !strings.Contains(excluded.Body.String(), `"locations":[]`) {
		t.Fatalf("excluded rounds = %d %s", excluded.Code, excluded.Body.String())
	}
	if got := perform(handler, localRequest(http.MethodGet, "/api/health", "")).Code; got != http.StatusNotFound {
		t.Fatalf("removed health endpoint status = %d", got)
	}
}

func TestHTTPRejectsBadBodiesAndPrivateData(t *testing.T) {
	a := newTestBackend(t)
	handler := a.Handler()

	wrongType := localRequest(http.MethodPost, "/api/maps", `{"name":"x","locations":[]}`)
	wrongType.Header.Set("Content-Type", "text/plain")
	if got := perform(handler, wrongType).Code; got != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong content type = %d", got)
	}
	trailing := localRequest(http.MethodPost, "/api/maps", `{"name":"x","locations":[]} {}`)
	if got := perform(handler, trailing).Code; got != http.StatusBadRequest {
		t.Fatalf("trailing JSON = %d", got)
	}
	empty := localRequest(http.MethodPost, "/api/maps", `{"name":"x","locations":[]}`)
	if got := perform(handler, empty).Code; got != http.StatusBadRequest {
		t.Fatalf("empty map = %d", got)
	}
	tooLarge := localRequest(http.MethodPost, "/api/maps", `{}`)
	tooLarge.ContentLength = httpjson.MaxBodySize + 1
	if got := perform(handler, tooLarge).Code; got != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d", got)
	}
	private := perform(handler, localRequest(http.MethodGet, "/data/.private.json", ""))
	if private.Code != http.StatusNotFound {
		t.Fatalf("private data = %d", private.Code)
	}
	unknownAPI := perform(handler, localRequest(http.MethodGet, "/api/nope", ""))
	if unknownAPI.Code != http.StatusNotFound || !strings.Contains(unknownAPI.Body.String(), `"error":"not found"`) {
		t.Fatalf("unknown API = %d %s", unknownAPI.Code, unknownAPI.Body.String())
	}
}

func TestHTTPFolderAndMapMutations(t *testing.T) {
	a := newTestBackend(t)
	handler := a.Handler()

	createFolder := perform(handler, localRequest(
		http.MethodPost,
		"/api/folders",
		`{"parent":"","name":"Trips"}`,
	))
	if createFolder.Code != http.StatusOK {
		t.Fatalf("create folder = %d %s", createFolder.Code, createFolder.Body.String())
	}
	duplicate := perform(handler, localRequest(
		http.MethodPost,
		"/api/folders",
		`{"parent":"","name":"trips"}`,
	))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate folder = %d %s", duplicate.Code, duplicate.Body.String())
	}
	traversal := perform(handler, localRequest(
		http.MethodPost,
		"/api/folders",
		`{"parent":"../outside","name":"Nope"}`,
	))
	if traversal.Code != http.StatusBadRequest {
		t.Fatalf("folder traversal = %d %s", traversal.Code, traversal.Body.String())
	}

	createMap := perform(handler, localRequest(
		http.MethodPost,
		"/api/maps",
		`{"name":"Test","folder":"Trips","locations":[{"lat":1,"lng":2}]}`,
	))
	if createMap.Code != http.StatusOK {
		t.Fatalf("selected-folder map = %d %s", createMap.Code, createMap.Body.String())
	}
	var entry mapEntry
	if err := json.Unmarshal(createMap.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.File != "Trips/test.json" {
		t.Fatalf("selected-folder file = %q", entry.File)
	}
	nonEmpty := perform(handler, localRequest(
		http.MethodDelete,
		"/api/folders",
		`{"path":"Trips"}`,
	))
	if nonEmpty.Code != http.StatusConflict {
		t.Fatalf("non-empty folder delete = %d %s", nonEmpty.Code, nonEmpty.Body.String())
	}
	move := perform(handler, localRequest(
		http.MethodPatch,
		"/api/maps/"+entry.ID,
		`{"folder":""}`,
	))
	if move.Code != http.StatusOK {
		t.Fatalf("map move = %d %s", move.Code, move.Body.String())
	}
	var moved mapEntry
	if err := json.Unmarshal(move.Body.Bytes(), &moved); err != nil {
		t.Fatal(err)
	}
	if moved.ID != entry.ID || moved.File != "test.json" {
		t.Fatalf("moved map = %#v", moved)
	}
	deleteFolder := perform(handler, localRequest(
		http.MethodDelete,
		"/api/folders",
		`{"path":"Trips"}`,
	))
	if deleteFolder.Code != http.StatusOK {
		t.Fatalf("empty folder delete = %d %s", deleteFolder.Code, deleteFolder.Body.String())
	}
	recursiveFolder := perform(handler, localRequest(
		http.MethodPost,
		"/api/folders",
		`{"parent":"","name":"Recursive"}`,
	))
	if recursiveFolder.Code != http.StatusOK {
		t.Fatalf("create recursive folder = %d %s", recursiveFolder.Code, recursiveFolder.Body.String())
	}
	recursiveMap := perform(handler, localRequest(
		http.MethodPost,
		"/api/maps",
		`{"name":"Nested","folder":"Recursive","locations":[{"lat":3,"lng":4}]}`,
	))
	if recursiveMap.Code != http.StatusOK {
		t.Fatalf("create recursive map = %d %s", recursiveMap.Code, recursiveMap.Body.String())
	}
	var recursiveEntry mapEntry
	if err := json.Unmarshal(recursiveMap.Body.Bytes(), &recursiveEntry); err != nil {
		t.Fatal(err)
	}
	recursiveDelete := perform(handler, localRequest(
		http.MethodDelete,
		"/api/folders",
		`{"path":"Recursive","recursive":true}`,
	))
	if recursiveDelete.Code != http.StatusOK ||
		!strings.Contains(recursiveDelete.Body.String(), recursiveEntry.ID) {
		t.Fatalf("recursive folder delete = %d %s", recursiveDelete.Code, recursiveDelete.Body.String())
	}
	missing := perform(handler, localRequest(
		http.MethodDelete,
		"/api/maps/missing",
		"",
	))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing map delete = %d %s", missing.Code, missing.Body.String())
	}
}

func TestManagedRootDeleteDisablesSync(t *testing.T) {
	a := newTestBackend(t)
	mma, learnable := a.mapPlugins[0], a.mapPlugins[1]
	mmaPolicy, learnablePolicy := mma.MapPolicy(), learnable.MapPolicy()
	_, err := mma.SetEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = learnable.SetEnabled(true)
	if err != nil {
		t.Fatal(err)
	}

	a.maps.mu.Lock()
	manifest, err := a.maps.loadManifestLocked()
	if err != nil {
		a.maps.mu.Unlock()
		t.Fatal(err)
	}
	manifest.Folders = append(manifest.Folders, mmaPolicy.Root)
	err = a.maps.saveManifestLocked(manifest)
	a.maps.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.deleteFolder(mmaPolicy.Root, true); !errors.Is(err, errFolderNotFound) || !mma.Enabled() {
		t.Fatalf("failed delete did not restore MMA sync: %v", err)
	}

	for _, folder := range []string{mmaPolicy.Root, learnablePolicy.Root} {
		filename := filepath.Join(a.maps.dir, folder, "Map.json")
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(`[{"lat":1,"lng":2}]`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.maps.mu.Lock()
	manifest, err = a.maps.loadManifestLocked()
	if err == nil {
		manifest.Folders = append(manifest.Folders, learnablePolicy.Root)
		manifest.Maps = append(manifest.Maps,
			mapEntry{
				ID: "editable:1", Name: "Editable", File: mmaPolicy.Root + "/Map.json", Count: 1,
				Source: map[string]any{"type": mmaPolicy.SourceType, "mapId": 1},
			},
			mapEntry{
				ID: "locked:demo", Name: "Locked", File: learnablePolicy.Root + "/Map.json", Count: 1,
				Source: map[string]any{"type": learnablePolicy.SourceType, "managed": true, "mapId": "demo"},
			},
		)
		err = a.maps.saveManifestLocked(manifest)
	}
	a.maps.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.deleteFolder(mmaPolicy.Root, false); !errors.Is(err, errFolderNotEmpty) || !mma.Enabled() {
		t.Fatalf("unconfirmed managed delete = %v", err)
	}
	for _, folder := range []string{mmaPolicy.Root, learnablePolicy.Root} {
		deleted, err := a.deleteFolder(folder, true)
		if err != nil || len(deleted) != 1 {
			t.Fatalf("delete %s = %#v, %v", folder, deleted, err)
		}
		if _, err := os.Stat(filepath.Join(a.maps.dir, folder)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", folder, err)
		}
	}

	if mma.Enabled() {
		t.Fatal("MMA sync remained enabled after deleting its managed root")
	}
	if learnable.Enabled() {
		t.Fatal("Learnable Meta sync remained enabled after deleting its managed root")
	}
}
