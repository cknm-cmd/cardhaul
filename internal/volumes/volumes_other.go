//go:build !darwin && !windows

package volumes

import "errors"

func list() []Volume { return nil }

func Eject(v Volume) error { return errors.New("eject isn't supported on this platform") }
