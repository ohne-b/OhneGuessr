package learnablemeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
)

func (s *Backend) getClue(rawMapID, rawPanoID string) (map[string]any, error) {
	mapID, err := cleanLearnableMapID(rawMapID)
	if err != nil {
		return nil, httpjson.Error(http.StatusBadRequest, err.Error())
	}
	panoID := strings.TrimSpace(rawPanoID)
	if panoID == "" || len(panoID) > 512 {
		return nil, httpjson.Error(http.StatusBadRequest, "Panorama ID required")
	}
	s.mu.Lock()
	configured := findLearnableConfigMap(s.loadConfigLocked(), mapID) >= 0
	s.mu.Unlock()
	if !configured {
		return nil, httpjson.Error(http.StatusNotFound, "Learnable Meta map not found")
	}
	query := url.Values{"mapId": {mapID}, "panoId": {panoID}}
	var raw map[string]any
	if err := s.apiGetJSON(context.Background(), "/api/userscript/location?"+query.Encode(), "", maxLearnableClueBytes, 0, &raw); err != nil {
		return nil, err
	}
	return normalizeLearnableClue(raw)
}

func (s *Backend) fetchLocations(ctx context.Context, mapID, key string) ([]map[string]any, error) {
	var payload struct {
		CustomCoordinates []map[string]any `json:"customCoordinates"`
	}
	endpoint := "/api/userscript/map/" + url.PathEscape(mapID) + "/locations"
	if err := s.apiGetJSON(ctx, endpoint, key, maxLearnableLocationBytes, 1, &payload); err != nil {
		return nil, err
	}
	if payload.CustomCoordinates == nil {
		return nil, &learnableAPIError{message: "Learnable Meta returned invalid location data", status: http.StatusBadGateway}
	}
	return payload.CustomCoordinates, nil
}

type learnableAPIError struct {
	message string
	status  int
}

func (e *learnableAPIError) Error() string { return e.message }

func learnableHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var apiError *learnableAPIError
	if errors.As(err, &apiError) {
		status := apiError.status
		if status != 401 && status != 403 && status != 404 && status != 429 {
			status = http.StatusBadGateway
		}
		return httpjson.Error(status, apiError.message)
	}
	return err
}

func (s *Backend) apiGetJSON(ctx context.Context, endpoint, key string, maximum int64, retries int, target any) error {
	var lastError error
	for attempt := 0; attempt <= retries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+endpoint, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "OhneGuessr/1 Learnable-Meta-Sync")
		if key != "" {
			request.Header.Set("Authorization", "Bearer "+key)
		}
		response, err := s.client.Do(request)
		if err == nil {
			if response.ContentLength > maximum {
				response.Body.Close()
				return &learnableAPIError{message: "Learnable Meta response is too large", status: http.StatusBadGateway}
			}
			body, readErr := httpjson.ReadLimited(response.Body, maximum)
			response.Body.Close()
			if readErr != nil {
				return &learnableAPIError{message: "Learnable Meta response is too large", status: http.StatusBadGateway}
			}
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				if json.Unmarshal(body, target) != nil {
					return &learnableAPIError{message: "Learnable Meta returned invalid JSON", status: http.StatusBadGateway}
				}
				return nil
			}
			lastError = &learnableAPIError{message: learnableStatusMessage(response.StatusCode), status: response.StatusCode}
			if response.StatusCode != http.StatusTooManyRequests && (response.StatusCode < 500 || response.StatusCode >= 600) {
				return lastError
			}
		} else {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastError = &learnableAPIError{message: "Could not reach Learnable Meta: " + err.Error(), status: http.StatusBadGateway}
		}
		if attempt < retries {
			if err := httpjson.Wait(ctx, time.Duration(attempt+1)*400*time.Millisecond); err != nil {
				return err
			}
		}
	}
	if lastError == nil {
		lastError = &learnableAPIError{message: "Learnable Meta request failed", status: http.StatusBadGateway}
	}
	return lastError
}

func learnableStatusMessage(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "Learnable Meta rejected the API key"
	case http.StatusForbidden:
		return "The API key cannot access this Learnable Meta map"
	case http.StatusNotFound:
		return "Learnable Meta map not found"
	case http.StatusTooManyRequests:
		return "Learnable Meta is rate limiting requests; try again shortly"
	default:
		return fmt.Sprintf("Learnable Meta request failed (HTTP %d)", status)
	}
}

func normalizeLearnableLocations(raw []map[string]any) ([]map[string]any, error) {
	if len(raw) > maxLearnableLocations {
		return nil, errors.New("Learnable Meta map has too many locations")
	}
	result := make([]map[string]any, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		latitude, latOK := finiteNumber(item["lat"])
		longitude, lngOK := finiteNumber(item["lng"])
		if !latOK || latitude < -90 || latitude > 90 || !lngOK || longitude < -180 || longitude > 180 {
			continue
		}
		panoValue := item["panoId"]
		if panoValue == nil {
			panoValue = item["panoid"]
		}
		panoID, ok := panoValue.(string)
		panoID = strings.TrimSpace(panoID)
		if !ok || panoID == "" || len(panoID) > 512 || seen[panoID] {
			continue
		}
		seen[panoID] = true
		location := map[string]any{"lat": latitude, "lng": longitude, "panoId": panoID}
		for _, key := range []string{"heading", "pitch", "zoom"} {
			if number, ok := finiteNumber(item[key]); ok {
				location[key] = number
			}
		}
		result = append(result, location)
	}
	if len(result) == 0 {
		return nil, errors.New("Learnable Meta map has no playable locations")
	}
	return result, nil
}

func normalizeLearnableClue(raw map[string]any) (map[string]any, error) {
	if raw == nil {
		return nil, &learnableAPIError{message: "Learnable Meta returned invalid clue data", status: http.StatusBadGateway}
	}
	result := map[string]any{
		"country":  cleanLearnableText(raw["country"]),
		"metaName": cleanLearnableText(raw["metaName"]),
		"note":     cleanLearnableText(raw["note"]),
		"footer":   cleanLearnableText(raw["footer"]),
		"images":   []string{},
	}
	images, _ := raw["images"].([]any)
	clean := make([]string, 0, min(len(images), maxLearnableImages))
	for _, value := range images {
		image, ok := value.(string)
		if !ok {
			continue
		}
		clean = append(clean, truncateRunes(image, 4096))
		if len(clean) == maxLearnableImages {
			break
		}
	}
	result["images"] = clean
	return result, nil
}

func cleanLearnableText(value any) string {
	text, _ := value.(string)
	return truncateRunes(text, maxLearnableText)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		return string(runes[:maximum])
	}
	return value
}

func finiteNumber(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}
