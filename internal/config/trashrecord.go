package config

import "path/filepath"

// TrashRecordPath returns where sushi notes what it moves to a trash that
// doesn't say where its items came from (~/.Trash), so its trash browser
// can put them back: trash.json beside the config. It is empty without a
// home folder, and then nothing is noted.
func TrashRecordPath() string {
	dir, err := getConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "trash.json")
}
