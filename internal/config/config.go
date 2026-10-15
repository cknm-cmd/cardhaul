// Package config stores CardHaul's settings, per-device state and run history as JSON files in
// the user's config directory (~/Library/Application Support/CardHaul on macOS,
// %AppData%\CardHaul on Windows).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Device is a camera or card CardHaul uploads from, matched by its volume label.
type Device struct {
	Label  string `json:"label"`  // volume name shown in Finder / Explorer
	Folder string `json:"folder"` // folder on the device to upload, "." for everything
	Dest   string `json:"dest"`   // folder in Drive, under the CardHaul root folder
}

// VerifyMode decides how uploads are checked before the device is declared safe to clear.
type VerifyMode string

const (
	VerifyChecksum VerifyMode = "checksum"
	VerifySize     VerifyMode = "size"
)

type Settings struct {
	Devices        []Device   `json:"devices"`
	Verify         VerifyMode `json:"verify"`
	Paused         bool       `json:"paused"`
	StartAtLogin   bool       `json:"startAtLogin"`
	SkipFiles      []string   `json:"skipFiles"`
	Transfers      int        `json:"transfers"`
	VerifyMBPerSec float64    `json:"verifyMBPerSec"` // measured device read speed, for time estimates
}

// DeviceState remembers what has been verified, so reconnecting a device with the same files is quiet.
type DeviceState struct {
	VerifiedFingerprint string `json:"verifiedFingerprint,omitempty"`
	FailedFingerprint   string `json:"failedFingerprint,omitempty"`
}

// Run is one entry in the upload history.
type Run struct {
	Device   string    `json:"device"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Files    int       `json:"files"`
	Bytes    int64     `json:"bytes"`
	Result   string    `json:"result"` // verified, failed, cancelled, paused, disconnected, error
	Message  string    `json:"message,omitempty"`
}

func defaults() Settings {
	return Settings{
		Verify:         VerifyChecksum,
		StartAtLogin:   true,
		SkipFiles:      []string{"fileinfo_list.list"},
		Transfers:      4,
		VerifyMBPerSec: 30,
	}
}

// Store guards the on-disk files.
type Store struct {
	mu       sync.Mutex
	dir      string
	settings Settings
	states   map[string]DeviceState
	history  []Run
}

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "CardHaul"), nil
}

func Open() (*Store, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, settings: defaults(), states: map[string]DeviceState{}}
	readJSON(filepath.Join(dir, "settings.json"), &s.settings)
	readJSON(filepath.Join(dir, "devices-state.json"), &s.states)
	readJSON(filepath.Join(dir, "history.json"), &s.history)
	if s.settings.Transfers <= 0 {
		s.settings.Transfers = 4
	}
	if s.settings.VerifyMBPerSec <= 0 {
		s.settings.VerifyMBPerSec = 30
	}
	return s, nil
}

func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.settings
	out.Devices = append([]Device(nil), s.settings.Devices...)
	out.SkipFiles = append([]string(nil), s.settings.SkipFiles...)
	return out
}

func (s *Store) UpdateSettings(change func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&s.settings)
	return writeJSON(filepath.Join(s.dir, "settings.json"), s.settings)
}

func (s *Store) DeviceState(label string) DeviceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[label]
}

func (s *Store) SetDeviceState(label string, st DeviceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[label] = st
	return writeJSON(filepath.Join(s.dir, "devices-state.json"), s.states)
}

func (s *Store) History() []Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Run(nil), s.history...)
}

func (s *Store) AddRun(r Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append([]Run{r}, s.history...)
	if len(s.history) > 200 {
		s.history = s.history[:200]
	}
	return writeJSON(filepath.Join(s.dir, "history.json"), s.history)
}

func readJSON(path string, v any) {
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, v)
	}
}

// writeJSON writes atomically so a crash never leaves a half-written file.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
