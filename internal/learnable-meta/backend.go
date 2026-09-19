package learnablemeta

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
	"github.com/ohne-b/OhneGuessr/internal/mapfile"
	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

const (
	learnableAPIBase          = "https://learnablemeta.com"
	learnableJobName          = "Learnable Meta"
	maxLearnableLocations     = 1_000_000
	maxLearnableText          = 200_000
	maxLearnableImages        = 100
	maxLearnableLocationBytes = 32 << 20
	maxLearnableClueBytes     = 2 << 20
	learnableRoot             = "Learnable Meta"
)

var (
	learnableMapIDPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
	errMapDataMissing     = errors.New("map data is missing")
)

type Entry = pluginhost.Entry
type Manifest = pluginhost.Manifest
type Library = pluginhost.Library
type Host = pluginhost.Host

type learnableConfigMap struct {
	MapID string `json:"mapId"`
	Name  string `json:"name"`
}

type learnableConfig struct {
	Version    int                  `json:"version"`
	Enabled    bool                 `json:"enabled"`
	APIKey     string               `json:"apiKey,omitempty"`
	Maps       []learnableConfigMap `json:"maps"`
	LastSyncAt string               `json:"lastSyncAt,omitempty"`
}

type syncRuntime struct {
	Running    bool           `json:"running"`
	Phase      string         `json:"phase"`
	Completed  int            `json:"completed"`
	Total      int            `json:"total"`
	Error      any            `json:"error"`
	LastResult map[string]any `json:"lastResult"`
}

type Backend struct {
	host       Host
	configPath string
	client     *http.Client
	baseURL    string
	mu         sync.Mutex
	runtime    syncRuntime
}

func New(host Host, configPath string) *Backend {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	service := &Backend{
		host: host, configPath: configPath,
		client: client, baseURL: learnableAPIBase, runtime: syncRuntime{Phase: "idle"},
	}
	service.mu.Lock()
	config := service.loadConfigLocked()
	service.mu.Unlock()
	if config.Enabled {
		_ = service.ensureRoot()
	}
	return service
}

func NewPlugin(host Host, dataDir string) pluginhost.MapPlugin {
	return New(host, filepath.Join(dataDir, "learnable-meta.json"))
}

func (s *Backend) MapPolicy() pluginhost.MapPolicy {
	return pluginhost.MapPolicy{SourceType: "learnable-meta", Root: learnableRoot}
}

