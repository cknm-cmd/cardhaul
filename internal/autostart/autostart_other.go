//go:build !darwin && !windows

package autostart

func Set(enabled bool) error { return nil }
