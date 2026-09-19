package desktop

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ohne-b/OhneGuessr/internal/backend"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type GameWindowState struct {
	Open       bool   `json:"open"`
	MapID      string `json:"mapId,omitempty"`
	Fullscreen bool   `json:"fullscreen"`
}

type DesktopService struct {
	backend               *backend.Backend
	exclusiveActive       func() bool
	stopExclusive         func()
	secondInstanceHandler func(application.SecondInstanceData) bool
	wails                 *application.App
	mu                    sync.RWMutex
	launcher              *application.WebviewWindow
	game                  *application.WebviewWindow
	gameMap               string
	gameTarget            string
}

func (d *DesktopService) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d.stopExclusiveSession()
	_ = d.backend.Shutdown(ctx)
}

func (d *DesktopService) exclusiveSessionActive() bool {
	return d.exclusiveActive != nil && d.exclusiveActive()
}

func (d *DesktopService) stopExclusiveSession() {
	if d.stopExclusive != nil {
		d.stopExclusive()
	}
}

func (d *DesktopService) secondInstance(data application.SecondInstanceData) {
	if d.secondInstanceHandler != nil && d.secondInstanceHandler(data) {
		return
	}
	d.FocusLauncher()
}

func (d *DesktopService) LaunchMap(mapID, mode string) error {
	if d.exclusiveSessionActive() {
		return errors.New("end the current hosted session first")
	}
	mapID = strings.TrimSpace(mapID)
	if mapID == "" || !d.backend.HasMap(mapID) {
		return errors.New("map not found")
	}
	return d.launchGame(gameURL(mapID, mode), mapID)
}

func (d *DesktopService) launchGame(targetURL, mapID string) error {
	d.mu.Lock()
	game := d.game
	if game != nil {
		sameTarget := d.gameTarget == targetURL
		if !sameTarget {
			d.gameMap = mapID
			d.gameTarget = targetURL
		}
		d.mu.Unlock()
		if !sameTarget {
			game.Hide()
			game.SetURL(targetURL)
		} else {
			game.Show()
			game.UnMinimise()
			game.Focus()
		}
		d.emitGameState()
		return nil
	}
	if d.wails == nil {
		d.mu.Unlock()
		return errors.New("desktop runtime is not ready")
	}
	game = d.wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                       "game",
		Title:                      "OhneGuessr",
		Width:                      1400,
		Height:                     900,
		MinWidth:                   800,
		MinHeight:                  600,
		StartState:                 application.WindowStateMaximised,
		Hidden:                     true,
		BackgroundColour:           application.NewRGB(11, 11, 11),
		DefaultContextMenuDisabled: true,
		URL:                        targetURL,
		Windows: application.WindowsWindow{
			Theme: application.Dark,
		},
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: application.WebviewGpuPolicyOnDemand,
		},
	})
	d.game = game
	d.gameMap = mapID
	d.gameTarget = targetURL
	d.mu.Unlock()

	game.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		d.mu.Lock()
		if d.game == game {
			d.game = nil
			d.gameMap = ""
			d.gameTarget = ""
		}
		d.mu.Unlock()
		d.stopExclusiveSession()
		d.emitGameState()
	})
	game.OnWindowEvent(events.Common.WindowFullscreen, func(*application.WindowEvent) {
		d.emitGameState()
	})
	game.OnWindowEvent(events.Common.WindowUnFullscreen, func(*application.WindowEvent) {
		d.emitGameState()
	})
	d.emitGameState()
	return nil
}

func (d *DesktopService) FocusLauncher() {
	d.mu.RLock()
	launcher := d.launcher
	d.mu.RUnlock()
	if launcher != nil {
		launcher.Show()
		launcher.UnMinimise()
		launcher.Focus()
	}
}

func (d *DesktopService) ExportMaps() (bool, error) {
	d.mu.RLock()
	app, launcher := d.wails, d.launcher
	d.mu.RUnlock()
	if app == nil {
		return false, errors.New("desktop runtime is not ready")
	}
	dialog := app.Dialog.SaveFile().
		SetMessage("Export all maps").
		SetButtonText("Export").
		SetFilename("ohneguessr-maps-"+time.Now().Format("2006-01-02")+".zip").
		CanCreateDirectories(true).
		AddFilter("ZIP archive", "*.zip")
	if launcher != nil {
		dialog.AttachToWindow(launcher)
	}
	filename, err := dialog.PromptForSingleSelection()
	if err != nil || filename == "" {
		return false, err
	}
	if !strings.EqualFold(filepath.Ext(filename), ".zip") {
		filename += ".zip"
	}
	return true, d.backend.ExportMaps(filename)
}

func (d *DesktopService) CloseGame() {
	d.mu.RLock()
	game := d.game
	d.mu.RUnlock()
	if game != nil {
		game.Close()
	}
}

func (d *DesktopService) SetGameFullscreen(enabled bool) GameWindowState {
	d.mu.RLock()
	game := d.game
	d.mu.RUnlock()
	if game != nil {
		if enabled {
			game.Fullscreen()
		} else {
			game.UnFullscreen()
		}
	}
	return d.GetGameWindowState()
}

func (d *DesktopService) GetGameWindowState() GameWindowState {
	d.mu.RLock()
	game, mapID := d.game, d.gameMap
	d.mu.RUnlock()
	state := GameWindowState{Open: game != nil, MapID: mapID}
	if game != nil {
		state.Fullscreen = game.IsFullscreen()
	}
	return state
}

func (d *DesktopService) GameReady(mapID string) {
	d.mu.RLock()
	game, activeMap := d.game, d.gameMap
	d.mu.RUnlock()
	if game == nil || mapID != activeMap {
		return
	}
	game.Show()
	if !game.IsFullscreen() {
		game.Maximise()
	}
	game.Focus()
	d.emitGameState()
}

func (d *DesktopService) SetGameWindowTheme(background, foreground string) {
	backgroundColour, backgroundOK := parseColourRef(background)
	foregroundColour, foregroundOK := parseColourRef(foreground)
	if !backgroundOK || !foregroundOK {
		return
	}
	d.mu.RLock()
	game := d.game
	d.mu.RUnlock()
	applyWindowTheme(game, backgroundColour, foregroundColour)
}

func (d *DesktopService) emitGameState() {
	d.mu.RLock()
	wails := d.wails
	d.mu.RUnlock()
	if wails != nil {
		wails.Event.Emit("desktop:game-state", d.GetGameWindowState())
	}
}

func gameURL(mapID, mode string) string {
	target := "/?view=game&map=" + url.QueryEscape(mapID)
	if mode = strings.TrimSpace(mode); mode != "" {
		target += "&mode=" + url.QueryEscape(mode)
	}
	return target
}

func parseColourRef(value string) (uint32, bool) {
	hex := strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return 0, false
	}
	rgb, err := strconv.ParseUint(hex, 16, 24)
	if err != nil {
		return 0, false
	}
	return uint32(rgb&0xff)<<16 | uint32(rgb&0xff00) | uint32(rgb>>16), true
}
