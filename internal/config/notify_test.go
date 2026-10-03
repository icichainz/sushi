package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestNotifyAfterSetting(t *testing.T) {
	if got := time.Duration(DefaultConfig().NotifyAfter); got != 5*time.Second {
		t.Fatalf("default notify_after = %v, want 5s", got)
	}

	dir := useTempHome(t)
	write := func(yaml string) *Config {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0644); err != nil {
			t.Fatal(err)
		}
		return LoadConfig()
	}

	if cfg := write("sort_by: size\n"); time.Duration(cfg.NotifyAfter) != 5*time.Second || len(cfg.Problems) > 0 {
		t.Fatalf("without notify_after: %v (problems %q)", time.Duration(cfg.NotifyAfter), cfg.Problems)
	}
	for value, want := range map[string]time.Duration{
		"30s":     30 * time.Second,
		"1m30s":   90 * time.Second,
		"500ms":   500 * time.Millisecond,
		"0":       0,
		"0s":      0,
		`"10s"`:   10 * time.Second,
		"2h":      2 * time.Hour,
		"1.5s":    1500 * time.Millisecond,
		"  45s  ": 45 * time.Second,
	} {
		if cfg := write("notify_after: " + value + "\n"); time.Duration(cfg.NotifyAfter) != want || len(cfg.Problems) > 0 {
			t.Errorf("notify_after: %s gave %v (problems %q), want %v", value, time.Duration(cfg.NotifyAfter), cfg.Problems, want)
		}
	}

	// What isn't a duration, a negative one, and a number without a unit
	// keep the default and are reported by their line; the rest still applies
	for _, value := range []string{"soon", "-5s", "10", "[5s]", "true"} {
		cfg := write("show_hidden: true\nnotify_after: " + value + "\n")
		if time.Duration(cfg.NotifyAfter) != 5*time.Second || !cfg.ShowHidden {
			t.Errorf("notify_after: %s gave %v, show_hidden %v; want the default, and the rest read", value, time.Duration(cfg.NotifyAfter), cfg.ShowHidden)
		}
		if len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0], "config.yaml: line 2: want a duration") {
			t.Errorf("notify_after: %s: problems %q", value, cfg.Problems)
		}
	}
}

func TestNotifyAfterIsSavedAsADuration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NotifyAfter = Duration(90 * time.Second)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "notify_after: 1m30s\n") {
		t.Fatalf("saved as:\n%s", data)
	}

	// And reads back the same, as --init-config writes it
	useTempHome(t)
	if created, err := CreateDefaultConfigFile(); err != nil || !created {
		t.Fatalf("CreateDefaultConfigFile: %v, %v", created, err)
	}
	if loaded := LoadConfig(); loaded.NotifyAfter != DefaultConfig().NotifyAfter || len(loaded.Problems) > 0 {
		t.Fatalf("read back %v (problems %q)", time.Duration(loaded.NotifyAfter), loaded.Problems)
	}
}
