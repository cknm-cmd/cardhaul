// Package volumes lists mounted external volumes (cameras, SD cards, USB drives).
package volumes

// Volume is a mounted external drive.
type Volume struct {
	Label string `json:"label"` // name shown in Finder / Explorer
	Path  string `json:"path"`  // mount point, e.g. /Volumes/Insta360 X5 or E:\
	// ConnectionID changes every time the device is plugged in (best effort per platform),
	// so a "cancel" can last for exactly one connection.
	ConnectionID string `json:"connectionId"`
	TotalBytes   uint64 `json:"totalBytes"`
}

// List returns the external volumes currently mounted.
func List() []Volume { return list() }

// Find returns the mounted volume with the given label.
func Find(label string) (Volume, bool) {
	for _, v := range list() {
		if v.Label == label {
			return v, true
		}
	}
	return Volume{}, false
}
