package learnablemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
	"github.com/ohne-b/OhneGuessr/internal/mapfile"
)

func (s *Backend) start() (map[string]any, error) {
	ctx, release, err := s.host.AcquireSync(learnableJobName)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	config := s.loadConfigLocked()
	if err := requireLearnableReady(config); err != nil {
		s.mu.Unlock()
		release()
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	if len(config.Maps) == 0 {
		s.mu.Unlock()
		release()
		return nil, httpjson.Error(http.StatusBadRequest, "Add a Learnable Meta map first")
	}
	maps := append([]learnableConfigMap(nil), config.Maps...)
	s.runtime = syncRuntime{Running: true, Phase: "starting", Total: len(maps)}
	status := s.publicStatusLocked()
	s.mu.Unlock()
	go s.run(ctx, release, config.APIKey, maps)
	return status, nil
}

func (s *Backend) cancel() {
	if s.host.CancelSync(learnableJobName) {
		s.mu.Lock()
		if s.runtime.Running {
			s.runtime.Phase = "cancelling"
			s.runtime.Error = nil
		}
		s.mu.Unlock()
	}
}

func (s *Backend) run(ctx context.Context, release func(), key string, maps []learnableConfigMap) {
	defer release()
	updated, unchanged := 0, 0
	failures := make([]map[string]any, 0)
	for index, item := range maps {
		if ctx.Err() != nil {
			break
		}
		s.mu.Lock()
		s.runtime.Phase = "downloading"
		s.runtime.Completed = index
		s.mu.Unlock()
		raw, err := s.fetchLocations(ctx, item.MapID, key)
		var locations []map[string]any
		if err == nil {
			locations, err = normalizeLearnableLocations(raw)
		}
		if err == nil && ctx.Err() == nil {
			var changed bool
			changed, err = s.publishLearnableMap(item.MapID, item.Name, locations)
			if changed {
				updated++
			} else if err == nil {
				unchanged++
			}
		}
		if err != nil && ctx.Err() == nil {
			failures = append(failures, map[string]any{"mapId": item.MapID, "error": err.Error()})
		}
		s.mu.Lock()
		s.runtime.Completed = index + 1
		s.mu.Unlock()
	}
	result := learnableResult(len(maps), updated, unchanged, failures)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		s.runtime.Running = false
		s.runtime.Phase = "cancelled"
		s.runtime.Error = nil
		s.runtime.LastResult = result
		return
	}
	config := s.loadConfigLocked()
	if config.APIKey != "" {
		config.LastSyncAt = utcNow()
		_ = s.saveConfigLocked(config)
	}
	s.runtime.Running = false
	s.runtime.Phase = "complete"
	s.runtime.Completed = len(maps)
	s.runtime.Error = nil
	s.runtime.LastResult = result
}

func learnableResult(total, updated, unchanged int, failures []map[string]any) map[string]any {
	if failures == nil {
		failures = []map[string]any{}
	}
	return map[string]any{
		"total": total, "updated": updated, "unchanged": unchanged,
		"failed": len(failures), "failures": failures,
	}
}

func stableLearnableHash(mapID string) string {
	digest := sha256.Sum256([]byte(mapID))
	return hex.EncodeToString(digest[:])
}

func learnableEntryID(mapID string) string {
	return "learnable-meta:" + stableLearnableHash(mapID)[:24]
}

func learnableTarget(mapID, name string, fullHash bool) string {
	hash := stableLearnableHash(mapID)
	if !fullHash {
		hash = hash[:16]
	}
	return path.Join(learnableRoot, mapfile.SafeComponent(name, "Untitled map")+"-"+hash+".json")
}

func (s *Backend) publishLearnableMap(mapID, name string, locations []map[string]any) (bool, error) {
	encoded, err := json.Marshal(locations)
	if err != nil {
		return false, err
	}
	checksum := mapfile.Checksum(encoded)
	var changed bool
	err = s.host.WithLibrary(func(library Library) error {
		changed, err = publishLearnableMapLocked(library, mapID, name, locations, encoded, checksum)
		return err
	})
	return changed, err
}

