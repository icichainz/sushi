package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryScoreIsFrecency(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	at := func(ago time.Duration) DirVisit { return DirVisit{Path: "/x", Count: 8, Last: now.Add(-ago).Unix()} }
	for _, c := range []struct {
		ago  time.Duration
		want float64
	}{{time.Minute, 32}, {3 * time.Hour, 16}, {3 * 24 * time.Hour, 4}, {30 * 24 * time.Hour, 2}} {
		if got := at(c.ago).Score(now); got != c.want {
			t.Errorf("8 visits, the latest %v ago: score %v, want %v", c.ago, got, c.want)
		}
	}
	// Often but long ago loses to a few visits today
	old := DirVisit{Count: 20, Last: now.Add(-60 * 24 * time.Hour).Unix()}
	recent := DirVisit{Count: 3, Last: now.Add(-time.Minute).Unix()}
	if old.Score(now) >= recent.Score(now) {
		t.Errorf("old %v should rank below recent %v", old.Score(now), recent.Score(now))
	}
}

func TestSaveDirHistoryAddsUpVisits(t *testing.T) {
	dir := useTempHome(t)
	now := time.Now()
	if got := LoadDirHistory(); len(got) != 0 {
		t.Fatalf("no file: %v", got)
	}

	// Two sushis save their own visits: the counts add up, and the latest
	// visit's time is kept
	if err := SaveDirHistory([]DirVisit{{Path: "/a", Count: 2, Last: now.Unix() - 100}, {Path: "/b", Count: 1, Last: now.Unix()}}, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := SaveDirHistory([]DirVisit{{Path: "/a", Count: 3, Last: now.Unix() - 50}}, nil, now); err != nil {
		t.Fatal(err)
	}
	got := LoadDirHistory()
	if len(got) != 2 || got[0].Path != "/a" || got[0].Count != 5 || got[0].Last != now.Unix()-50 || got[1].Path != "/b" {
		t.Fatalf("history = %+v", got)
	}

	// Gone folders are dropped
	if err := SaveDirHistory(nil, []string{"/a"}, now); err != nil {
		t.Fatal(err)
	}
	if got := LoadDirHistory(); len(got) != 1 || got[0].Path != "/b" {
		t.Fatalf("after dropping /a: %+v", got)
	}

	// Only its owner can read it, and no temporary file is left behind
	info, err := os.Stat(filepath.Join(dir, "history.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("history.json: %v, %v", info, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("config dir holds %d entries, want history.json alone", len(entries))
	}
}

func TestDirHistoryIsBounded(t *testing.T) {
	useTempHome(t)
	now := time.Now()
	var visits []DirVisit
	for i := range HistoryLimit + 20 {
		// The first folders are the least visited
		visits = append(visits, DirVisit{Path: fmt.Sprintf("/d%03d", i), Count: float64(i + 1), Last: now.Unix()})
	}
	if err := SaveDirHistory(visits, nil, now); err != nil {
		t.Fatal(err)
	}
	got := LoadDirHistory()
	if len(got) != HistoryLimit {
		t.Fatalf("%d folders kept, want %d", len(got), HistoryLimit)
	}
	for _, v := range got {
		if v.Path < "/d020" {
			t.Fatalf("%s, among the least frecent, was kept", v.Path)
		}
	}

	// Past historyMaxCount visits in all, every count shrinks, and
	// folders left with less than one visit are forgotten
	if err := SaveDirHistory([]DirVisit{{Path: "/hot", Count: historyMaxCount, Last: now.Unix()}}, nil, now); err != nil {
		t.Fatal(err)
	}
	total := 0.0
	got = LoadDirHistory()
	for _, v := range got {
		total += v.Count
	}
	if total > historyMaxCount || got[0].Path != "/hot" {
		t.Fatalf("after aging: %d folders, %v visits in all, first %s", len(got), total, got[0].Path)
	}
}

func TestBrokenHistoryFileIsEmpty(t *testing.T) {
	dir := useTempHome(t)
	path := filepath.Join(dir, "history.json")
	for _, text := range []string{"not json", `{"dirs":[{"path":"relative","count":3},{"path":"/ok","count":1},{"path":"/ok/","count":2},{"path":"/zero","count":0}]}`} {
		os.WriteFile(path, []byte(text), 0600)
		got := LoadDirHistory()
		if text == "not json" && len(got) != 0 {
			t.Errorf("%q: %+v", text, got)
		}
		if text != "not json" && (len(got) != 1 || got[0].Path != "/ok" || got[0].Count != 3) {
			t.Errorf("%q: %+v, want /ok alone, its entries merged", text, got)
		}
	}
}

func TestDualPaneAndHistorySettings(t *testing.T) {
	dir := useTempHome(t)
	if cfg := DefaultConfig(); cfg.DualPane || !cfg.History {
		t.Fatalf("defaults: dual_pane=%v history=%v", cfg.DualPane, cfg.History)
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("dual_pane: true\nhistory: false\n"), 0644)
	if cfg := LoadConfig(); !cfg.DualPane || cfg.History || len(cfg.Problems) > 0 {
		t.Fatalf("dual_pane=%v history=%v problems=%q", cfg.DualPane, cfg.History, cfg.Problems)
	}
}
