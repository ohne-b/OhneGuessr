package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

const (
	manifestVersion = 2
	manifestName    = "maps.json"
)

type mapEntry = pluginhost.Entry

type mapManifest struct {
	Version int        `json:"version"`
	Folders []string   `json:"folders"`
	Maps    []mapEntry `json:"maps"`
}

func emptyManifest() mapManifest {
	return mapManifest{Version: manifestVersion, Folders: []string{}, Maps: []mapEntry{}}
}

func (s *mapStore) initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.loadManifestLocked(); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("maps.json is missing from the non-empty maps directory")
	}
	return s.saveManifestLocked(emptyManifest())
}

func (s *mapStore) loadManifestLocked() (mapManifest, error) {
	raw, err := os.ReadFile(s.manifestPath)
	if err != nil {
		return mapManifest{}, err
	}
	var decoded mapManifest
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return mapManifest{}, fmt.Errorf("maps.json is invalid: %w", err)
	}
	if decoded.Version != manifestVersion {
		return mapManifest{}, fmt.Errorf("maps.json version %d is unsupported", decoded.Version)
	}
	return cleanManifest(decoded)
}

func cleanManifest(manifest mapManifest) (mapManifest, error) {
	clean := emptyManifest()
	folders := map[string]string{}
	ids := map[string]bool{}
	files := map[string]bool{}
	for _, entry := range manifest.Maps {
		rel, err := normalizeRelative(entry.File)
		if err != nil || rel == "" || entry.ID == "" || !strings.EqualFold(path.Ext(rel), ".json") {
			return mapManifest{}, errors.New("maps.json contains an invalid map entry")
		}
		fileKey := strings.ToLower(rel)
		if ids[entry.ID] || files[fileKey] {
			return mapManifest{}, errors.New("maps.json contains duplicate map entries")
		}
		ids[entry.ID], files[fileKey] = true, true
		entry.File = rel
		if entry.Name == "" {
			entry.Name = strings.TrimSpace(strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
			if entry.Name == "" {
				entry.Name = entry.ID
			}
		}
		clean.Maps = append(clean.Maps, entry)
		addFolderParents(folders, mapfile.Folder(rel))
	}
	for _, folder := range manifest.Folders {
		rel, err := normalizeRelative(folder)
		if err != nil || rel == "" {
			return mapManifest{}, errors.New("maps.json contains an invalid folder")
		}
		folders[strings.ToLower(rel)] = rel
	}
	clean.Folders = folderValues(folders)
	return clean, nil
}

func (s *mapStore) saveManifestLocked(manifest mapManifest) error {
	clean, err := cleanManifest(manifest)
	if err != nil {
		return err
	}
	return mapfile.WriteJSON(s.manifestPath, clean, 0o644)
}
