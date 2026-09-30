package ui_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
)

func TestBuiltInThemesAreComplete(t *testing.T) {
	for _, name := range ui.ThemeNames() {
		theme, warnings := ui.LoadTheme(name, nil)
		if len(warnings) > 0 {
			t.Errorf("%s: warnings %v", name, warnings)
		}
		v := reflect.ValueOf(theme)
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).String() == "" {
				t.Errorf("%s: %s is not set", name, v.Type().Field(i).Name)
			}
		}
		if !components.HasSyntaxTheme(theme.Syntax) {
			t.Errorf("%s: unknown syntax style %q", name, theme.Syntax)
		}
	}
}

func TestClassicThemeKeepsOriginalColors(t *testing.T) {
	theme, warnings := ui.LoadTheme("classic", nil)
	if len(warnings) > 0 || theme.Accent != "62" || theme.CursorBg != "13" || theme.Syntax != "monokai" {
		t.Fatalf("classic theme changed: %+v (%v)", theme, warnings)
	}
	if theme, _ := ui.LoadTheme("", nil); theme.Name != "default" || theme.Syntax != "sushi" {
		t.Fatalf("no name should mean the default theme, got %+v", theme)
	}
}

func TestLoadThemeOverridesAndWarnings(t *testing.T) {
	theme, warnings := ui.LoadTheme("light", map[string]string{
		"accent":    "#ff0000",
		"directory": "33",
		"danger":    "not-a-color",
		"sparkles":  "1",
	})
	if theme.Accent != lipgloss.Color("#ff0000") || theme.Directory != lipgloss.Color("33") {
		t.Fatalf("overrides not applied: accent=%s directory=%s", theme.Accent, theme.Directory)
	}
	if theme.Danger == "not-a-color" {
		t.Fatal("invalid color was applied")
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "not-a-color") || !strings.Contains(joined, "sparkles") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestUnknownThemeFallsBack(t *testing.T) {
	theme, warnings := ui.LoadTheme("neon", nil)
	if theme.Name != "default" || len(warnings) != 1 {
		t.Fatalf("theme=%s warnings=%v", theme.Name, warnings)
	}
}
