package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/cknm-cmd/cardhaul/internal/autostart"
	"github.com/cknm-cmd/cardhaul/internal/config"
	"github.com/cknm-cmd/cardhaul/internal/engine"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/tray-template.png
var trayTemplateIcon []byte

//go:embed build/tray-color.png
var trayColorIcon []byte

func init() {
	application.RegisterEvent[engine.Snapshot]("state")
}

func main() {
	store, err := config.Open()
	if err != nil {
		log.Fatal(err)
	}
	configDir, _ := config.Dir()
	// Re-register start-at-login on every launch, so it points at wherever CardHaul now lives
	// (a moved portable .exe, or the copy an installer just put in place).
	if store.Settings().StartAtLogin {
		_ = autostart.Set(true)
	}
	notifier := notifications.New()

	app := application.New(application.Options{
		Name:        "CardHaul",
		Description: "Plug in a camera or SD card: new footage uploads one-way to Google Drive and is verified.",
		Services: []application.Service{
			application.NewService(notifier),
		},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory, // lives in the menu bar, no Dock icon
		},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "io.github.cknm-cmd.cardhaul"},
	})

	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplateIcon)
	} else {
		tray.SetIcon(trayColorIcon)
	}
	tray.SetTooltip("CardHaul")

	windows := &windowSet{app: app}
	popover := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "tray",
		URL:             "/#/tray",
		Width:           320,
		Height:          380,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableResize:   true,
		HideOnEscape:    true,
		HideOnFocusLost: true,
		Windows:         application.WindowsWindow{HiddenOnTaskbar: true},
		Mac:             application.MacWindow{Backdrop: application.MacBackdropTranslucent},
	})
	popover.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		popover.Hide()
		e.Cancel()
	})
	tray.AttachWindow(popover).WindowOffset(6)

	var askOnce sync.Once
	notify := func(title, body string) {
		askOnce.Do(func() { _, _ = notifier.RequestNotificationAuthorization() })
		_ = notifier.SendNotification(notifications.NotificationOptions{ID: fmt.Sprint(title, body), Title: title, Body: body})
	}

	eng := engine.New(store, configDir, func(s engine.Snapshot) {
		tray.SetLabel(trayLabel(s))
		tray.SetTooltip("CardHaul: " + trayTooltip(s))
		app.Event.Emit("state", s)
	}, notify)

	app.RegisterService(application.NewService(&AppService{store: store, engine: eng, configDir: configDir, windows: windows}))

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go eng.Run(ctx)

	// First run: open Settings so the user can sign in and add a device.
	if len(store.Settings().Devices) == 0 {
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			windows.show("settings")
		})
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// windowSet creates the Settings and History windows on demand and reuses them.
type windowSet struct {
	app *application.App
	mu  sync.Mutex
	win map[string]*application.WebviewWindow
}

func (w *windowSet) show(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.win == nil {
		w.win = map[string]*application.WebviewWindow{}
	}
	if win, ok := w.win[name]; ok {
		win.Show()
		win.Focus()
		return
	}
	title, width, height := "CardHaul settings", 520, 660
	if name == "history" {
		title, width, height = "CardHaul history", 640, 520
	}
	win := w.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   name,
		Title:  title,
		URL:    "/#/" + name,
		Width:  width,
		Height: height,
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		win.Hide() // keep it around; the app itself keeps running in the menu bar
		e.Cancel()
	})
	w.win[name] = win
	win.Show()
	win.Focus()
}

func trayLabel(s engine.Snapshot) string {
	switch s.Phase {
	case engine.PhaseUploading:
		if s.BytesTotal > 0 {
			return fmt.Sprintf("%d%%", s.BytesDone*100/s.BytesTotal)
		}
	case engine.PhaseVerifying:
		return fmt.Sprintf("%d/%d", s.FilesDone, s.FilesTotal)
	case engine.PhaseProblem:
		return "!"
	}
	return ""
}

func trayTooltip(s engine.Snapshot) string {
	switch {
	case s.Paused:
		return "paused"
	case s.Phase == engine.PhaseUploading:
		return "uploading " + s.Device + " " + trayLabel(s)
	case s.Phase == engine.PhaseVerifying:
		return "verifying " + s.Device
	case s.Phase == engine.PhaseDone:
		return s.Device + " is safe to clear"
	case s.Phase == engine.PhaseProblem:
		return "needs attention"
	}
	return "idle"
}
