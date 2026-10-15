// Package autostart starts CardHaul when the user logs in.
package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const label = "io.github.cknm-cmd.cardhaul"

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// Set turns start-at-login on or off, using a LaunchAgent that opens the app bundle.
func Set(enabled bool) error {
	if !enabled {
		err := os.Remove(plistPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	target := exe
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		target = exe[:i+4] // open the .app so macOS treats it as a normal app launch
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>%s</string>
    <key>ProgramArguments</key>
    <array><string>/usr/bin/open</string><string>-g</string><string>%s</string></array>
    <key>RunAtLoad</key><true/>
</dict>
</plist>
`, label, target)
	if err := os.MkdirAll(filepath.Dir(plistPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(plistPath(), []byte(plist), 0o644)
}
