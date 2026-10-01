package jxa

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLiteral(t *testing.T) {
	got, err := Literal([]string{`/tmp/a "b"\c`, "/tmp/line\u2028sep\u2029", "/tmp/<&>", "/tmp/日本"})
	if err != nil {
		t.Fatal(err)
	}
	want := `["/tmp/a \"b\"\\c","/tmp/line\u2028sep\u2029","/tmp/\u003c\u0026\u003e","/tmp/日本"]`
	if got != want {
		t.Fatalf("Literal = %s\nwant      %s", got, want)
	}
	for _, bad := range []any{"bad\xff", []string{"/ok", "/bad\xfe"}} {
		if _, err := Literal(bad); err == nil {
			t.Errorf("Literal(%q) took a string that isn't UTF-8", bad)
		}
	}
}

func TestDecode(t *testing.T) {
	var v struct {
		Count int      `json:"count"`
		Files []string `json:"files"`
	}
	if err := Decode([]byte(`{"count":3,"files":["/tmp/r\u00e9sum\u00e9","/tmp/\ud83c\udf63"]}`+"\n"), &v); err != nil {
		t.Fatal(err)
	}
	if v.Count != 3 || v.Files[0] != "/tmp/résumé" || v.Files[1] != "/tmp/🍣" {
		t.Fatalf("Decode = %+v", v)
	}
	for _, out := range []string{"", "\n", "{", "true"} {
		if err := Decode([]byte(out), &v); err == nil {
			t.Errorf("Decode(%q) succeeded", out)
		}
	}
}

func TestScriptError(t *testing.T) {
	for stderr, want := range map[string]string{
		"execution error: Error: Error: the pasteboard did not take the files (-2700)\n": ": the pasteboard did not take the files",
		"0:12: syntax error: Expected end of line (-2741)":                               ": 0:12: syntax error: Expected end of line",
		"execution error: Error: (not a code) (x)":                                       ": (not a code) (x)",
		"": "",
	} {
		if got := scriptError(stderr); got != want {
			t.Errorf("scriptError(%q) = %q, want %q", stderr, got, want)
		}
	}
}

// TestOsascript runs a script that touches nothing but itself, where
// osascript is there
func TestOsascript(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("no osascript")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := Osascript(ctx, Prelude+`function run() { return result({s: "résumé 🍣", n: 1 + 1}); }`)
	if err != nil {
		t.Skipf("osascript can't run here: %v", err)
	}
	if strings.ContainsFunc(string(out), func(r rune) bool { return r > 0x7e }) {
		t.Errorf("output isn't plain ASCII: %q", out)
	}
	var v struct {
		S string `json:"s"`
		N int    `json:"n"`
	}
	if err := Decode(out, &v); err != nil || v.S != "résumé 🍣" || v.N != 2 {
		t.Fatalf("Decode(%q) = %+v, %v", out, v, err)
	}

	_, err = Osascript(ctx, `function run() { throw new Error("nope"); }`)
	if err == nil || !strings.HasSuffix(err.Error(), ": nope") {
		t.Fatalf("err = %v", err)
	}
}
