package tags

import (
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
)

// finderPlist is what plutil -convert binary1 makes of an array holding
// "Red\n6", "Work" and "Café ✓": ASCII strings, then a UTF-16 one
const finderPlist = "62706c6973743030a3010203555265640a3654576f726b6600430061006600e900202713" +
	"080c12170000000000000101000000000000000400000000000000000000000000000024"

func TestDecodeFinderPlist(t *testing.T) {
	data, err := hex.DecodeString(finderPlist)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []Tag{{"Red", Red}, {"Work", None}, {"Café ✓", None}}
	if !slices.Equal(got, want) {
		t.Fatalf("Decode = %q, want %q", got, want)
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	long := strings.Repeat("long name ", 20) // Past the count that fits in a marker
	cases := [][]Tag{
		{},
		{{"Red", Red}},
		{{"Work", None}, {"Orange", Orange}, {"Gray", Gray}},
		{{"Café", Green}, {"日本語", Blue}, {"emoji 🍣", Purple}}, // UTF-16, with a surrogate pair
		{{long, Yellow}, {"a", None}},
	}
	// Enough tags for references of two bytes
	var many []Tag
	for i := range 300 {
		many = append(many, Tag{Name: strings.Repeat("x", i%40+1), Color: Color(i % 8)})
	}
	cases = append(cases, many)

	for _, list := range cases {
		got, err := Decode(Encode(list))
		if err != nil {
			t.Fatalf("%d tags: %v", len(list), err)
		}
		if !Equal(got, list) && !(len(got) == 0 && len(list) == 0) {
			t.Fatalf("round trip of %q gave %q", list, got)
		}
	}
}

func TestEncodeMatchesPlutil(t *testing.T) {
	// The same bytes plutil writes for the same array
	want, _ := hex.DecodeString(finderPlist)
	got := Encode([]Tag{{"Red", Red}, {"Work", None}, {"Café ✓", None}})
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("Encode =\n%x\nwant\n%x", got, want)
	}
}

func TestDecodeEmptyArray(t *testing.T) {
	got, err := Decode(Encode(nil))
	if err != nil || len(got) != 0 {
		t.Fatalf("Decode(empty) = %q, %v", got, err)
	}
}

func TestDecodeXMLPlist(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
	<string>Green
2</string>
	<string>R&amp;D</string>
</array>
</plist>`)
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Tag{{"Green", Green}, {"R&D", None}}; !slices.Equal(got, want) {
		t.Fatalf("Decode = %q, want %q", got, want)
	}
}

func TestDecodeRejectsDamage(t *testing.T) {
	good := Encode([]Tag{{"Red", Red}, {"Work", None}})
	for name, data := range map[string][]byte{
		"empty":       nil,
		"other":       []byte("hello"),
		"short":       good[:20],
		"no trailer":  good[:len(good)-1],
		"xml no list": []byte("<plist><dict/></plist>"),
		"not strings": []byte("<plist><array><integer>1</integer></array></plist>"),
	} {
		if _, err := Decode(data); !errors.Is(err, errPlist) {
			t.Errorf("%s: err = %v, want errPlist", name, err)
		}
	}

	// Every corruption of a byte is refused or read, never a panic
	for i := range good {
		for _, b := range []byte{0x00, 0xFF, 0x0F, 0xAF, 0x5F, 0x6F, 0x13} {
			bad := slices.Clone(good)
			bad[i] = b
			Decode(bad)
		}
	}
}

func TestParseTag(t *testing.T) {
	for in, want := range map[string]Tag{
		"Red\n6":      {"Red", Red},
		"Work":        {"Work", None},
		"Work\n0":     {"Work", None},
		"Two\nlines":  {"Two\nlines", None},
		"Odd\n9":      {"Odd\n9", None},
		"Orange\n7":   {"Orange", Orange},
		"a\nb\n1":     {"a\nb", Gray},
		"Projet été":  {"Projet été", None},
		"trailing\n":  {"trailing\n", None},
		"Purple\n3\n": {"Purple\n3\n", None},
	} {
		if got := parse(in); got != want {
			t.Errorf("parse(%q) = %q, want %q", in, got, want)
		}
	}
	if s := (Tag{"Red", Red}).String(); s != "Red\n6" {
		t.Errorf("String = %q", s)
	}
	if s := (Tag{"Work", None}).String(); s != "Work" {
		t.Errorf("String = %q", s)
	}
}

func TestListHelpers(t *testing.T) {
	list := []Tag{{"Red", Red}, {"Work", None}}
	if !Has(list, "work") || Has(list, "Blue") {
		t.Fatal("Has ignores case and finds only what is there")
	}
	added := With(list, Tag{"Blue", Blue})
	if len(list) != 2 || len(added) != 3 || added[2].Name != "Blue" {
		t.Fatalf("With = %q, and list is now %q", added, list)
	}
	if again := With(added, Tag{"BLUE", None}); len(again) != 3 {
		t.Fatalf("With added a tag already there: %q", again)
	}
	if out := Without(added, "red"); !slices.Equal(out, []Tag{{"Work", None}, {"Blue", Blue}}) {
		t.Fatalf("Without = %q", out)
	}

	for _, c := range []struct {
		prefix string
		score  int
		ok     bool
	}{{"red", 0, true}, {"Re", 1, true}, {"", 0, true}, {"Blue", 0, false}, {"Workshop", 0, false}} {
		score, ok := Matches(list, c.prefix)
		if ok != c.ok || ok && score != c.score {
			t.Errorf("Matches(%q) = %d, %v; want %d, %v", c.prefix, score, ok, c.score, c.ok)
		}
	}
	if _, ok := Matches(nil, ""); ok {
		t.Error("no tags matched an empty prefix")
	}
}

func TestKnownHasEveryColour(t *testing.T) {
	seen := map[Color]bool{}
	for _, k := range Known() {
		if k.Name != k.Color.String() {
			t.Errorf("%q is called after the wrong colour %s", k.Name, k.Color)
		}
		seen[k.Color] = true
	}
	if len(seen) != 7 || seen[None] {
		t.Fatalf("Known covers %v", seen)
	}
}

func FuzzDecode(f *testing.F) {
	seed, _ := hex.DecodeString(finderPlist)
	f.Add(seed)
	f.Add(Encode(nil))
	f.Add(Encode([]Tag{{strings.Repeat("y", 300), Red}}))
	f.Add([]byte("<plist><array><string>a</string></array></plist>"))
	f.Fuzz(func(t *testing.T, data []byte) {
		list, err := Decode(data)
		if err != nil {
			return
		}
		// Whatever reads back writes out the same
		again, err := Decode(Encode(list))
		if err != nil || !Equal(again, list) && len(list) > 0 {
			t.Fatalf("%q read back as %q (%v)", list, again, err)
		}
	})
}
