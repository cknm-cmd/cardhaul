package volumes

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func list() []Volume {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil
	}
	var root syscall.Stat_t
	_ = syscall.Stat("/", &root)

	var out []Volume
	for _, e := range entries {
		path := filepath.Join("/Volumes", e.Name())
		var st syscall.Stat_t
		if syscall.Stat(path, &st) != nil || st.Dev == root.Dev {
			continue // the startup disk appears in /Volumes as a link to /
		}
		var fsStat syscall.Statfs_t
		total := uint64(0)
		if syscall.Statfs(path, &fsStat) == nil {
			total = fsStat.Blocks * uint64(fsStat.Bsize)
			// Skip disk images and network shares: only local, removable-style volumes.
			if fsStat.Flags&0x1000 == 0 { // MNT_LOCAL
				continue
			}
		}
		out = append(out, Volume{
			Label: e.Name(),
			Path:  path,
			// The kernel assigns a device number per mount; combined with the mount's inode
			// generation this is unique enough per connection (CardHaul also watches for unplugs).
			ConnectionID: fmt.Sprintf("%d-%d", st.Dev, fsStat.Fsid.Val[0]),
			TotalBytes:   total,
		})
	}
	return out
}

// Eject unmounts the volume so it can be unplugged.
func Eject(v Volume) error {
	out, err := exec.Command("diskutil", "eject", v.Path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}
