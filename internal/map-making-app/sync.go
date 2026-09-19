package mapmakingapp

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
	"github.com/ohne-b/OhneGuessr/internal/mapfile"
)

func (s *Backend) start() (map[string]any, error) {
	ctx, release, err := s.host.AcquireSync(mmaJobName)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	config := s.loadConfigLocked()
	if !config.Enabled {
		s.mu.Unlock()
		release()
		return nil, httpjson.Error(http.StatusBadRequest, "Map Making App sync is off")
	}
	if config.APIKey == "" {
		s.mu.Unlock()
		release()
		return nil, httpjson.Error(http.StatusBadRequest, "Save an API key first")
	}
	s.beginLocked()
	status := s.publicStatusLocked()
	s.mu.Unlock()
	go s.run(ctx, release, config.APIKey)
	return status, nil
}

func (s *Backend) beginLocked() {
	s.runtime = syncRuntime{Running: true, Phase: "catalog"}
}

func (s *Backend) cancel() {
	if s.host.CancelSync(mmaJobName) {
		s.mu.Lock()
		if s.runtime.Running {
			s.runtime.Phase = "cancelling"
			s.runtime.Error = nil
		}
		s.mu.Unlock()
	}
}

func (s *Backend) run(ctx context.Context, release func(), key string) {
	defer release()
	result, err := s.syncMaps(ctx, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		s.runtime.Running = false
		s.runtime.Phase = "cancelled"
		s.runtime.Error = nil
		return
	}
	if err != nil {
		s.runtime.Running = false
		s.runtime.Phase = "error"
		s.runtime.Error = err.Error()
		return
	}
	config := s.loadConfigLocked()
	if config.APIKey != "" {
		config.LastSyncAt = utcNow()
		_ = s.saveConfigLocked(config)
	}
	s.runtime.Running = false
	s.runtime.Phase = "complete"
	s.runtime.Completed = result.Total
	s.runtime.Total = result.Total
	s.runtime.Error = nil
	s.runtime.LastResult = result.asMap()
}

func (s *Backend) progress(phase string, completed, total int) {
	s.mu.Lock()
	s.runtime.Phase = phase
	s.runtime.Completed = completed
	s.runtime.Total = total
	s.mu.Unlock()
}

type mmaPlan struct {
	remote   mmaRemoteMap
	existing *Entry
	target   string
	source   map[string]any
}

type mmaSyncResult struct {
	Total     int
	Updated   int
	Unchanged int
	Failed    int
	Removed   int
	Failures  []map[string]any
}

func (r mmaSyncResult) asMap() map[string]any {
	return map[string]any{
		"total": r.Total, "updated": r.Updated, "unchanged": r.Unchanged,
		"failed": r.Failed, "removed": r.Removed, "failures": r.Failures,
	}
}

func (s *Backend) syncMaps(ctx context.Context, key string) (mmaSyncResult, error) {
	s.progress("catalog", 0, 0)
	var catalog []mmaRemoteMap
	if err := s.apiGetJSON(ctx, "/api/maps", key, &catalog); err != nil {
		return mmaSyncResult{}, err
	}
	remotes := catalog[:0]
	for _, remote := range catalog {
		if remote.Type == "locations" && remote.Storage == "active" && remote.ArchivedAt == nil && remote.LocationCount > 0 && remote.ID > 0 {
			remotes = append(remotes, remote)
		}
	}
	sort.Slice(remotes, func(i, j int) bool {
		leftFolder, rightFolder := strings.ToLower(remotes[i].Folder), strings.ToLower(remotes[j].Folder)
		if leftFolder == rightFolder {
			return strings.ToLower(remotes[i].Name) < strings.ToLower(remotes[j].Name)
		}
		return leftFolder < rightFolder
	})

	var staging string
	err := s.host.WithLibrary(func(library Library) error {
		var err error
		staging, err = os.MkdirTemp(library.Directory(), ".mma-sync-")
		return err
	})
	if err != nil {
		return mmaSyncResult{}, err
	}
	defer os.RemoveAll(staging)
	s.progress("downloading", 0, len(remotes))
	downloads := s.downloadMMA(ctx, key, staging, remotes)
	if err := ctx.Err(); err != nil {
		return mmaSyncResult{}, err
	}
	var result mmaSyncResult
	err = s.host.WithLibrary(func(library Library) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Read current state after downloading so edits made during sync survive publication.
		var err error
		result, err = s.publishMMA(library, remotes, downloads)
		return err
	})
	return result, err
}

