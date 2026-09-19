package backend

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

func storageTestStore(t *testing.T, policies ...pluginhost.MapPolicy) *mapStore {
	t.Helper()
	store, err := newMapStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		store.registerMapPolicy(policy)
	}
	if err := store.initialize(); err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func storageManifest(t *testing.T, store *mapStore) mapManifest {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	manifest, err := store.loadManifestLocked()
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func saveStorageManifest(t *testing.T, store *mapStore, manifest mapManifest) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.saveManifestLocked(manifest); err != nil {
		t.Fatal(err)
	}
}

func TestSafeNamesAndPaths(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		`  A  map  `: "A map",
		`CON`:        "CON-map",
		`a<b>:c`:     "a-b--c",
		`...`:        "Untitled",
	}
	for input, want := range tests {
		if got := mapfile.SafeComponent(input, "Untitled"); got != want {
			t.Errorf("safeComponent(%q) = %q, want %q", input, got, want)
		}
	}
	if got := slugify("  My Great Map!  "); got != "my-great-map" {
		t.Fatalf("slugify = %q", got)
	}
	for _, invalid := range []string{"../outside", "one/../../outside", `one\..\outside`, "/absolute", `C:\absolute`} {
		if _, err := normalizeRelative(invalid); err == nil {
			t.Errorf("normalizeRelative(%q) accepted traversal", invalid)
		}
	}
	for _, invalid := range []string{"", "CON", "bad/name", "trailing.", ".hidden"} {
		if _, err := validateFolderName(invalid); err == nil {
			t.Errorf("validateFolderName(%q) accepted invalid name", invalid)
		}
	}
}

