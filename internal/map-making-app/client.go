package mapmakingapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
	"github.com/ohne-b/OhneGuessr/internal/mapfile"
)

type mmaRemoteMap struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Folder        string `json:"folder"`
	Type          string `json:"type"`
	Storage       string `json:"storage"`
	ArchivedAt    any    `json:"archivedAt"`
	LocationCount int    `json:"locationCount"`
}

type mmaDownload struct {
	mapID     int64
	stagePath string
	count     int
	checksum  string
	err       error
}

func (s *Backend) downloadMMA(ctx context.Context, key, staging string, remotes []mmaRemoteMap) map[int64]mmaDownload {
	jobs := make(chan mmaRemoteMap)
	results := make(chan mmaDownload, len(remotes))
	workers := min(mmaMaxWorkers, len(remotes))
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for remote := range jobs {
				results <- s.downloadOneMMA(ctx, key, staging, remote.ID)
			}
		}()
	}
	go func() {
		for _, remote := range remotes {
			select {
			case jobs <- remote:
			case <-ctx.Done():
				close(jobs)
				group.Wait()
				close(results)
				return
			}
		}
		close(jobs)
		group.Wait()
		close(results)
	}()
	downloads := make(map[int64]mmaDownload, len(remotes))
	completed := 0
	for result := range results {
		downloads[result.mapID] = result
		completed++
		s.progress("downloading", completed, len(remotes))
	}
	return downloads
}

func (s *Backend) downloadOneMMA(ctx context.Context, key, staging string, mapID int64) mmaDownload {
	result := mmaDownload{mapID: mapID}
	var locations []json.RawMessage
	err := s.apiGetJSON(ctx, "/api/maps/"+strconv.FormatInt(mapID, 10)+"/locations", key, &locations)
	if err == nil && len(locations) == 0 {
		err = fmt.Errorf("Map %d returned invalid locations", mapID)
	}
	if err != nil {
		result.err = err
		return result
	}
	encoded, err := json.Marshal(locations)
	if err != nil {
		result.err = err
		return result
	}
	result.stagePath = filepath.Join(staging, strconv.FormatInt(mapID, 10)+".json")
	if err := mapfile.Write(result.stagePath, encoded, 0o644); err != nil {
		result.err = err
		return result
	}
	result.count = len(locations)
	result.checksum = mapfile.Checksum(encoded)
	return result
}

func (s *Backend) apiGetJSON(ctx context.Context, endpoint, key string, target any) error {
	lastError := errors.New("Map Making App request failed")
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+endpoint, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "API "+key)
		request.Header.Set("User-Agent", "OhneGuessr/1")
		response, err := s.client.Do(request)
		if err == nil {
			body, readErr := httpjson.ReadLimited(response.Body, mmaMaxResponse)
			response.Body.Close()
			if readErr != nil {
				return fmt.Errorf("Map Making App response is too large")
			}
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				decoder := json.NewDecoder(bytes.NewReader(body))
				decoder.UseNumber()
				if err := decoder.Decode(target); err != nil {
					return fmt.Errorf("Map Making App returned invalid JSON")
				}
				if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
					return fmt.Errorf("Map Making App returned invalid JSON")
				}
				return nil
			}
			message := response.Status
			var payload struct {
				Message string `json:"message"`
				Error   string `json:"error"`
			}
			if json.Unmarshal(body, &payload) == nil {
				if candidate := defaultString(payload.Message, payload.Error); candidate != "" {
					message = candidate
				}
			}
			lastError = fmt.Errorf("Map Making App: %s", message)
			if response.StatusCode != http.StatusTooManyRequests && (response.StatusCode < 500 || response.StatusCode >= 600) {
				return lastError
			}
			delay := time.Duration(attempt+1) * 750 * time.Millisecond
			if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil && seconds >= 0 && seconds <= 30 {
				delay = time.Duration(seconds) * time.Second
			}
			if attempt < 2 {
				if err := httpjson.Wait(ctx, delay); err != nil {
					return err
				}
			}
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastError = fmt.Errorf("Map Making App request failed: %v", err)
		if attempt < 2 {
			if err := httpjson.Wait(ctx, time.Duration(attempt+1)*750*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return lastError
}

func integerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case string:
		result, err := strconv.ParseInt(typed, 10, 64)
		return result, err == nil
	default:
		return 0, false
	}
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}