func (s *Backend) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/learnable-meta/status", httpjson.Handler(func(_ *http.Request) (any, int, error) {
		return s.publicStatus(), http.StatusOK, nil
	}))
	mux.HandleFunc("GET /api/learnable-meta/clue", httpjson.Handler(func(r *http.Request) (any, int, error) {
		clue, err := s.getClue(r.URL.Query().Get("mapId"), r.URL.Query().Get("panoId"))
		return clue, http.StatusOK, learnableHTTPError(err)
	}))
	mux.HandleFunc("PUT /api/learnable-meta/settings", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Enabled bool `json:"enabled"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		status, err := s.SetEnabled(body.Enabled)
		return status, http.StatusOK, err
	}))
	mux.HandleFunc("PUT /api/learnable-meta/key", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			APIKey string `json:"apiKey"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		status, err := s.saveKey(body.APIKey)
		return status, http.StatusOK, err
	}))
	mux.HandleFunc("DELETE /api/learnable-meta/key", httpjson.Handler(func(_ *http.Request) (any, int, error) {
		status, err := s.forgetKey()
		return status, http.StatusOK, err
	}))
	mux.HandleFunc("POST /api/learnable-meta/maps", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[learnableConfigMap](r)
		if err != nil {
			return nil, 0, err
		}
		status, err := s.addMap(body.MapID, body.Name)
		return status, http.StatusCreated, learnableHTTPError(err)
	}))
	mux.HandleFunc("PATCH /api/learnable-meta/maps", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[learnableConfigMap](r)
		if err != nil {
			return nil, 0, err
		}
		status, err := s.renameMap(body.MapID, body.Name)
		return status, http.StatusOK, learnableHTTPError(err)
	}))
	mux.HandleFunc("DELETE /api/learnable-meta/maps", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			MapID string `json:"mapId"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		status, err := s.removeMap(body.MapID)
		return status, http.StatusOK, learnableHTTPError(err)
	}))
	mux.HandleFunc("POST /api/learnable-meta/sync", httpjson.Handler(func(_ *http.Request) (any, int, error) {
		status, err := s.start()
		return status, http.StatusAccepted, err
	}))
}

func defaultLearnableConfig() learnableConfig {
	return learnableConfig{Version: 1, Maps: []learnableConfigMap{}}
}

func (s *Backend) loadConfigLocked() learnableConfig {
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		return defaultLearnableConfig()
	}
	var decoded learnableConfig
	if json.Unmarshal(raw, &decoded) != nil {
		return defaultLearnableConfig()
	}
	clean := defaultLearnableConfig()
	clean.Enabled = decoded.Enabled
	if key := strings.TrimSpace(decoded.APIKey); len(key) <= 4096 {
		clean.APIKey = key
	}
	clean.LastSyncAt = decoded.LastSyncAt
	ids, names := map[string]bool{}, map[string]bool{}
	for _, item := range decoded.Maps {
		id, idErr := cleanLearnableMapID(item.MapID)
		name, nameErr := cleanLearnableMapName(item.Name)
		if idErr != nil || nameErr != nil || ids[strings.ToLower(id)] || names[strings.ToLower(name)] {
			continue
		}
		ids[strings.ToLower(id)] = true
		names[strings.ToLower(name)] = true
		clean.Maps = append(clean.Maps, learnableConfigMap{MapID: id, Name: name})
	}
	return clean
}

func (s *Backend) saveConfigLocked(config learnableConfig) error {
	config.Version = 1
	if config.Maps == nil {
		config.Maps = []learnableConfigMap{}
	}
	return mapfile.WriteJSON(s.configPath, config, 0o600)
}

func (s *Backend) publicStatus() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publicStatusLocked()
}

func (s *Backend) publicStatusLocked() map[string]any {
	config := s.loadConfigLocked()
	maps := make([]learnableConfigMap, len(config.Maps))
	copy(maps, config.Maps)
	return map[string]any{
		"available":  true,
		"enabled":    config.Enabled,
		"hasKey":     config.APIKey != "",
		"maps":       maps,
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
	if enabled {
		if err := s.ensureRoot(); err != nil {
			return nil, httpjson.Error(http.StatusInternalServerError, "could not create Learnable Meta map folder")
		}
	}
	s.mu.Lock()
	config := s.loadConfigLocked()
	config.Enabled = enabled
	err := s.saveConfigLocked(config)
	status := s.publicStatusLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not save Learnable Meta settings")
	}
	if !enabled {
		s.cancel()
		status = s.publicStatus()
	}
	return status, nil
}

func (s *Backend) ensureRoot() error {
	return s.host.WithLibrary(func(library Library) error {
		return os.MkdirAll(filepath.Join(library.Directory(), learnableRoot), 0o755)
	})
}

func (s *Backend) saveKey(rawKey string) (map[string]any, error) {
	key, err := cleanLearnableAPIKey(rawKey)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime.Running {
		return nil, httpjson.Error(http.StatusConflict, "Stop synchronization before replacing the API key")
	}
	config := s.loadConfigLocked()
	config.Enabled = true
	config.APIKey = key
	if err := s.saveConfigLocked(config); err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not save Learnable Meta settings")
	}
	if err := s.ensureRoot(); err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not create Learnable Meta map folder")
	}
	return s.publicStatusLocked(), nil
}

func (s *Backend) forgetKey() (map[string]any, error) {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	config := s.loadConfigLocked()
	config.APIKey = ""
	config.Enabled = false
	if err := s.saveConfigLocked(config); err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not forget the Learnable Meta key")
	}
	return s.publicStatusLocked(), nil
}

func (s *Backend) addMap(rawID, rawName string) (map[string]any, error) {
	mapID, err := cleanLearnableMapID(rawID)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	name, err := cleanLearnableMapName(rawName)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	ctx, release, err := s.host.AcquireSync(learnableJobName)
	if err != nil {
		return nil, err
	}
	defer release()
	s.mu.Lock()
	config := s.loadConfigLocked()
	if err := requireLearnableReady(config); err != nil {
		s.mu.Unlock()
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	if err := checkLearnableUnique(config, mapID, name); err != nil {
		s.mu.Unlock()
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	key := config.APIKey
	s.mu.Unlock()

	rawLocations, err := s.fetchLocations(ctx, mapID, key)
	if err != nil {
		return nil, err
	}
	locations, err := normalizeLearnableLocations(rawLocations)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	if err := ctx.Err(); err != nil {
		return nil, httpjson.Error(http.StatusConflict, "synchronization cancelled")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	config = s.loadConfigLocked()
	if err := requireLearnableReady(config); err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	if err := checkLearnableUnique(config, mapID, name); err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	previous := config
	previous.Maps = append([]learnableConfigMap(nil), config.Maps...)
	config.Maps = append(config.Maps, learnableConfigMap{MapID: mapID, Name: name})
	config.LastSyncAt = utcNow()
	if err := s.saveConfigLocked(config); err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not save Learnable Meta settings")
	}
	changed, err := s.publishLearnableMap(mapID, name, locations)
	if err != nil {
		_ = s.saveConfigLocked(previous)
		_ = s.deletePublishedLearnableMap(mapID)
		return nil, err
	}
	s.runtime.Phase = "complete"
	s.runtime.Error = nil
	s.runtime.LastResult = learnableResult(1, boolInt(changed), boolInt(!changed), nil)
	return s.publicStatusLocked(), nil
}

func (s *Backend) renameMap(rawID, rawName string) (map[string]any, error) {
	mapID, err := cleanLearnableMapID(rawID)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	name, err := cleanLearnableMapName(rawName)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	_, release, err := s.host.AcquireSync(learnableJobName)
	if err != nil {
		return nil, err
	}
	defer release()
	s.mu.Lock()
	defer s.mu.Unlock()
	config := s.loadConfigLocked()
	index := findLearnableConfigMap(config, mapID)
	if index < 0 {
		return nil, httpjson.Error(http.StatusNotFound, "Learnable Meta map not found")
	}
	for i, item := range config.Maps {
		if i != index && strings.EqualFold(item.Name, name) {
			return nil, httpjson.Error(http.StatusBadRequest, "A Learnable Meta map already uses that name")
		}
	}
	oldName := config.Maps[index].Name
	config.Maps[index].Name = name
	if err := s.saveConfigLocked(config); err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not save Learnable Meta settings")
	}
	if err := s.renamePublishedLearnableMap(mapID, name); err != nil {
		config.Maps[index].Name = oldName
		_ = s.saveConfigLocked(config)
		return nil, err
	}
	return s.publicStatusLocked(), nil
}

func (s *Backend) removeMap(rawID string) (map[string]any, error) {
	mapID, err := cleanLearnableMapID(rawID)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	_, release, err := s.host.AcquireSync(learnableJobName)
	if err != nil {
		return nil, err
	}
	defer release()
	s.mu.Lock()
	defer s.mu.Unlock()
	config := s.loadConfigLocked()
	index := findLearnableConfigMap(config, mapID)
	if index < 0 {
		return nil, httpjson.Error(http.StatusNotFound, "Learnable Meta map not found")
	}
	previous := append([]learnableConfigMap(nil), config.Maps...)
	config.Maps = append(config.Maps[:index], config.Maps[index+1:]...)
	if err := s.saveConfigLocked(config); err != nil {
		return nil, httpjson.Error(http.StatusInternalServerError, "could not save Learnable Meta settings")
	}
	if err := s.deletePublishedLearnableMap(mapID); err != nil {
		config.Maps = previous
		_ = s.saveConfigLocked(config)
		return nil, err
	}
	return s.publicStatusLocked(), nil
}

func cleanLearnableAPIKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", errors.New("API key required")
	}
	if len(key) > 4096 {
		return "", errors.New("API key is too long")
	}
	return key, nil
}

func cleanLearnableMapID(value string) (string, error) {
	mapID := strings.TrimSpace(value)
	if mapID == "" {
		return "", errors.New("Learnable Meta map ID required")
	}
	if len(mapID) > 200 || !learnableMapIDPattern.MatchString(mapID) {
		return "", errors.New("Map ID must use only letters, numbers, dots, dashes, underscores, or tildes")
	}
	return mapID, nil
}

func cleanLearnableMapName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", errors.New("Map name required")
	}
	if len([]rune(name)) > 120 {
		return "", errors.New("Map name is too long")
	}
	return name, nil
}

func requireLearnableReady(config learnableConfig) error {
	if !config.Enabled {
		return errors.New("Learnable Meta sync is off")
	}
	if config.APIKey == "" {
		return errors.New("Save an API key first")
	}
	return nil
}

func checkLearnableUnique(config learnableConfig, mapID, name string) error {
	for _, item := range config.Maps {
		if strings.EqualFold(item.MapID, mapID) {
			return errors.New("That Learnable Meta map is already configured")
		}
		if strings.EqualFold(item.Name, name) {
			return errors.New("A Learnable Meta map already uses that name")
		}
	}
	return nil
}

func findLearnableConfigMap(config learnableConfig, mapID string) int {
	for index, item := range config.Maps {
		if item.MapID == mapID {
			return index
		}
	}
	return -1
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
