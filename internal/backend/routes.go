package backend

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ohne-b/OhneGuessr/internal/httpjson"
)

func (a *Backend) Handler() http.Handler {
	mux := http.NewServeMux()
	a.registerMapRoutes(mux)
	for _, plugin := range a.mapPlugins {
		plugin.RegisterRoutes(mux)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		mux.HandleFunc(method+" /api/{path...}", func(w http.ResponseWriter, _ *http.Request) {
			httpjson.Write(w, http.StatusNotFound, map[string]string{"error": "not found"})
		})
	}
	mux.HandleFunc("GET /data/{file...}", a.serveMapData)
	return mux
}

func (a *Backend) registerMapRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/maps", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Name      string          `json:"name"`
			Folder    string          `json:"folder"`
			Locations json.RawMessage `json:"locations"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		entry, err := a.maps.createLocal(body.Name, body.Locations, body.Folder)
		if err != nil {
			return nil, 0, mapMutationResponse(err, "create failed")
		}
		return entry, http.StatusOK, nil
	}))
	mux.HandleFunc("POST /api/maps/{id}/rounds", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Count   int64 `json:"count"`
			Exclude []int `json:"exclude"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		if body.Count <= 0 {
			return nil, 0, httpjson.Error(http.StatusBadRequest, "count must be positive")
		}
		excluded := make(map[int]bool, len(body.Exclude))
		for _, index := range body.Exclude {
			if index < 0 {
				return nil, 0, httpjson.Error(http.StatusBadRequest, "excluded indexes must be non-negative")
			}
			excluded[index] = true
		}
		file, err := a.maps.openByID(r.PathValue("id"))
		if err != nil {
			return nil, 0, mapMutationResponse(err, "map load failed")
		}
		defer file.Close()
		result, err := sampleMapLocations(file, body.Count, excluded)
		if errors.Is(err, errInvalidMapData) {
			return nil, 0, httpjson.Error(http.StatusUnprocessableEntity, "map data is invalid")
		}
		if err != nil {
			return nil, 0, httpjson.Error(http.StatusInternalServerError, "map load failed")
		}
		return result, http.StatusOK, nil
	}))
	mux.HandleFunc("PATCH /api/maps/{id}", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Name   *string `json:"name"`
			Folder *string `json:"folder"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		entry, err := a.maps.updateMap(r.PathValue("id"), body.Name, body.Folder)
		if err != nil {
			return nil, 0, mapMutationResponse(err, "update failed")
		}
		return entry, http.StatusOK, nil
	}))
	mux.HandleFunc("DELETE /api/maps/{id}", httpjson.Handler(func(r *http.Request) (any, int, error) {
		err := a.maps.deleteLocal(r.PathValue("id"))
		if err != nil {
			return nil, 0, mapMutationResponse(err, "delete failed")
		}
		return map[string]any{"ok": true}, http.StatusOK, nil
	}))
	mux.HandleFunc("POST /api/folders", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Parent string `json:"parent"`
			Name   string `json:"name"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		folder, err := a.maps.createFolder(body.Parent, body.Name)
		if err != nil {
			return nil, 0, mapMutationResponse(err, "create folder failed")
		}
		return map[string]string{"path": folder}, http.StatusOK, nil
	}))
	mux.HandleFunc("PATCH /api/folders", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		folder, err := a.maps.renameFolder(body.Path, body.Name)
		if err != nil {
			return nil, 0, mapMutationResponse(err, "rename folder failed")
		}
		return map[string]string{"path": folder}, http.StatusOK, nil
	}))
	mux.HandleFunc("DELETE /api/folders", httpjson.Handler(func(r *http.Request) (any, int, error) {
		body, err := httpjson.Decode[struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
		}](r)
		if err != nil {
			return nil, 0, err
		}
		deleted, err := a.deleteFolder(body.Path, body.Recursive)
		if err != nil {
			return nil, 0, mapMutationResponse(err, "delete folder failed")
		}
		return map[string]any{"ok": true, "deletedMapIds": deleted}, http.StatusOK, nil
	}))
}

func (a *Backend) deleteFolder(folder string, recursive bool) ([]string, error) {
	clean, _ := normalizeRelative(folder)
	var restore func()
	for _, plugin := range a.mapPlugins {
		if !strings.EqualFold(clean, plugin.MapPolicy().Root) {
			continue
		}
		if !recursive {
			return nil, errFolderNotEmpty
		}
		if plugin.Enabled() {
			if _, err := plugin.SetEnabled(false); err != nil {
				return nil, err
			}
			restore = func() { _, _ = plugin.SetEnabled(true) }
		}
		break
	}
	deleted, err := a.maps.deleteFolder(folder, recursive)
	if err != nil && restore != nil {
		restore()
	}
	return deleted, err
}

func mapMutationResponse(err error, fallback string) error {
	switch {
	case errors.Is(err, errMapNotFound), errors.Is(err, errFolderNotFound),
		errors.Is(err, errMapDataMissing):
		return httpjson.Error(http.StatusNotFound, err.Error())
	case errors.Is(err, errNoLocations), errors.Is(err, errNameRequired),
		errors.Is(err, errNameTooLong), errors.Is(err, errInvalidFolder),
		errors.Is(err, errNoMutation):
		return httpjson.Error(http.StatusBadRequest, err.Error())
	case errors.Is(err, errManagedMap), errors.Is(err, errManagedFolder),
		errors.Is(err, errMoveRestricted), errors.Is(err, errFolderExists),
		errors.Is(err, errFolderNotEmpty):
		return httpjson.Error(http.StatusConflict, err.Error())
	default:
		return httpjson.Error(http.StatusInternalServerError, fallback)
	}
}

func (a *Backend) serveMapData(w http.ResponseWriter, r *http.Request) {
	file, info, err := a.maps.openPublic(r.PathValue("file"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
