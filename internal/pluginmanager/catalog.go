package pluginmanager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
)

const (
	repositoryURL    = "https://raw.githubusercontent.com/ohne-b/OhneGuessr/main/plugins"
	maxPluginCatalog = 256 << 10
)

func (s *PluginService) Catalog() ([]PluginManifest, error) {
	return s.catalog()
}

func (s *PluginService) catalog() ([]PluginManifest, error) {
	contents, err := s.fetch(s.baseURL+"/registry.json", maxPluginCatalog)
	if err != nil {
		return nil, fmt.Errorf("download plugin catalog: %w", err)
	}
	var catalog []PluginManifest
	if err := json.Unmarshal(contents, &catalog); err != nil {
		return nil, fmt.Errorf("decode plugin catalog: %w", err)
	}
	compatible := catalog[:0]
	seen := make(map[string]bool, len(catalog))
	for _, manifest := range catalog {
		if manifest.APIVersion != pluginAPIVersion {
			continue
		}
		if err := validatePluginManifest(manifest, manifest.ID, true); err != nil {
			return nil, err
		}
		if seen[manifest.ID] {
			return nil, fmt.Errorf("duplicate plugin id %q", manifest.ID)
		}
		seen[manifest.ID] = true
		compatible = append(compatible, manifest)
	}
	sort.Slice(compatible, func(i, j int) bool { return compatible[i].Name < compatible[j].Name })
	return compatible, nil
}

func (s *PluginService) fetch(url string, maximum int64) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The catalog, manifest, and source can have different CDN cache ages after a release.
	request.Header.Set("Cache-Control", "no-cache")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return httpjson.ReadLimited(response.Body, maximum)
}
