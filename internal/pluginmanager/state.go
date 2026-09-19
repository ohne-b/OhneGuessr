package pluginmanager

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
)

type pluginState struct {
	Version  int                          `json:"version"`
	Enabled  []string                     `json:"enabled"`
	Settings map[string]map[string]string `json:"settings,omitempty"`
}

func (s *PluginService) SetEnabled(id string, enabled bool) (PluginInfo, error) {
	if err := validatePluginID(id); err != nil {
		return PluginInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	manifest, err := s.readInstalledManifest(filepath.Join(s.pluginsDir(), id), id)
	if err != nil {
		if os.IsNotExist(err) {
			return PluginInfo{}, errors.New("plugin is not installed")
		}
		return PluginInfo{}, err
	}
	state, err := s.loadStateLocked()
	if err != nil {
		return PluginInfo{}, err
	}
	setPluginEnabled(&state, id, enabled)
	if err := s.saveStateLocked(state); err != nil {
		return PluginInfo{}, err
	}
	return pluginInfo(manifest, enabled, configuredPluginSettings(manifest, state.Settings[id])), nil
}

func (s *PluginService) SetSetting(id, key, value string) (PluginInfo, error) {
	if err := validatePluginID(id); err != nil {
		return PluginInfo{}, err
	}
	if err := validatePluginSettingKey(key); err != nil {
		return PluginInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	manifest, err := s.readInstalledManifest(filepath.Join(s.pluginsDir(), id), id)
	if err != nil {
		if os.IsNotExist(err) {
			return PluginInfo{}, errors.New("plugin is not installed")
		}
		return PluginInfo{}, err
	}
	if !pluginDeclaresSetting(manifest, key) {
		return PluginInfo{}, errors.New("plugin setting is not declared")
	}
	value = strings.TrimSpace(value)
	if len(value) > 4096 {
		return PluginInfo{}, errors.New("plugin setting is too long")
	}
	state, err := s.loadStateLocked()
	if err != nil {
		return PluginInfo{}, err
	}
	if value == "" {
		delete(state.Settings[id], key)
		if len(state.Settings[id]) == 0 {
			delete(state.Settings, id)
		}
	} else {
		if state.Settings == nil {
			state.Settings = make(map[string]map[string]string)
		}
		if state.Settings[id] == nil {
			state.Settings[id] = make(map[string]string)
		}
		state.Settings[id][key] = value
	}
	if err := s.saveStateLocked(state); err != nil {
		return PluginInfo{}, err
	}
	return pluginInfo(manifest, pluginEnabled(state, id), configuredPluginSettings(manifest, state.Settings[id])), nil
}

func (s *PluginService) Setting(id, key string) (string, error) {
	if err := validatePluginID(id); err != nil {
		return "", err
	}
	if err := validatePluginSettingKey(key); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	manifest, err := s.readInstalledManifest(filepath.Join(s.pluginsDir(), id), id)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("plugin is not installed")
		}
		return "", err
	}
	if !pluginDeclaresSetting(manifest, key) {
		return "", errors.New("plugin setting is not declared")
	}
	state, err := s.loadStateLocked()
	if err != nil {
		return "", err
	}
	return state.Settings[id][key], nil
}

func (s *PluginService) loadStateLocked() (pluginState, error) {
	contents, err := os.ReadFile(s.statePath())
	if os.IsNotExist(err) {
		return pluginState{Version: 1, Enabled: []string{}}, nil
	}
	if err != nil {
		return pluginState{}, err
	}
	var stored pluginState
	if err := json.Unmarshal(contents, &stored); err != nil || stored.Version != 1 {
		return pluginState{}, errors.New("plugin state is invalid")
	}
	enabled := make(map[string]bool, len(stored.Enabled))
	for _, id := range stored.Enabled {
		if validatePluginID(id) == nil {
			enabled[id] = true
		}
	}
	stored.Enabled = stored.Enabled[:0]
	for id := range enabled {
		stored.Enabled = append(stored.Enabled, id)
	}
	sort.Strings(stored.Enabled)
	settings := make(map[string]map[string]string)
	for id, values := range stored.Settings {
		if validatePluginID(id) != nil {
			continue
		}
		for key, value := range values {
			if validatePluginSettingKey(key) != nil || value == "" || len(value) > 4096 {
				continue
			}
			if settings[id] == nil {
				settings[id] = make(map[string]string)
			}
			settings[id][key] = value
		}
	}
	if len(settings) == 0 {
		settings = nil
	}
	stored.Settings = settings
	return stored, nil
}

func (s *PluginService) saveStateLocked(state pluginState) error {
	state.Version = 1
	sort.Strings(state.Enabled)
	return mapfile.WriteJSON(s.statePath(), state, 0o600)
}

func (s *PluginService) statePath() string { return filepath.Join(s.dataDir, "plugins.json") }

func pluginEnabled(state pluginState, id string) bool {
	return slices.Contains(state.Enabled, id)
}

func setPluginEnabled(state *pluginState, id string, enabled bool) {
	index := slices.Index(state.Enabled, id)
	if enabled && index < 0 {
		state.Enabled = append(state.Enabled, id)
	} else if !enabled && index >= 0 {
		state.Enabled = slices.Delete(state.Enabled, index, index+1)
	}
}

func configuredPluginSettings(manifest PluginManifest, values map[string]string) []string {
	configured := make([]string, 0, len(manifest.Settings))
	for _, setting := range manifest.Settings {
		if values[setting.Key] != "" {
			configured = append(configured, setting.Key)
		}
	}
	return configured
}
