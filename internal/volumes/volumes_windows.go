package volumes

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
)

func list() []Volume {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var out []Volume
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		rootPtr, _ := windows.UTF16PtrFromString(root)
		driveType := windows.GetDriveType(rootPtr)
		// Cameras and card readers show up as removable; USB SSDs as fixed. Skip C: (system).
		if driveType != windows.DRIVE_REMOVABLE && driveType != windows.DRIVE_FIXED {
			continue
		}
		if strings.EqualFold(root, systemDrive()) {
			continue
		}
		label := make([]uint16, windows.MAX_PATH+1)
		fsName := make([]uint16, windows.MAX_PATH+1)
		var serial, maxLen, flags uint32
		if windows.GetVolumeInformation(rootPtr, &label[0], uint32(len(label)), &serial, &maxLen, &flags, &fsName[0], uint32(len(fsName))) != nil {
			continue // no media in the reader
		}
		name := windows.UTF16ToString(label)
		if name == "" {
			name = root[:2] // unlabeled volume: use the drive letter
		}
		var free, total, totalFree uint64
		_ = windows.GetDiskFreeSpaceEx(rootPtr, &free, &total, &totalFree)
		out = append(out, Volume{
			Label:        name,
			Path:         root,
			ConnectionID: fmt.Sprintf("%s-%08x", root[:1], serial),
			TotalBytes:   total,
		})
	}
	return out
}

func systemDrive() string {
	dir, err := windows.GetSystemWindowsDirectory()
	if err != nil || len(dir) < 3 {
		return `C:\`
	}
	return dir[:3]
}

// Eject asks Windows to safely remove the drive (same as Explorer's "Eject").
func Eject(v Volume) error {
	letter := strings.TrimSuffix(v.Path, `\`)
	script := fmt.Sprintf(`(New-Object -ComObject Shell.Application).Namespace(17).ParseName('%s').InvokeVerb('Eject')`, letter)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}
