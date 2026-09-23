package desktop

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ohne-b/OhneGuessr/internal/backend"
	"github.com/ohne-b/OhneGuessr/internal/challenges"
	learnablemeta "github.com/ohne-b/OhneGuessr/internal/learnable-meta"
	"github.com/ohne-b/OhneGuessr/internal/local-party"
	mapmakingapp "github.com/ohne-b/OhneGuessr/internal/map-making-app"
	"github.com/ohne-b/OhneGuessr/internal/pluginmanager"
	"github.com/ohne-b/OhneGuessr/internal/updates"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

func Run(frontendAssets fs.FS, version string, arguments []string) error {
	updater.HandleHelperMode()

	frontend, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		return err
	}
	var startupChallenge string
	configArgs := make([]string, 0, len(arguments))
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "-data-dir" || argument == "--data-dir" {
			configArgs = append(configArgs, argument)
			if index+1 < len(arguments) {
				index++
				configArgs = append(configArgs, arguments[index])
			}
			continue
		}
		if strings.HasPrefix(argument, "-data-dir=") || strings.HasPrefix(argument, "--data-dir=") {
			configArgs = append(configArgs, argument)
			continue
		}
		if challenges.IsFile(argument) {
			if startupChallenge == "" {
				startupChallenge, _ = filepath.Abs(argument)
			}
			continue
		}
		configArgs = append(configArgs, argument)
	}
	dataDir, err := backend.ResolveDataDir(configArgs)
	if err != nil {
		return err
	}
	core, err := backend.New(
		dataDir,
		mapmakingapp.NewPlugin,
		learnablemeta.NewPlugin,
	)
	if err != nil {
		return err
	}
	desktop := &DesktopService{backend: core}
	party := localparty.New(frontend, core.HasMap, desktop.launchGame, func(id string) {
		desktop.mu.RLock()
		wails := desktop.wails
		desktop.mu.RUnlock()
		if wails != nil {
			wails.Event.Emit("party:changed", id)
		}
	})
	desktop.exclusiveActive = party.Active
	desktop.stopExclusive = func() { _ = party.StopParty("") }
	challengeService := challenges.NewService(
		desktop.exclusiveSessionActive,
		func(target string) error { return desktop.launchGame(target, "") },
		desktop.FocusLauncher,
		func() (*application.App, *application.WebviewWindow) {
			desktop.mu.RLock()
			defer desktop.mu.RUnlock()
			return desktop.wails, desktop.game
		},
	)
	desktop.secondInstanceHandler = func(data application.SecondInstanceData) bool {
		return challenges.HandleSecondInstance(challengeService, data)
	}

	backendHandler := core.Handler()
	handler := http.NewServeMux()
	handler.Handle("/api", backendHandler)
	handler.Handle("/api/", backendHandler)
	handler.Handle("/data/", backendHandler)
	handler.Handle("/", application.AssetFileServerFS(frontend))

	singleInstanceID := "5ac23bb7-9f87-48bc-a73f-e4fe65ce85c1"
	if runtime.GOOS == "linux" {
		// Wails beta.23 expects a D-Bus name; preserve the previous Linux lock identity.
		singleInstanceID = "org.wails_app_" + strings.ReplaceAll(singleInstanceID, "-", "_")
	}
	wailsApp := application.New(application.Options{
		Name:        "OhneGuessr",
		Description: "A free, lean, local GeoGuessr alternative.",
		Assets: application.AssetOptions{
			Handler: handler,
		},
		OnShutdown: desktop.shutdown,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               singleInstanceID,
			OnSecondInstanceLaunch: desktop.secondInstance,
		},
		FileAssociations: []string{".ohne"},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: filepath.Join(dataDir, "webview"),
		},
		Linux: application.LinuxOptions{
			ProgramName: "ohneguessr",
		},
	})
	updateService, err := updates.New(wailsApp, version, dataDir)
	if err != nil {
		return err
	}
	pluginService := pluginmanager.New(dataDir)
	desktop.wails = wailsApp
	wailsApp.Event.OnApplicationEvent(events.Common.ApplicationOpenedWithFile, func(event *application.ApplicationEvent) {
		challenges.QueueFile(challengeService, event.Context().Filename())
	})
	wailsApp.RegisterService(application.NewService(desktop))
	wailsApp.RegisterService(application.NewService(updateService))
	wailsApp.RegisterService(application.NewService(pluginService))
	wailsApp.RegisterService(application.NewService(party))
	wailsApp.RegisterService(application.NewService(challengeService))
	launcher := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                       "launcher",
		Title:                      "OhneGuessr",
		Frameless:                  true,
		Width:                      1400,
		Height:                     900,
		MinWidth:                   760,
		MinHeight:                  520,
		StartState:                 application.WindowStateNormal,
		InitialPosition:            application.WindowCentered,
		BackgroundColour:           application.NewRGB(11, 11, 11),
		DefaultContextMenuDisabled: true,
		EnableFileDrop:             true,
		URL:                        "/?view=launcher",
		Windows: application.WindowsWindow{
			Theme:                      application.Dark,
			WebView2CompositionHosting: true,
		},
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: application.WebviewGpuPolicyOnDemand,
		},
	})
	desktop.launcher = launcher
	if startupChallenge != "" {
		challenges.QueueFile(challengeService, startupChallenge)
	}
	launcher.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		go wailsApp.Quit()
	})
	return wailsApp.Run()
}