func TestManifestInitializationAndStrictLoading(t *testing.T) {
	store := storageTestStore(t)
	if manifest := storageManifest(t, store); len(manifest.Maps) != 0 || len(manifest.Folders) != 0 {
		t.Fatalf("fresh manifest = %#v", manifest)
	}
	if err := os.WriteFile(filepath.Join(store.dir, "external.json"), []byte(`[{"lat":1,"lng":2}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest := storageManifest(t, store); len(manifest.Maps) != 0 {
		t.Fatalf("external file was indexed: %#v", manifest.Maps)
	}
	if err := os.WriteFile(store.manifestPath, []byte(`{
		"version": 2,
		"folders": [],
		"maps": [{"id":"old","name":"Old","file":"old.json","count":1,"size":123,"mtimeNs":456}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest := storageManifest(t, store); len(manifest.Maps) != 1 || manifest.Maps[0].ID != "old" {
		t.Fatalf("legacy v2 manifest metadata was not accepted: %#v", manifest)
	}
	if err := os.WriteFile(store.manifestPath, []byte(`{"version":2`), 0o644); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	_, err := store.loadManifestLocked()
	store.mu.Unlock()
	if err == nil {
		t.Fatal("corrupt manifest was accepted")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orphan.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	missing, err := newMapStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Close()
	if err := missing.initialize(); err == nil {
		t.Fatal("missing manifest beside existing data was replaced")
	}
	if _, err := os.Stat(filepath.Join(dir, manifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing manifest was written: %v", err)
	}
}

func TestMapStoreLifecycleAndUnsupportedExternalMove(t *testing.T) {
	store := storageTestStore(t)
	entry, err := store.createLocal("My Map", json.RawMessage(`[{"lat":1,"lng":2}]`), "")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Count != 1 || entry.File != "my-map.json" || entry.ID == "" || entry.Checksum != "" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
	renamed, err := store.updateMap(entry.ID, pointer("Renamed"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.File != "renamed.json" {
		t.Fatalf("renamed file = %q", renamed.File)
	}
	destination := filepath.Join(store.dir, "External", "moved.json")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(store.dir, renamed.File), destination); err != nil {
		t.Fatal(err)
	}
	manifest := storageManifest(t, store)
	if len(manifest.Maps) != 1 || manifest.Maps[0].File != "renamed.json" || len(manifest.Folders) != 0 {
		t.Fatalf("external move changed manifest: %#v", manifest)
	}
	if _, err := store.updateMap(entry.ID, pointer("Again"), nil); !errors.Is(err, errMapDataMissing) {
		t.Fatalf("missing-data rename error = %v", err)
	}
	if err := store.deleteLocal(entry.ID); err != nil {
		t.Fatal(err)
	}
	if manifest := storageManifest(t, store); len(manifest.Maps) != 0 {
		t.Fatalf("stale entry was not removed: %#v", manifest.Maps)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("untracked external file was removed: %v", err)
	}
}

func TestFolderAndMapMutations(t *testing.T) {
	store := storageTestStore(t)
	parent, err := store.createFolder("", "Trips")
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.createFolder(parent, "Europe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.createFolder("", "trips"); !errors.Is(err, errFolderExists) {
		t.Fatalf("case-insensitive collision error = %v", err)
	}
	caseChild, err := store.createFolder("trips", "Case child")
	if err != nil || caseChild != "Trips/Case child" {
		t.Fatalf("canonical parent folder = %q, %v", caseChild, err)
	}
	if _, err := store.deleteFolder("TRIPS/CASE CHILD", false); err != nil {
		t.Fatalf("case-insensitive folder delete: %v", err)
	}
	entry, err := store.createLocal("Capitals", json.RawMessage(`[{"lat":1,"lng":2}]`), child)
	if err != nil {
		t.Fatal(err)
	}
	if entry.File != "Trips/Europe/capitals.json" {
		t.Fatalf("selected-folder import = %q", entry.File)
	}
	if _, err := store.deleteFolder(parent, false); !errors.Is(err, errFolderNotEmpty) {
		t.Fatalf("non-empty folder delete error = %v", err)
	}

	destination, err := store.createFolder("", "Archive")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.createLocal("Capitals", json.RawMessage(`[{"lat":3,"lng":4}]`), destination)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := store.updateMap(entry.ID, nil, &destination)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != entry.ID || moved.File != "Archive/capitals-2.json" {
		t.Fatalf("collision-safe move = %#v", moved)
	}
	if _, err := os.Stat(filepath.Join(store.dir, filepath.FromSlash(other.File))); err != nil {
		t.Fatalf("existing destination was overwritten: %v", err)
	}

	renamed, err := store.renameFolder(parent, "Journeys")
	if err != nil || renamed != "Journeys" {
		t.Fatalf("renamed folder = %q, %v", renamed, err)
	}
	if _, err := os.Stat(filepath.Join(store.dir, "Journeys", "Europe")); err != nil {
		t.Fatalf("empty descendant folder was not moved: %v", err)
	}
	manifest := storageManifest(t, store)
	if !hasFolder(manifest, "Journeys/Europe") {
		t.Fatalf("empty descendant missing from manifest: %#v", manifest.Folders)
	}
	if _, err := store.deleteFolder("Journeys/Europe", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.deleteFolder("Journeys", false); err != nil {
		t.Fatal(err)
	}
}

func TestRecursiveFolderDelete(t *testing.T) {
	store := storageTestStore(t)
	parent, err := store.createFolder("", "Delete me")
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.createFolder(parent, "Nested")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.createLocal("First", json.RawMessage(`[{"lat":1,"lng":2}]`), parent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.createLocal("Second", json.RawMessage(`[{"lat":3,"lng":4}]`), child)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := store.deleteFolder(parent, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || deleted[0] != first.ID || deleted[1] != second.ID {
		t.Fatalf("deleted map IDs = %#v", deleted)
	}
	if _, err := os.Stat(filepath.Join(store.dir, "Delete me")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted folder still exists: %v", err)
	}
	manifest := storageManifest(t, store)
	if len(manifest.Maps) != 0 || len(manifest.Folders) != 0 {
		t.Fatalf("manifest retained deleted content: %#v", manifest)
	}
}

func TestManagedMoveRules(t *testing.T) {
	const managedType = "editable-sync"
	const managedRoot = "editable-sync"
	const lockedType = "locked-sync"
	const lockedRoot = "Locked Sync"
	store := storageTestStore(t, pluginhost.MapPolicy{
		SourceType: managedType, Root: managedRoot, EditableFolders: true,
		RenameMaps: true, MoveMaps: true, DeleteMaps: true,
		Filename: func(name string) string { return mapfile.SafeComponent(name, "Untitled map") + ".json" },
		UpdateSource: func(source map[string]any, renamed, moved bool) map[string]any {
			source = maps.Clone(source)
			if renamed {
				source["nameOverride"] = true
			}
			if moved {
				source["folderOverride"] = true
			}
			return source
		},
	}, pluginhost.MapPolicy{
		SourceType: lockedType, Root: lockedRoot,
	})
	local, err := store.createLocal("Local", json.RawMessage(`[{"lat":1,"lng":2}]`), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{managedRoot, path.Join(managedRoot, "Custom"), lockedRoot} {
		if err := os.MkdirAll(filepath.Join(store.dir, filepath.FromSlash(folder)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	managedFile := path.Join(managedRoot, "Managed.json")
	lockedFile := path.Join(lockedRoot, "Locked.json")
	if err := os.WriteFile(filepath.Join(store.dir, filepath.FromSlash(managedFile)), []byte(`[{"lat":1,"lng":2}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.dir, filepath.FromSlash(lockedFile)), []byte(`[{"lat":1,"lng":2}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := storageManifest(t, store)
	manifest.Folders = append(manifest.Folders, managedRoot, path.Join(managedRoot, "Custom"), lockedRoot)
	manifest.Maps = append(manifest.Maps,
		mapEntry{
			ID: "editable:1", Name: "Managed", File: managedFile, Count: 1,
			Source: map[string]any{"type": managedType, "managed": true, "mapId": 1},
		},
		mapEntry{
			ID: "locked:1", Name: "Locked", File: lockedFile, Count: 1,
			Source: map[string]any{"type": lockedType, "managed": true, "mapId": 1},
		},
	)
	saveStorageManifest(t, store, manifest)

	if _, err := store.updateMap(local.ID, nil, pointer(path.Join(managedRoot, "Custom"))); !errors.Is(err, errMoveRestricted) {
		t.Fatalf("local managed-root move error = %v", err)
	}
	name := "My managed map"
	folder := path.Join(managedRoot, "Custom")
	updated, err := store.updateMap("editable:1", &name, &folder)
	if err != nil {
		t.Fatal(err)
	}
	nameOverride, _ := updated.Source["nameOverride"].(bool)
	folderOverride, _ := updated.Source["folderOverride"].(bool)
	if !mapfile.UnderRoot(updated.File, folder) || !nameOverride || !folderOverride {
		t.Fatalf("managed overrides = %#v", updated)
	}
	if _, err := store.createFolder(lockedRoot, "Nope"); !errors.Is(err, errManagedFolder) {
		t.Fatalf("locked managed folder create error = %v", err)
	}
	if _, err := store.updateMap("locked:1", &name, nil); !errors.Is(err, errManagedMap) {
		t.Fatalf("locked managed rename error = %v", err)
	}
	if err := store.deleteLocal("locked:1"); !errors.Is(err, errManagedMap) {
		t.Fatalf("locked managed delete error = %v", err)
	}
}

func TestPortableZIPExport(t *testing.T) {
	store := storageTestStore(t)
	trips, err := store.createFolder("", "Trips")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.createFolder(trips, "Empty"); err != nil {
		t.Fatal(err)
	}
	rootMap, err := store.createLocal("Root", json.RawMessage(`[{"lat":1,"lng":2}]`), "")
	if err != nil {
		t.Fatal(err)
	}
	nestedMap, err := store.createLocal("Nested", json.RawMessage(`[{"lat":3,"lng":4}]`), trips)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "maps.zip")
	if err := store.exportZIP(filename); err != nil {
		t.Fatal(err)
	}
	if err := store.exportZIP(filename); err != nil {
		t.Fatalf("overwrite export: %v", err)
	}
	archive, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	contents := map[string]string{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			contents[file.Name] = ""
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		contents[file.Name] = string(body)
	}
	for _, name := range []string{"Trips/", "Trips/Empty/", rootMap.File, nestedMap.File} {
		if _, ok := contents[name]; !ok {
			t.Errorf("archive missing %q: %#v", name, contents)
		}
	}
	if _, ok := contents[manifestName]; ok {
		t.Fatal("archive included maps.json")
	}
	if !strings.Contains(contents[nestedMap.File], `"lat":3`) {
		t.Fatalf("nested map contents = %q", contents[nestedMap.File])
	}

	if err := os.Remove(filepath.Join(store.dir, filepath.FromSlash(nestedMap.File))); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.zip")
	if err := store.exportZIP(missing); !errors.Is(err, errMapDataMissing) {
		t.Fatalf("missing-data export error = %v", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed export left destination: %v", err)
	}
	if temporary, err := filepath.Glob(filepath.Join(filepath.Dir(missing), ".ohneguessr-export-*.tmp")); err != nil || len(temporary) != 0 {
		t.Fatalf("failed export left temporary files: %#v, %v", temporary, err)
	}
}

func pointer(value string) *string { return &value }