func (s *Backend) publishMMA(library Library, remotes []mmaRemoteMap, downloads map[int64]mmaDownload) (mmaSyncResult, error) {
	manifest, err := library.Manifest()
	if err != nil {
		return mmaSyncResult{}, err
	}
	local := make([]Entry, 0, len(manifest.Maps))
	synced := map[int64]Entry{}
	for _, entry := range manifest.Maps {
		if mapfile.SourceType(entry.Source) != "map-making-app" {
			local = append(local, entry)
			continue
		}
		if id, ok := integerValue(entry.Source["mapId"]); ok {
			synced[id] = entry
		}
	}
	reserved := map[string]bool{}
	for _, entry := range local {
		reserved[strings.ToLower(entry.File)] = true
	}
	plans := make([]mmaPlan, 0, len(remotes))
	for _, remote := range remotes {
		var existing *Entry
		if value, ok := synced[remote.ID]; ok {
			copy := value
			existing = &copy
		}
		target := canonicalMMATarget(remote, existing, reserved)
		source := map[string]any{}
		if existing != nil {
			source = maps.Clone(existing.Source)
		}
		source["type"] = "map-making-app"
		source["managed"] = true
		source["mapId"] = remote.ID
		source["remoteName"] = defaultString(remote.Name, "Untitled map")
		if remote.Folder == "" {
			source["remoteFolder"] = nil
		} else {
			source["remoteFolder"] = remote.Folder
		}
		source["nameOverride"] = boolValue(source["nameOverride"])
		source["folderOverride"] = boolValue(source["folderOverride"])
		plans = append(plans, mmaPlan{remote: remote, existing: existing, target: target, source: source})
	}

	s.progress("publishing", len(plans), len(plans))
	finalSynced := make([]Entry, 0, len(plans))
	keepOld := map[string]bool{}
	failures := map[int64]string{}
	updated, unchanged := 0, 0
	for _, plan := range plans {
		download := downloads[plan.remote.ID]
		if download.err != nil {
			failures[plan.remote.ID] = download.err.Error()
			if plan.existing != nil {
				finalSynced = append(finalSynced, *plan.existing)
				keepOld[strings.ToLower(plan.existing.File)] = true
			}
			continue
		}
		targetPath, resolveErr := library.Resolve(plan.target)
		if resolveErr != nil {
			failures[plan.remote.ID] = resolveErr.Error()
			if plan.existing != nil {
				finalSynced = append(finalSynced, *plan.existing)
				keepOld[strings.ToLower(plan.existing.File)] = true
			}
			continue
		}
		same := plan.existing != nil && plan.existing.Checksum == download.checksum && strings.EqualFold(plan.existing.File, plan.target)
		if same {
			if _, statErr := os.Stat(targetPath); statErr != nil {
				same = false
			}
		}
		if !same {
			resolveErr = os.MkdirAll(filepath.Dir(targetPath), 0o755)
			if resolveErr == nil {
				resolveErr = os.Rename(download.stagePath, targetPath)
			}
		} else {
			resolveErr = os.Remove(download.stagePath)
			if resolveErr == nil || errors.Is(resolveErr, os.ErrNotExist) {
				resolveErr = nil
			}
		}
		_, statErr := os.Stat(targetPath)
		if resolveErr != nil || statErr != nil {
			if resolveErr == nil {
				resolveErr = statErr
			}
			failures[plan.remote.ID] = resolveErr.Error()
			if plan.existing != nil {
				finalSynced = append(finalSynced, *plan.existing)
				keepOld[strings.ToLower(plan.existing.File)] = true
			}
			continue
		}
		if same {
			unchanged++
		} else {
			updated++
		}
		name := defaultString(plan.remote.Name, "Untitled map")
		if plan.existing != nil && boolValue(plan.source["nameOverride"]) {
			name = plan.existing.Name
		}
		finalSynced = append(finalSynced, Entry{
			ID: "mma:" + strconv.FormatInt(plan.remote.ID, 10), Name: name, File: plan.target,
			Count: download.count, Checksum: download.checksum, Source: plan.source,
		})
	}

	finalManifest := Manifest{
		Folders: mapfile.FoldersOutsideRoot(manifest.Folders, mmaRoot),
		Maps:    append(local, finalSynced...),
	}
	if err := library.Save(finalManifest); err != nil {
		return mmaSyncResult{}, err
	}
	finalPaths := map[string]bool{}
	for _, entry := range finalSynced {
		finalPaths[strings.ToLower(entry.File)] = true
	}
	removed := 0
	for _, old := range synced {
		key := strings.ToLower(old.File)
		if finalPaths[key] || keepOld[key] {
			continue
		}
		if filename, err := library.Resolve(old.File); err == nil {
			if err := os.Remove(filename); err == nil || errors.Is(err, os.ErrNotExist) {
				removed++
			}
		}
	}
	mapfile.RemoveEmptyDirectories(library.Directory(), mmaRoot)
	failureList := make([]map[string]any, 0, len(failures))
	ids := make([]int64, 0, len(failures))
	for id := range failures {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		failureList = append(failureList, map[string]any{"mapId": id, "error": failures[id]})
	}
	return mmaSyncResult{
		Total: len(remotes), Updated: updated, Unchanged: unchanged, Failed: len(failures),
		Removed: removed, Failures: failureList,
	}, nil
}

func canonicalMMATarget(remote mmaRemoteMap, existing *Entry, reserved map[string]bool) string {
	source := map[string]any{}
	if existing != nil {
		source = existing.Source
	}
	folder := mmaRoot
	if existing != nil && boolValue(source["folderOverride"]) {
		folder = mapfile.Folder(existing.File)
	} else if remote.Folder != "" {
		folder = path.Join(folder, mapfile.SafeComponent(remote.Folder, "Unsorted"))
	}
	filename := mapfile.SafeComponent(remote.Name, "Untitled map") + ".json"
	if existing != nil && boolValue(source["nameOverride"]) {
		filename = path.Base(existing.File)
	}
	rel := path.Join(folder, filename)
	if reserved[strings.ToLower(rel)] && (existing == nil || !strings.EqualFold(rel, existing.File)) {
		extension := path.Ext(filename)
		stem := strings.TrimSuffix(filename, extension)
		rel = path.Join(folder, stem+"-"+strconv.FormatInt(remote.ID, 10)+extension)
	}
	reserved[strings.ToLower(rel)] = true
	return rel
}

func utcNow() string { return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339) }
