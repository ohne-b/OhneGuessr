package pluginmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
)

const (
	maxPluginManifest = 64 << 10
	maxPluginSource   = 2 << 20
)

func (s *PluginService) Installed() ([]PluginInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installedLocked()
}

func (s *PluginService) Install(id string) (PluginInfo, error) {
	if err := validatePluginID(id); err != nil {
		return PluginInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	catalog, err := s.catalog()
	if err != nil {
		return PluginInfo{}, err
	}
	var expected *PluginManifest
	for index := range catalog {
		if catalog[index].ID == id {
			expected = &catalog[index]
			break
		}
	}
	if expected == nil {
		return PluginInfo{}, errors.New("plugin is not in the curated catalog")
	}

	manifestBytes, err := s.fetch(s.baseURL+"/"+id+"/manifest.json", maxPluginManifest)
	if err != nil {
		return PluginInfo{}, fmt.Errorf("download plugin manifest: %w", err)
	}
	var manifest PluginManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return PluginInfo{}, fmt.Errorf("decode plugin manifest: %w", err)
	}
	if err := validatePluginManifest(manifest, id, false); err != nil {
		return PluginInfo{}, err
	}
	if !samePluginManifest(manifest, *expected) {
		return PluginInfo{}, errors.New("plugin manifest does not match the curated catalog")
	}

	source, err := s.fetch(s.baseURL+"/"+id+"/"+manifest.Main, maxPluginSource)
	if err != nil {
		return PluginInfo{}, fmt.Errorf("download plugin source: %w", err)
	}
	if !utf8.Valid(source) {
		return PluginInfo{}, errors.New("plugin source is not valid UTF-8")
	}
	if pluginChecksum(source) != expected.SHA256 {
		return PluginInfo{}, errors.New("plugin checksum does not match the curated catalog")
	}
	manifest.SHA256 = expected.SHA256

	state, err := s.loadStateLocked()
	if err != nil {
		return PluginInfo{}, err
	}
	target := filepath.Join(s.pluginsDir(), id)
	_, statErr := os.Lstat(target)
	wasInstalled := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return PluginInfo{}, statErr
	}
	if err := s.stagePluginLocked(target, manifest, source); err != nil {
		return PluginInfo{}, err
	}
	if !wasInstalled {
		setPluginEnabled(&state, id, true)
	}
	if err := s.saveStateLocked(state); err != nil {
		return PluginInfo{}, err
	}
	return pluginInfo(manifest, pluginEnabled(state, id), configuredPluginSettings(manifest, state.Settings[id])), nil
}

func (s *PluginService) Uninstall(id string) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	target := filepath.Join(s.pluginsDir(), id)
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to uninstall a symbolic link")
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove plugin: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	state, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	setPluginEnabled(&state, id, false)
	delete(state.Settings, id)
	return s.saveStateLocked(state)
}

func (s *PluginService) EnabledModules() ([]PluginModule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadStateLocked()
	if err != nil {
		return nil, err
	}
	installed, err := s.installedManifestsLocked()
	if err != nil {
		return nil, err
	}
	modules := make([]PluginModule, 0, len(installed))
	for _, manifest := range installed {
		if !pluginEnabled(state, manifest.ID) {
			continue
		}
		source, err := readFileLimited(
			filepath.Join(s.pluginsDir(), manifest.ID, manifest.Main),
			maxPluginSource,
		)
		if err != nil || !utf8.Valid(source) || pluginChecksum(source) != manifest.SHA256 {
			log.Printf("skip invalid plugin %q", manifest.ID)
			continue
		}
		modules = append(modules, PluginModule{Manifest: manifest, Source: string(source)})
	}
	return modules, nil
}

func (s *PluginService) installedLocked() ([]PluginInfo, error) {
	state, err := s.loadStateLocked()
	if err != nil {
		return nil, err
	}
	manifests, err := s.installedManifestsLocked()
	if err != nil {
		return nil, err
	}
	result := make([]PluginInfo, 0, len(manifests))
	for _, manifest := range manifests {
		result = append(result, pluginInfo(
			manifest,
			pluginEnabled(state, manifest.ID),
			configuredPluginSettings(manifest, state.Settings[manifest.ID]),
		))
	}
	return result, nil
}

func (s *PluginService) installedManifestsLocked() ([]PluginManifest, error) {
	entries, err := os.ReadDir(s.pluginsDir())
	if os.IsNotExist(err) {
		return []PluginManifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	manifests := make([]PluginManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		manifest, err := s.readInstalledManifest(filepath.Join(s.pluginsDir(), entry.Name()), entry.Name())
		if err == nil {
			manifests = append(manifests, manifest)
		}
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })
	return manifests, nil
}

func (s *PluginService) readInstalledManifest(directory, id string) (PluginManifest, error) {
	contents, err := readFileLimited(filepath.Join(directory, "manifest.json"), maxPluginManifest)
	if err != nil {
		return PluginManifest{}, err
	}
	var manifest PluginManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return PluginManifest{}, err
	}
	if err := validatePluginManifest(manifest, id, true); err != nil {
		return PluginManifest{}, err
	}
	return manifest, nil
}

func (s *PluginService) stagePluginLocked(target string, manifest PluginManifest, source []byte) (err error) {
	if err := os.MkdirAll(s.pluginsDir(), 0o700); err != nil {
		return err
	}
	if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace a symbolic link")
	}
	stage, err := os.MkdirTemp(s.pluginsDir(), "."+manifest.ID+"-install-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := mapfile.WriteJSON(filepath.Join(stage, "manifest.json"), manifest, 0o600); err != nil {
		return err
	}
	if err := mapfile.Write(filepath.Join(stage, manifest.Main), source, 0o600); err != nil {
		return err
	}

	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return os.Rename(stage, target)
	} else if err != nil {
		return err
	}
	backup, err := os.MkdirTemp(s.pluginsDir(), "."+manifest.ID+"-backup-*")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	return nil
}

func (s *PluginService) pluginsDir() string { return filepath.Join(s.dataDir, "plugins") }

func readFileLimited(filename string, maximum int64) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, errors.New("file is too large")
	}
	return contents, nil
}
