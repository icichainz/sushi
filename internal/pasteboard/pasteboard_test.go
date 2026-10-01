package pasteboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/icichainz/sushi/internal/jxa"
	"golang.org/x/text/unicode/norm"
)

// fake records the scripts it is given and answers with reply
type fake struct {
	scripts []string
	reply   string
	err     error
}

func (f *fake) run(_ context.Context, script string) ([]byte, error) {
	f.scripts = append(f.scripts, script)
	return []byte(f.reply), f.err
}

func TestWriteBuildsTheScriptAndReadsTheCount(t *testing.T) {
	f := &fake{reply: `{"count":42}` + "\n"}
	b := New(f.run, "")
	paths := []string{"/tmp/a b.txt", `/tmp/quote"back\slash`, "/tmp/line\u2028sep", "/tmp/résumé 日本.txt"}
	count, err := b.Write(context.Background(), paths)
	if err != nil || count != 42 {
		t.Fatalf("Write = %d, %v", count, err)
	}
	script := f.scripts[0]
	list, _ := jxa.Literal(paths)
	for _, want := range []string{"ObjC.import('AppKit')", "const paths = " + list + ";", "$.NSPasteboard.generalPasteboard",
		"pb.clearContents", "pb.writeObjects(urls)", "$.NSURL.fileURLWithPath(p)", "result({count: Number(pb.changeCount)})"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
	// The literal is plain JSON, with the line separator escaped
	if !strings.Contains(list, `\"back\\slash`) || !strings.Contains(list, `\u2028`) || strings.Contains(list, "\u2028") {
		t.Errorf("literal = %s", list)
	}
}

func TestNamedBoard(t *testing.T) {
	f := &fake{reply: `{"count":1,"files":[]}`}
	New(f.run, "sushi-test").Read(context.Background(), -1)
	if !strings.Contains(f.scripts[0], `$.NSPasteboard.pasteboardWithName("sushi-test")`) || strings.Contains(f.scripts[0], "generalPasteboard") {
		t.Fatalf("script:\n%s", f.scripts[0])
	}
}

func TestWriteRefusesWhatItCantPutThere(t *testing.T) {
	f := &fake{reply: `{"count":1}`}
	b := New(f.run, "")
	for _, paths := range [][]string{nil, {"relative.txt"}, {"/tmp/ok", "/tmp/bad\xff"}} {
		if _, err := b.Write(context.Background(), paths); err == nil {
			t.Errorf("Write(%q) succeeded", paths)
		}
	}
	if len(f.scripts) != 0 {
		t.Fatalf("osascript ran for paths it can't take: %d scripts", len(f.scripts))
	}
}

func TestWriteReportsFailures(t *testing.T) {
	for _, f := range []*fake{
		{err: errors.New("osascript: exit status 1: the pasteboard did not take the files")},
		{reply: ""},
		{reply: "not json"},
		{reply: `{}`},
	} {
		if _, err := New(f.run, "").Write(context.Background(), []string{"/tmp/a"}); err == nil {
			t.Errorf("reply %q, error %v: no error", f.reply, f.err)
		}
	}
}

func TestReadParsesFiles(t *testing.T) {
	// As result writes it: everything outside ASCII escaped, an emoji as
	// a surrogate pair
	f := &fake{reply: `{"count":7,"files":["/Users/me/r\u00e9sum\u00e9.pdf","/tmp/\ud83c\udf63 sushi","/tmp/a\"b"]}` + "\n"}
	c, err := New(f.run, "").Read(context.Background(), -1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/Users/me/résumé.pdf", "/tmp/🍣 sushi", `/tmp/a"b`}; c.Count != 7 || !slices.Equal(c.Files, want) {
		t.Fatalf("Read = %+v, want count 7 and %q", c, want)
	}
	for _, want := range []string{"readObjectsForClassesOptions", "NSPasteboardURLReadingFileURLsOnlyKey", "filePathURL", "generalPasteboard",
		"if (count === -1 || !types.includes('public.file-url'))"} {
		if !strings.Contains(f.scripts[0], want) {
			t.Errorf("script lacks %q:\n%s", want, f.scripts[0])
		}
	}

	f = &fake{reply: `{"count":8,"files":[]}`}
	if c, err := New(f.run, "").Read(context.Background(), 41); err != nil || c.Count != 8 || len(c.Files) != 0 {
		t.Fatalf("text on the pasteboard: %+v, %v", c, err)
	}
	// With the count known, the files are left unread
	if !strings.Contains(f.scripts[0], "if (count === 41 ||") {
		t.Errorf("script:\n%s", f.scripts[0])
	}
	f = &fake{err: errors.New("osascript: not found")}
	if _, err := New(f.run, "").Read(context.Background(), -1); err == nil {
		t.Fatal("a failed read succeeded")
	}
}

func TestCountReadsOnlyTheCount(t *testing.T) {
	f := &fake{reply: `{"count":12}` + "\n"}
	if n, err := New(f.run, "").Count(context.Background()); err != nil || n != 12 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	if script := f.scripts[0]; !strings.Contains(script, "$.NSPasteboard.generalPasteboard.changeCount") || strings.Contains(script, "readObjects") {
		t.Fatalf("script:\n%s", script)
	}
	for _, f := range []*fake{{err: errors.New("osascript: exit status 1")}, {reply: `{}`}, {reply: "nope"}} {
		if _, err := New(f.run, "").Count(context.Background()); err == nil {
			t.Errorf("reply %q, error %v: no error", f.reply, f.err)
		}
	}
}

// TestRealPasteboard puts a file on a pasteboard with osascript and reads
// it back. It runs only with SUSHI_PASTEBOARD_TEST=1, as it talks to the
// pasteboard server of the logged-in user, and skips where osascript is
// missing or can't reach it (over ssh, or on CI). It uses a pasteboard of
// its own name, released afterwards, so what the user copied is left as it
// was; the general pasteboard differs only in how the script names it.
func TestRealPasteboard(t *testing.T) {
	if os.Getenv("SUSHI_PASTEBOARD_TEST") != "1" {
		t.Skip("set SUSHI_PASTEBOARD_TEST=1 to talk to the real pasteboard")
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("no osascript")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "pasted résumé 日本.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("sushi-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b := New(jxa.Osascript, name)
	t.Cleanup(func() {
		jxa.Osascript(context.Background(), jxa.Prelude+b.board()+".releaseGlobally;")
	})

	count, err := b.Write(ctx, []string{file})
	if err != nil {
		t.Skipf("osascript can't reach the pasteboard: %v", err)
	}
	c, err := b.Read(ctx, -1)
	if err != nil {
		t.Fatal(err)
	}
	// AppKit hands back names decomposed, as in e and a combining accent
	if c.Count != count || len(c.Files) != 1 || norm.NFC.String(c.Files[0]) != norm.NFC.String(file) {
		t.Fatalf("read %+v after writing %s at count %d", c, file, count)
	}
	if c, err := b.Read(ctx, count); err != nil || c.Count != count || len(c.Files) != 0 {
		t.Fatalf("with the count known: %+v, %v", c, err)
	}
}
