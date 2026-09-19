package challenges

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const maxChallengeSize = 5 << 20

type Service struct {
	active  func() bool
	launch  func(string) error
	focus   func()
	runtime func() (*application.App, *application.WebviewWindow)
	mu      sync.RWMutex
	id      string
	data    string
	pending string
}

func NewService(
	active func() bool,
	launch func(string) error,
	focus func(),
	runtime func() (*application.App, *application.WebviewWindow),
) *Service {
	return &Service{active: active, launch: launch, focus: focus, runtime: runtime}
}

func IsFile(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".ohne")
}

func HandleSecondInstance(service *Service, data application.SecondInstanceData) bool {
	for _, argument := range data.Args[1:] {
		filename := argument
		if !filepath.IsAbs(filename) && data.WorkingDir != "" {
			filename = filepath.Join(data.WorkingDir, filename)
		}
		if IsFile(filename) {
			QueueFile(service, filename)
			return true
		}
	}
	return false
}

func (s *Service) LaunchChallenge(id, contents string) error {
	if s.active != nil && s.active() {
		return errors.New("end the current hosted session first")
	}
	id = strings.TrimSpace(id)
	if id == "" || len(contents) > maxChallengeSize || !json.Valid([]byte(contents)) {
		return errors.New("invalid challenge")
	}
	var header struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
		ID      string `json:"id"`
	}
	if json.Unmarshal([]byte(contents), &header) != nil || header.Format != "ohneguessr.challenge" ||
		header.Version != 1 || header.ID != id {
		return errors.New("invalid challenge")
	}
	s.mu.Lock()
	s.id = id
	s.data = contents
	s.mu.Unlock()
	return s.launch("/?view=game&challenge=" + url.QueryEscape(id))
}

func (s *Service) GetActiveChallenge(id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id == "" || id != s.id || s.data == "" {
		return "", errors.New("challenge is no longer available")
	}
	return s.data, nil
}

func (s *Service) TakePendingChallenge() (string, error) {
	s.mu.Lock()
	filename := s.pending
	s.pending = ""
	s.mu.Unlock()
	if filename == "" {
		return "", nil
	}
	return readChallengeFile(filename)
}

func (s *Service) SaveChallenge(suggestedName, contents string) (bool, error) {
	if len(contents) > maxChallengeSize || !json.Valid([]byte(contents)) {
		return false, errors.New("invalid challenge")
	}
	app, game := s.runtime()
	if app == nil {
		return false, errors.New("desktop runtime is not ready")
	}
	suggestedName = filepath.Base(strings.TrimSpace(suggestedName))
	if suggestedName == "." || suggestedName == "" {
		suggestedName = "challenge.ohne"
	}
	dialog := app.Dialog.SaveFile().
		SetMessage("Save challenge").
		SetButtonText("Save").
		SetFilename(suggestedName).
		CanCreateDirectories(true).
		AddFilter("OhneGuessr challenge", "*.ohne")
	if game != nil {
		dialog.AttachToWindow(game)
	}
	filename, err := dialog.PromptForSingleSelection()
	if err != nil || filename == "" {
		return false, err
	}
	if !IsFile(filename) {
		filename += ".ohne"
	}
	if err := mapfile.Write(filename, []byte(contents), 0o644); err != nil {
		return true, fmt.Errorf("save challenge: %w", err)
	}
	return true, nil
}

func QueueFile(s *Service, filename string) {
	s.mu.Lock()
	s.pending = filename
	s.mu.Unlock()
	if s.focus != nil {
		s.focus()
	}
	if app, _ := s.runtime(); app != nil {
		app.Event.Emit("challenge:file-opened")
	}
}

func readChallengeFile(filename string) (string, error) {
	if !IsFile(filename) {
		return "", errors.New("only .ohne challenge files can be opened")
	}
	info, err := os.Stat(filename)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxChallengeSize {
		return "", errors.New("challenge file is invalid or too large")
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(contents), nil
}
