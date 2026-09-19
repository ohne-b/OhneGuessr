package updates

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

const (
	updateManifestURL = "https://github.com/ohne-b/OhneGuessr/releases/latest/download/latest.json"
	updatePublicKey   = "zHw2caFsAlJGZ02o83rf9d9Rf7yRyiWuz/+vrAYIsdU="
)

type UpdateService struct {
	updater *updater.Updater
	app     *application.App
	dataDir string
}

func New(app *application.App, currentVersion, dataDir string) (*UpdateService, error) {
	service := &UpdateService{app: app, dataDir: dataDir}
	if currentVersion == "dev" || !platformUpdatesSupported() {
		return service, nil
	}

	publicKey, err := base64.StdEncoding.DecodeString(updatePublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("invalid updater public key")
	}
	provider, err := endpoint.New(endpoint.Config{
		URL:        updateManifestURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Minute},
	})
	if err != nil {
		return nil, fmt.Errorf("configure update endpoint: %w", err)
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: currentVersion,
		Providers:      []updater.Provider{provider},
		PublicKey:      publicKey,
	}); err != nil {
		return nil, fmt.Errorf("configure updater: %w", err)
	}
	service.updater = app.Updater
	return service, nil
}

func (u *UpdateService) CheckAvailable() (string, error) {
	if u.updater == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	release, err := u.updater.Check(ctx)
	if err != nil || release == nil {
		return "", err
	}
	return release.Version, nil
}

func (u *UpdateService) OpenUpdater() error {
	if u.updater == nil {
		return nil
	}
	return u.openUpdater()
}
