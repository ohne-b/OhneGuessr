package mapmakingapp

import (
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
	"github.com/ohne-b/OhneGuessr/internal/mapfile"
	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

const (
	mmaAPIBase     = "https://map-making.app"
	mmaMaxWorkers  = 10
	mmaMaxResponse = 64 << 20
	mmaJobName     = "Map Making App"
	mmaRoot        = "map-making-app"
)

type mmaConfig struct {
	Version    int    `json:"version"`
	Enabled    bool   `json:"enabled"`
	APIKey     string `json:"apiKey,omitempty"`
	UserID     any    `json:"userId,omitempty"`
	Username   string `json:"username,omitempty"`
	LastSyncAt string `json:"lastSyncAt,omitempty"`
}

type syncRuntime struct {
	Running    bool           `json:"running"`
	Phase      string         `json:"phase"`
	Completed  int            `json:"completed"`
	Total      int            `json:"total"`
	Error      any            `json:"error"`
	LastResult map[string]any `json:"lastResult"`
}

type Entry = pluginhost.Entry
type Manifest = pluginhost.Manifest
type Library = pluginhost.Library
type Host = pluginhost.Host

type Backend struct {
	host       Host
	configPath string
	client     *http.Client
	baseURL    string
	mu         sync.Mutex
	runtime    syncRuntime
}

func New(host Host, configPath string) *Backend {
	return &Backend{
		host: host, configPath: configPath,
		client:  &http.Client{Timeout: 90 * time.Second},
		baseURL: mmaAPIBase,
		runtime: syncRuntime{Phase: "idle"},
	}
}

func NewPlugin(host Host, dataDir string) pluginhost.MapPlugin {
	return New(host, filepath.Join(dataDir, "map-making-app.json"))
}

func (s *Backend) MapPolicy() pluginhost.MapPolicy {
	return pluginhost.MapPolicy{
		SourceType:      "map-making-app",
		Root:            mmaRoot,
		EditableFolders: true,
		RenameMaps:      true,
		MoveMaps:        true,
		DeleteMaps:      true,
		Filename: func(name string) string {
			return mapfile.SafeComponent(name, "Untitled map") + ".json"
		},
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
	}
}

func (s *Backend) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mma-sync/status", httpjson.Handler(func(_ *http.Request) (any, int, error) {
		return s.publicStatus(), http.StatusOK, nil
	}))
	mux.HandleFunc("PUT /api/mma-sync/config", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Enabled bool `json:"enabled"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		status, err := s.SetEnabled(body.Enabled)
		return status, http.StatusOK, err
	}))
	mux.HandleFunc("PUT /api/mma-sync/key", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			APIKey string `json:"apiKey"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		status, err := s.saveKey(body.APIKey)
		return status, http.StatusOK, err
	}))
	mux.HandleFunc("DELETE /api/mma-sync/key", httpjson.Handler(func(_ *http.Request) (any, int, error) {
		status, err := s.forgetKey()
		return status, http.StatusOK, err
	}))
	mux.HandleFunc("POST /api/mma-sync/run", httpjson.Handler(func(_ *http.Request) (any, int, error) {
		status, err := s.start()
		return status, http.StatusAccepted, err
	}))
}

func defaultMMAConfig() mmaConfig { return mmaConfig{Version: 1} }

func (s *Backend) loadConfigLocked() mmaConfig {
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		return defaultMMAConfig()
	}
	config := defaultMMAConfig()
	if json.Unmarshal(raw, &config) != nil {
		return defaultMMAConfig()
	}
	config.Version = 1
	config.APIKey = strings.TrimSpace(config.APIKey)
	return config
}

func (s *Backend) saveConfigLocked(config mmaConfig) error {
	config.Version = 1
	return mapfile.WriteJSON(s.configPath, config, 0o600)
}

func (s *Backend) publicStatus() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publicStatusLocked()
}

func (s *Backend) publicStatusLocked() map[string]any {
	config := s.loadConfigLocked()
	var user any
	if config.Username != "" {
		user = map[string]any{"id": config.UserID, "username": config.Username}
	}
	return map[string]any{
		"available":  true,
		"enabled":    config.Enabled,
		"hasKey":     config.APIKey != "",
		"user":       user,
		"lastSyncAt": nilIfEmpty(config.LastSyncAt),
		"running":    s.runtime.Running,
		"phase":      s.runtime.Phase,
		"completed":  s.runtime.Completed,
		"total":      s.runtime.Total,
		"error":      s.runtime.Error,
		"lastResult": s.runtime.LastResult,
	}
}

func (s *Backend) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadConfigLocked().Enabled
}

func (s *Backend) SetEnabled(enabled bool) (map[string]any, error) {
	s.mu.Lock()
	config := s.loadConfigLocked()
	config.Enabled = enabled
	err := s.saveConfigLocked(config)
	status := s.publicStatusLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not save Map Making App settings")
	}
	if !enabled {
		s.cancel()
		status = s.publicStatus()
	}
	return status, nil
}

func (s *Backend) saveKey(rawKey string) (map[string]any, error) {
	key := strings.TrimSpace(rawKey)
	if key == "" {
		return nil, httpjson.Error(http.StatusBadRequest, "API key required")
	}
	if len(key) > 4096 {
		return nil, httpjson.Error(http.StatusBadRequest, "API key is too long")
	}
	ctx, release, err := s.host.AcquireSync(mmaJobName)
	if err != nil {
		return nil, err
	}
	var user struct {
		ID       any    `json:"id"`
		Username string `json:"username"`
	}
	if err := s.apiGetJSON(ctx, "/api/user", key, &user); err != nil {
		release()
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	if user.ID == nil || strings.TrimSpace(user.Username) == "" {
		release()
		return nil, httpjson.Error(http.StatusBadRequest, "Map Making App returned an invalid user")
	}

	s.mu.Lock()
	config := s.loadConfigLocked()
	config.Enabled = true
	config.APIKey = key
	config.UserID = user.ID
	config.Username = user.Username
	if err := s.saveConfigLocked(config); err != nil {
		s.mu.Unlock()
		release()
		return nil, httpjson.Error(http.StatusInternalServerError, "could not save Map Making App settings")
	}
	s.beginLocked()
	status := s.publicStatusLocked()
	s.mu.Unlock()
	go s.run(ctx, release, key)
	return status, nil
}

func (s *Backend) forgetKey() (map[string]any, error) {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.loadConfigLocked()
	clean := defaultMMAConfig()
	clean.LastSyncAt = previous.LastSyncAt
	if err := s.saveConfigLocked(clean); err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not forget the Map Making App key")
	}
	return s.publicStatusLocked(), nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
