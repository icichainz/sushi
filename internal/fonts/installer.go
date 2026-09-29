package fonts

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// NerdFont represents a downloadable Nerd Font
type NerdFont struct {
	Name    string
	URL     string
	Version string
}

// AvailableFonts lists the Nerd Fonts that can be installed
var AvailableFonts = []NerdFont{
	{
		Name:    "JetBrainsMono",
		URL:     "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/JetBrainsMono.zip",
		Version: "3.3.0",
	},
	{
		Name:    "FiraCode",
		URL:     "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/FiraCode.zip",
		Version: "3.3.0",
	},
	{
		Name:    "Hack",
		URL:     "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/Hack.zip",
		Version: "3.3.0",
	},
}

// GetFontDir returns the system font directory based on OS
func GetFontDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Fonts
		return filepath.Join(homeDir, "Library", "Fonts"), nil
	case "linux":
		// Linux: ~/.local/share/fonts
		return filepath.Join(homeDir, ".local", "share", "fonts"), nil
	case "windows":
		// Windows: User fonts folder (requires admin for system fonts)
		return filepath.Join(homeDir, "AppData", "Local", "Microsoft", "Windows", "Fonts"), nil
	default:
		return "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// InstallFont downloads and installs a Nerd Font
func InstallFont(font NerdFont, progressFn func(status string)) error {
	// Get font directory
	fontDir, err := GetFontDir()
	if err != nil {
		return err
	}

	// Create font directory if it doesn't exist
	if err := os.MkdirAll(fontDir, 0755); err != nil {
		return fmt.Errorf("failed to create font directory: %w", err)
	}

	progressFn(fmt.Sprintf("Downloading %s Nerd Font v%s...", font.Name, font.Version))

	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "nerd-font-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Download the font zip
	resp, err := http.Get(font.URL)
	if err != nil {
		return fmt.Errorf("failed to download font: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download font: HTTP %d", resp.StatusCode)
	}

	// Copy to temp file
	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save font: %w", err)
	}

	progressFn("Extracting fonts...")

	// Extract font files
	paths, added, err := extractFonts(tmpFile.Name(), fontDir)
	if err != nil {
		return fmt.Errorf("failed to extract fonts: %w", err)
	}

	progressFn(fmt.Sprintf("Installed %d font files to %s (%d already present)", added, fontDir, len(paths)-added))

	// Platform-specific post-install
	switch runtime.GOOS {
	case "linux":
		progressFn("Refreshing font cache...")
		if out, err := exec.Command("fc-cache", "-f", fontDir).CombinedOutput(); err != nil {
			progressFn(fmt.Sprintf("Could not refresh the font cache (%v %s); run: fc-cache -f", err, strings.TrimSpace(string(out))))
		}
	case "windows":
		progressFn("Registering fonts...")
		if err := registerWindowsFonts(paths); err != nil {
			return fmt.Errorf("fonts were copied but not registered: %w", err)
		}
	}

	return nil
}

// registerWindowsFonts adds per-user registry entries for the fonts.
// Windows ignores fonts copied into the per-user font folder without them.
func registerWindowsFonts(paths []string) error {
	const key = `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts`
	for _, path := range paths {
		kind := " (TrueType)"
		if strings.EqualFold(filepath.Ext(path), ".otf") {
			kind = " (OpenType)"
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + kind

		out, err := exec.Command("reg", "add", key, "/v", name, "/t", "REG_SZ", "/d", path, "/f").CombinedOutput()
		if err != nil {
			return fmt.Errorf("reg add %s: %v: %s", name, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// extractFonts extracts .ttf and .otf files from a zip archive. It returns
// the paths of every font from the archive now in destDir, including ones
// already there, and how many were newly added.
func extractFonts(zipPath, destDir string) ([]string, int, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, 0, err
	}
	defer reader.Close()

	var paths []string
	added := 0
	for _, file := range reader.File {
		// Only extract font files (skip Windows Compatible versions to reduce clutter)
		name := strings.ToLower(file.Name)
		if !strings.HasSuffix(name, ".ttf") && !strings.HasSuffix(name, ".otf") {
			continue
		}

		// Skip Windows Compatible fonts (they have "Windows Compatible" in name)
		if strings.Contains(name, "windows") {
			continue
		}

		// Extract just the filename (ignore directory structure in zip)
		destPath := filepath.Join(destDir, filepath.Base(file.Name))

		// Check if file already exists
		if _, err := os.Stat(destPath); err == nil {
			// File exists, skip
			paths = append(paths, destPath)
			continue
		}

		// Open file in zip
		rc, err := file.Open()
		if err != nil {
			return paths, added, err
		}

		// Create destination file
		destFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return paths, added, err
		}

		// Copy content
		_, err = io.Copy(destFile, rc)
		rc.Close()
		destFile.Close()

		if err != nil {
			return paths, added, err
		}

		paths = append(paths, destPath)
		added++
	}

	return paths, added, nil
}

// ListInstalledNerdFonts checks which Nerd Fonts are already installed
func ListInstalledNerdFonts() ([]string, error) {
	fontDir, err := GetFontDir()
	if err != nil {
		return nil, err
	}

	var installed []string

	entries, err := os.ReadDir(fontDir)
	if err != nil {
		if os.IsNotExist(err) {
			return installed, nil
		}
		return nil, err
	}

	// Check for common Nerd Font patterns
	nerdFontPatterns := []string{
		"JetBrainsMono",
		"FiraCode",
		"Hack",
		"CascadiaCode",
		"SourceCodePro",
		"UbuntuMono",
		"RobotoMono",
	}

	seen := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		for _, pattern := range nerdFontPatterns {
			if strings.Contains(name, pattern) && strings.Contains(strings.ToLower(name), "nerd") {
				if !seen[pattern] {
					installed = append(installed, pattern+" Nerd Font")
					seen[pattern] = true
				}
			}
		}
	}

	return installed, nil
}

// FindFont looks up an available font by name, ignoring case
func FindFont(name string) (NerdFont, bool) {
	for _, font := range AvailableFonts {
		if strings.EqualFold(font.Name, name) {
			return font, true
		}
	}
	return NerdFont{}, false
}

// GetDefaultFont returns the recommended font to install
func GetDefaultFont() NerdFont {
	return AvailableFonts[0] // JetBrainsMono
}
