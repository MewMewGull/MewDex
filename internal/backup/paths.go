package backup

import (
	"errors"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Archive paths are portable across the supported Windows, Mac and Linux
// devices. In particular, reject Windows alternate streams and device names on
// every host, including when an archive is produced or inspected on Linux.
func validPath(name string) bool {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 || strings.TrimRight(part, " .") != part || strings.ContainsAny(part, `\<>:"|?*`) {
			return false
		}
		for _, c := range part {
			if unicode.IsControl(c) {
				return false
			}
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		runes := []rune(base)
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || base == "CLOCK$" || (len(runes) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("0123456789¹²³", runes[3])) {
			return false
		}
	}
	return true
}

func validate(manifest Manifest, limits Limits) error {
	if manifest.Format != 1 || manifest.Kind != "mewdex-device" || len(manifest.Metadata) > maxManifestBytes/2 || len(manifest.Files) > limits.MaxFiles {
		return errors.New("unsupported or oversized device backup manifest")
	}
	seen := map[string]bool{}
	var total int64
	for _, file := range manifest.Files {
		key := strings.ToLower(file.Name)
		if !validPath(file.Name) || key == "manifest.json" || seen[key] || len(file.SHA256) != 64 || strings.Trim(file.SHA256, "0123456789abcdef") != "" || file.Bytes < 0 || file.Bytes > limits.MaxBytes-total {
			return errors.New("invalid, duplicate or oversized backup entry")
		}
		seen[key] = true
		total += file.Bytes
	}
	for key := range seen {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return errors.New("backup file collides with a parent directory")
			}
		}
	}
	return nil
}
