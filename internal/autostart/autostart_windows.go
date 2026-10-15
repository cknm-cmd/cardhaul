// Package autostart starts CardHaul when the user logs in.
package autostart

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Set turns start-at-login on or off via the per-user Run registry key.
func Set(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		err := k.DeleteValue("CardHaul")
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue("CardHaul", `"`+exe+`"`)
}
