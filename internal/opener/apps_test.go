package opener

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/icichainz/sushi/internal/jxa"
)

// fakeRunner answers every script with reply and records them
func fakeRunner(reply string, err error, scripts *[]string) jxa.Runner {
	return func(_ context.Context, script string) ([]byte, error) {
		*scripts = append(*scripts, script)
		return []byte(reply), err
	}
}

func TestAppsPutsTheDefaultFirst(t *testing.T) {
	var scripts []string
	reply := `{"default":"/Applications/Xcode.app","apps":["/System/Applications/TextEdit.app","/Applications/Xcode.app",` +
		`"/Applications/Visual Studio Code.app","/Users/me/Downloads/TextEdit.app"]}`
	apps, err := Apps(context.Background(), fakeRunner(reply, nil, &scripts), "/tmp/notes & \"todo\".md")
	if err != nil {
		t.Fatal(err)
	}
	want := []App{
		{Name: "Xcode", Path: "/Applications/Xcode.app", Default: true},
		{Name: "TextEdit", Path: "/System/Applications/TextEdit.app"},
		{Name: "Visual Studio Code", Path: "/Applications/Visual Studio Code.app"},
		{Name: "TextEdit", Path: "/Users/me/Downloads/TextEdit.app"},
	}
	if !slices.Equal(apps, want) {
		t.Fatalf("Apps = %+v\nwant %+v", apps, want)
	}
	for _, s := range []string{`$.NSURL.fileURLWithPath("/tmp/notes \u0026 \"todo\".md")`, "URLForApplicationToOpenURL(url)", "URLsForApplicationsToOpenURL(url)"} {
		if !strings.Contains(scripts[0], s) {
			t.Errorf("script lacks %q:\n%s", s, scripts[0])
		}
	}
}

func TestAppsWithoutADefault(t *testing.T) {
	var scripts []string
	apps, err := Apps(context.Background(), fakeRunner(`{"default":"","apps":[]}`, nil, &scripts), "/tmp/x.qqq")
	if err != nil || len(apps) != 0 {
		t.Fatalf("Apps = %+v, %v", apps, err)
	}
	apps, _ = Apps(context.Background(), fakeRunner(`{"default":"","apps":["/Applications/A.app"]}`, nil, &scripts), "/tmp/x.qqq")
	if len(apps) != 1 || apps[0].Default || apps[0].Name != "A" {
		t.Fatalf("Apps = %+v", apps)
	}
}

func TestAppsFailures(t *testing.T) {
	var scripts []string
	if _, err := Apps(context.Background(), fakeRunner("", errors.New("osascript: no"), &scripts), "/tmp/a"); err == nil {
		t.Error("a failed lookup succeeded")
	}
	if _, err := Apps(context.Background(), fakeRunner("garbage", nil, &scripts), "/tmp/a"); err == nil {
		t.Error("garbage was read as apps")
	}
	n := len(scripts)
	for _, bad := range []string{"relative.txt", "/tmp/bad\xff"} {
		if _, err := Apps(context.Background(), fakeRunner("{}", nil, &scripts), bad); err == nil {
			t.Errorf("Apps(%q) succeeded", bad)
		}
	}
	if len(scripts) != n {
		t.Error("osascript ran for a path it can't take")
	}
}

func TestOpenWithAndRevealArgs(t *testing.T) {
	if args := OpenWithArgs("/Applications/Visual Studio Code.app", "/tmp/a b", "/tmp/-c"); !slices.Equal(args,
		[]string{"-a", "/Applications/Visual Studio Code.app", "/tmp/a b", "/tmp/-c"}) {
		t.Errorf("OpenWithArgs = %q", args)
	}
	if args := RevealArgs("/tmp/a & b"); !slices.Equal(args, []string{"-R", "/tmp/a & b"}) {
		t.Errorf("RevealArgs = %q", args)
	}
}

func TestRevealScript(t *testing.T) {
	var scripts []string
	paths := []string{"/tmp/a.txt", "/tmp/b \"c\".txt"}
	if err := Reveal(context.Background(), fakeRunner("{}", nil, &scripts), paths); err != nil {
		t.Fatal(err)
	}
	lit, _ := jxa.Literal(paths)
	for _, s := range []string{"const paths = " + lit + ";", "activateFileViewerSelectingURLs(urls)"} {
		if !strings.Contains(scripts[0], s) {
			t.Errorf("script lacks %q:\n%s", s, scripts[0])
		}
	}
	if err := Reveal(context.Background(), fakeRunner("", errors.New("osascript: no"), &scripts), paths); err == nil {
		t.Error("a failed reveal succeeded")
	}
	for _, bad := range [][]string{nil, {"rel"}} {
		if err := Reveal(context.Background(), fakeRunner("{}", nil, &scripts), bad); err == nil {
			t.Errorf("Reveal(%q) succeeded", bad)
		}
	}
}

// TestAppsForARealFile asks Launch Services which apps open a text file,
// where osascript is there. It only reads: no app is opened.
func TestAppsForARealFile(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("no osascript")
	}
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	apps, err := Apps(ctx, jxa.Osascript, file)
	if err != nil {
		t.Skipf("osascript can't ask Launch Services here: %v", err)
	}
	if len(apps) == 0 || !strings.HasSuffix(apps[0].Path, ".app") || apps[0].Name == "" {
		t.Fatalf("apps for a text file: %+v", apps)
	}
	for _, app := range apps[1:] {
		if app.Default {
			t.Fatalf("a default app after the first: %+v", apps)
		}
	}
}