func publishLearnableMapLocked(library Library, mapID, name string, locations []map[string]any, encoded []byte, checksum string) (bool, error) {
	manifest, err := library.Manifest()
	if err != nil {
		return false, err
	}
	index := -1
	for i, entry := range manifest.Maps {
		if mapfile.SourceType(entry.Source) == "learnable-meta" && entry.Source["mapId"] == mapID {
			index = i
			break
		}
	}
	target := learnableTarget(mapID, name, false)
	for i, entry := range manifest.Maps {
		if i != index && strings.EqualFold(entry.File, target) {
			target = learnableTarget(mapID, name, true)
			break
		}
	}
	filename, err := library.Resolve(target)
	if err != nil {
		return false, err
	}
	existing := Entry{}
	if index >= 0 {
		existing = manifest.Maps[index]
	}
	same := index >= 0 && existing.Checksum == checksum && strings.EqualFold(existing.File, target)
	if same {
		_, err = os.Stat(filename)
		same = err == nil
	}
	if !same {
		if err := mapfile.Write(filename, encoded, 0o644); err != nil {
			return false, err
		}
	}
	if _, err := os.Stat(filename); err != nil {
		return false, err
	}
	entry := Entry{
		ID: learnableEntryID(mapID), Name: name, File: target, Count: len(locations),
		Checksum: checksum,
		Source:   map[string]any{"type": "learnable-meta", "managed": true, "mapId": mapID},
	}
	if index >= 0 {
		manifest.Maps[index] = entry
	} else {
		manifest.Maps = append(manifest.Maps, entry)
	}
	err = library.Save(manifest)
	if err != nil {
		return false, err
	}
	if index >= 0 && !strings.EqualFold(existing.File, target) {
		if old, resolveErr := library.Resolve(existing.File); resolveErr == nil {
			_ = os.Remove(old)
		}
	}
	return !same, nil
}

func (s *Backend) renamePublishedLearnableMap(mapID, name string) error {
	return s.host.WithLibrary(func(library Library) error {
		return renamePublishedLearnableMapLocked(library, mapID, name)
	})
}

func renamePublishedLearnableMapLocked(library Library, mapID, name string) error {
	manifest, err := library.Manifest()
	if err != nil {
		return err
	}
	index := -1
	for i, entry := range manifest.Maps {
		if mapfile.SourceType(entry.Source) == "learnable-meta" && entry.Source["mapId"] == mapID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	entry := &manifest.Maps[index]
	oldFile := entry.File
	newFile := learnableTarget(mapID, name, false)
	for i, other := range manifest.Maps {
		if i != index && strings.EqualFold(other.File, newFile) {
			newFile = learnableTarget(mapID, name, true)
			break
		}
	}
	oldPath, err := library.Resolve(oldFile)
	if err != nil {
		return err
	}
	if _, err := os.Stat(oldPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w for %q", errMapDataMissing, entry.Name)
		}
		return err
	}
	newPath, err := library.Resolve(newFile)
	if err != nil {
		return err
	}
	moved := false
	if oldFile != newFile {
		if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
			return err
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		moved = true
	}
	entry.Name = name
	entry.File = newFile
	err = library.Save(manifest)
	if err != nil && moved {
		_ = os.Rename(newPath, oldPath)
	}
	return err
}

func (s *Backend) deletePublishedLearnableMap(mapID string) error {
	return s.host.WithLibrary(func(library Library) error {
		return deletePublishedLearnableMapLocked(library, mapID)
	})
}

func deletePublishedLearnableMapLocked(library Library, mapID string) error {
	manifest, err := library.Manifest()
	if err != nil {
		return err
	}
	targets := make([]Entry, 0)
	kept := make([]Entry, 0, len(manifest.Maps))
	for _, entry := range manifest.Maps {
		if mapfile.SourceType(entry.Source) == "learnable-meta" && entry.Source["mapId"] == mapID {
			targets = append(targets, entry)
		} else {
			kept = append(kept, entry)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	previous := manifest
	manifest.Maps = kept
	manifest.Folders = mapfile.FoldersOutsideRoot(manifest.Folders, learnableRoot)
	if err := library.Save(manifest); err != nil {
		return err
	}
	for _, entry := range targets {
		filename, err := library.Resolve(entry.File)
		if err != nil {
			_ = library.Save(previous)
			return err
		}
		if err := os.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = library.Save(previous)
			return err
		}
	}
	mapfile.RemoveEmptyDirectories(library.Directory(), learnableRoot)
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func utcNow() string { return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339) }
