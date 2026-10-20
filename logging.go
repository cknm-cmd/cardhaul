package main

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	rlog "github.com/rclone/rclone/fs/log"
)

// appVersion is shown in the log; keep in sync with build/config.yml.
const appVersion = "2.0.1"

// LogPath is where CardHaul writes its log: ~/Library/Logs/CardHaul.log on macOS,
// %AppData%\CardHaul\CardHaul.log on Windows.
func LogPath(configDir string) string {
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Logs", "CardHaul.log")
		}
	}
	return filepath.Join(configDir, "CardHaul.log")
}

// setupLogging sends CardHaul's and rclone's messages to the log file (rotated at 5 MB).
func setupLogging(configDir string) {
	path := LogPath(configDir)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.LstdFlags)
	rlog.Handler.SetOutput(func(level slog.Level, text string) {
		log.Print("rclone: " + text)
	})
}
