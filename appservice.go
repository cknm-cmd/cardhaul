package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/cknm-cmd/cardhaul/internal/autostart"
	"github.com/cknm-cmd/cardhaul/internal/config"
	"github.com/cknm-cmd/cardhaul/internal/engine"
	"github.com/cknm-cmd/cardhaul/internal/gauth"
	"github.com/cknm-cmd/cardhaul/internal/volumes"
)

// AppService is what the frontend calls. Every exported method becomes a TypeScript binding.
type AppService struct {
	store     *config.Store
	engine    *engine.Engine
	configDir string
	windows   *windowSet
}

func (s *AppService) State() engine.Snapshot { return s.engine.Snapshot() }

func (s *AppService) Pause()    { s.engine.Pause() }
func (s *AppService) Resume()   { s.engine.Resume() }
func (s *AppService) Cancel()   { s.engine.Cancel() }
func (s *AppService) CheckNow() { s.engine.CheckNow() }

func (s *AppService) Settings() config.Settings { return s.store.Settings() }

func (s *AppService) SetVerify(mode string) error {
	if mode != string(config.VerifyChecksum) && mode != string(config.VerifySize) {
		return fmt.Errorf("unknown verify mode %q", mode)
	}
	return s.store.UpdateSettings(func(st *config.Settings) { st.Verify = config.VerifyMode(mode) })
}

func (s *AppService) SetStartAtLogin(enabled bool) error {
	if err := autostart.Set(enabled); err != nil {
		return err
	}
	return s.store.UpdateSettings(func(st *config.Settings) { st.StartAtLogin = enabled })
}

// AvailableVolumes lists plugged-in drives that aren't set up as devices yet, for "Add device".
func (s *AppService) AvailableVolumes() []volumes.Volume {
	configured := map[string]bool{}
	for _, d := range s.store.Settings().Devices {
		configured[d.Label] = true
	}
	var out []volumes.Volume
	for _, v := range volumes.List() {
		if !configured[v.Label] {
			out = append(out, v)
		}
	}
	return out
}

// SuggestFolder picks DCIM when the drive has one (cameras, drones), otherwise the whole drive.
func (s *AppService) SuggestFolder(label string) string {
	if v, ok := volumes.Find(label); ok {
		if info, err := os.Stat(v.Path + string(os.PathSeparator) + "DCIM"); err == nil && info.IsDir() {
			return "DCIM"
		}
	}
	return "."
}

func (s *AppService) AddDevice(label, folder string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("choose a drive first")
	}
	if folder == "" {
		folder = "."
	}
	err := s.store.UpdateSettings(func(st *config.Settings) {
		for _, d := range st.Devices {
			if d.Label == label {
				return
			}
		}
		st.Devices = append(st.Devices, config.Device{Label: label, Folder: folder, Dest: label})
	})
	s.engine.Kick()
	return err
}

func (s *AppService) RemoveDevice(label string) error {
	return s.store.UpdateSettings(func(st *config.Settings) {
		var kept []config.Device
		for _, d := range st.Devices {
			if d.Label != label {
				kept = append(kept, d)
			}
		}
		st.Devices = kept
	})
}

func (s *AppService) SignIn() error {
	if _, err := gauth.SignIn(context.Background(), s.configDir); err != nil {
		return err
	}
	s.engine.Kick()
	return nil
}

func (s *AppService) SignOut() { gauth.SignOut(); s.engine.Kick() }

func (s *AppService) Account() (gauth.Account, error) {
	return gauth.AccountInfo(context.Background(), s.configDir)
}

func (s *AppService) History() []config.Run { return s.store.History() }

// OpenLog shows CardHaul's log file (useful when something goes wrong).
func (s *AppService) OpenLog() error {
	return browser.OpenFile(LogPath(s.configDir))
}

// VerifyEstimate returns how long checking the given number of bytes would take in each mode.
func (s *AppService) VerifyEstimate(totalBytes int64) map[string]float64 {
	mbps := s.store.Settings().VerifyMBPerSec
	return map[string]float64{"checksum": float64(totalBytes) / 1e6 / mbps, "size": 5}
}

func (s *AppService) Eject(label string) error {
	v, ok := volumes.Find(label)
	if !ok {
		return fmt.Errorf("%s isn't connected", label)
	}
	return volumes.Eject(v)
}

// OpenDrive opens the CardHaul folder in Google Drive in the browser.
func (s *AppService) OpenDrive() error {
	return browser.OpenURL("https://drive.google.com/drive/search?q=" + url.QueryEscape(engine.DriveRoot))
}

func (s *AppService) OpenSettings() { s.windows.show("settings") }
func (s *AppService) OpenHistory()  { s.windows.show("history") }
func (s *AppService) Platform() string { return runtime.GOOS }

func (s *AppService) Quit() { application.Get().Quit() }
