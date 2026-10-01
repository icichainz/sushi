package tags

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"unicode/utf16"
)

// Finder keeps a file's tags as a property list holding an array of
// strings, almost always in Apple's binary format (bplist00). Only that
// much of the format is handled here, which saves a dependency: the
// header, the trailer and offset table, and array and string objects.
// XML property lists, which some tools write instead, are read too.

const bplistMagic = "bplist00"

// trailerSize is the size of the trailer that ends a binary plist
const trailerSize = 32

// Object markers: the high nibble of an object's first byte
const (
	markerInt    = 0x1
	markerASCII  = 0x5
	markerUTF16  = 0x6
	markerArray  = 0xA
	countInFull  = 0xF // Low nibble: the count follows as an int object
	maxNibbleLen = 14  // Longest count kept in the low nibble
)

var errPlist = errors.New("not a list of tags")

// rawString is a string of a property list as read, with the object that
// holds it in a binary one, to write back exactly as it was: text that
// isn't quite Unicode, as an unpaired surrogate, reads as something else
type rawString struct {
	s   string
	obj []byte // nil for an XML property list
}

// decodeStrings reads a property list holding an array of strings
func decodeStrings(data []byte) ([]string, error) {
	raw, err := decodeRaw(data)
	if err != nil {
		return nil, err
	}
	strs := make([]string, len(raw))
	for i, r := range raw {
		strs[i] = r.s
	}
	return strs, nil
}

// decodeRaw reads a property list holding an array of strings, keeping
// each string's object
func decodeRaw(data []byte) ([]rawString, error) {
	if bytes.HasPrefix(data, []byte(bplistMagic)) {
		return decodeBinary(data)
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("<?xml")) || bytes.HasPrefix(trimmed, []byte("<plist")) {
		strs, err := decodeXML(trimmed)
		raw := make([]rawString, len(strs))
		for i, s := range strs {
			raw[i] = rawString{s: s}
		}
		return raw, err
	}
	return nil, fmt.Errorf("%w: unknown property list format", errPlist)
}

// bplist is a binary property list being read
type bplist struct {
	data       []byte
	offsets    []uint64 // Where each object starts
	refSize    int      // Bytes in a reference to an object
	objectsEnd uint64   // Objects lie between the header and here
}

// decodeBinary reads a binary property list whose top object is an array
// of strings
func decodeBinary(data []byte) ([]rawString, error) {
	if len(data) < len(bplistMagic)+trailerSize {
		return nil, fmt.Errorf("%w: too short", errPlist)
	}
	trailer := data[len(data)-trailerSize:]
	offsetSize := int(trailer[6])
	refSize := int(trailer[7])
	count := binary.BigEndian.Uint64(trailer[8:16])
	top := binary.BigEndian.Uint64(trailer[16:24])
	tableAt := binary.BigEndian.Uint64(trailer[24:32])

	tableEnd := uint64(len(data) - trailerSize)
	if !validSize(offsetSize) || !validSize(refSize) || tableAt < uint64(len(bplistMagic)) || tableAt > tableEnd ||
		count == 0 || top >= count || count > (tableEnd-tableAt)/uint64(offsetSize) {
		return nil, fmt.Errorf("%w: bad trailer", errPlist)
	}

	p := bplist{data: data, refSize: refSize, objectsEnd: tableAt}
	p.offsets = make([]uint64, count)
	for i := range p.offsets {
		at := tableAt + uint64(i*offsetSize)
		p.offsets[i] = readUint(data[at : at+uint64(offsetSize)])
	}

	marker, length, body, err := p.object(top)
	if err != nil {
		return nil, err
	}
	if marker != markerArray {
		return nil, fmt.Errorf("%w: not an array", errPlist)
	}
	if uint64(length) > uint64(len(body))/uint64(refSize) {
		return nil, fmt.Errorf("%w: array runs past the end", errPlist)
	}
	out := make([]rawString, 0, length)
	for i := range length {
		ref := readUint(body[i*refSize : (i+1)*refSize])
		s, err := p.str(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// validSize reports whether n is a size the format uses for integers
func validSize(n int) bool {
	return n == 1 || n == 2 || n == 4 || n == 8
}

// readUint reads a big-endian unsigned integer of 1 to 8 bytes
func readUint(b []byte) uint64 {
	var n uint64
	for _, c := range b {
		n = n<<8 | uint64(c)
	}
	return n
}

// object returns object ref's kind, its count (elements of an array,
// characters of a string) and the bytes from its contents to the end of
// the objects
func (p bplist) object(ref uint64) (marker byte, length int, body []byte, err error) {
	if ref >= uint64(len(p.offsets)) {
		return 0, 0, nil, fmt.Errorf("%w: reference out of range", errPlist)
	}
	at := p.offsets[ref]
	if at < uint64(len(bplistMagic)) || at >= p.objectsEnd {
		return 0, 0, nil, fmt.Errorf("%w: object out of range", errPlist)
	}
	body = p.data[at:p.objectsEnd]
	marker, n := body[0]>>4, uint64(body[0]&0xF)
	body = body[1:]
	if n == countInFull && (marker == markerASCII || marker == markerUTF16 || marker == markerArray) {
		// The count is an int object of 1, 2, 4 or 8 bytes
		if len(body) == 0 || body[0]>>4 != markerInt {
			return 0, 0, nil, fmt.Errorf("%w: bad count", errPlist)
		}
		size := 1 << (body[0] & 0xF)
		if size > 8 || len(body) < 1+size {
			return 0, 0, nil, fmt.Errorf("%w: bad count", errPlist)
		}
		n = readUint(body[1 : 1+size])
		body = body[1+size:]
	}
	// No count can exceed the bytes there are, which also keeps a corrupt
	// one from asking for a huge allocation
	if n > uint64(len(body)) {
		return 0, 0, nil, fmt.Errorf("%w: object runs past the end", errPlist)
	}
	return marker, int(n), body, nil
}

// str reads string object ref
func (p bplist) str(ref uint64) (rawString, error) {
	marker, length, body, err := p.object(ref)
	if err != nil {
		return rawString{}, err
	}
	// The object runs from its marker to the end of its text
	obj := func(size int) []byte {
		return p.data[p.offsets[ref] : p.objectsEnd-uint64(len(body)-size)]
	}
	switch marker {
	case markerASCII:
		// Bytes past ASCII shouldn't be there; read as Latin-1 they at
		// least make valid text
		var b strings.Builder
		for _, c := range body[:length] {
			b.WriteRune(rune(c))
		}
		return rawString{b.String(), obj(length)}, nil
	case markerUTF16:
		if 2*length > len(body) {
			return rawString{}, fmt.Errorf("%w: string runs past the end", errPlist)
		}
		units := make([]uint16, length)
		for i := range units {
			units[i] = binary.BigEndian.Uint16(body[2*i:])
		}
		return rawString{string(utf16.Decode(units)), obj(2 * length)}, nil
	}
	return rawString{}, fmt.Errorf("%w: holds something other than text", errPlist)
}

// encodeStrings writes strs as a binary property list: an array, then
// each string, ASCII where it can be and UTF-16 otherwise, as Finder does
func encodeStrings(strs []string) []byte {
	objs := make([][]byte, len(strs))
	for i, s := range strs {
		objs[i] = stringObject(s)
	}
	return encodeObjects(objs)
}

// stringObject returns the object for s: ASCII where it can be, and
// UTF-16 otherwise
func stringObject(s string) []byte {
	var b bytes.Buffer
	if isASCII(s) {
		writeMarker(&b, markerASCII, len(s))
		b.WriteString(s)
		return b.Bytes()
	}
	units := utf16.Encode([]rune(s))
	writeMarker(&b, markerUTF16, len(units))
	for _, u := range units {
		b.Write([]byte{byte(u >> 8), byte(u)})
	}
	return b.Bytes()
}

// encodeObjects writes a binary property list of an array holding objs,
// string objects as stringObject makes them or as they were read
func encodeObjects(objs [][]byte) []byte {
	count := len(objs) + 1 // The array and its strings
	refSize := 1
	if count > 0xFF {
		refSize = 2
	}
	if count > 0xFFFF {
		refSize = 4
	}

	var b bytes.Buffer
	offsets := make([]uint64, 0, count)
	b.WriteString(bplistMagic)

	offsets = append(offsets, uint64(b.Len()))
	writeMarker(&b, markerArray, len(objs))
	for i := range objs {
		writeUint(&b, uint64(i+1), refSize)
	}
	for _, obj := range objs {
		offsets = append(offsets, uint64(b.Len()))
		b.Write(obj)
	}

	tableAt := uint64(b.Len())
	offsetSize := uintSize(tableAt)
	for _, off := range offsets {
		writeUint(&b, off, offsetSize)
	}
	trailer := make([]byte, trailerSize)
	trailer[6] = byte(offsetSize)
	trailer[7] = byte(refSize)
	binary.BigEndian.PutUint64(trailer[8:], uint64(count))
	binary.BigEndian.PutUint64(trailer[16:], 0) // The array
	binary.BigEndian.PutUint64(trailer[24:], tableAt)
	b.Write(trailer)
	return b.Bytes()
}

// writeMarker writes an object's marker with its count, in the low nibble
// if it fits there, otherwise as an int object after it
func writeMarker(b *bytes.Buffer, marker byte, n int) {
	if n <= maxNibbleLen {
		b.WriteByte(marker<<4 | byte(n))
		return
	}
	b.WriteByte(marker<<4 | countInFull)
	size := uintSize(uint64(n))
	power := byte(bits.TrailingZeros(uint(size))) // 1, 2, 4, 8 bytes are 2^0 to 2^3
	b.WriteByte(markerInt<<4 | power)
	writeUint(b, uint64(n), size)
}

// uintSize returns the fewest bytes, of 1, 2, 4 or 8, that hold n
func uintSize(n uint64) int {
	switch {
	case n <= 0xFF:
		return 1
	case n <= 0xFFFF:
		return 2
	case n <= 0xFFFFFFFF:
		return 4
	}
	return 8
}

// writeUint writes n big-endian in size bytes
func writeUint(b *bytes.Buffer, n uint64, size int) {
	for i := size - 1; i >= 0; i-- {
		b.WriteByte(byte(n >> (8 * i)))
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// decodeXML reads an XML property list holding an array of strings
func decodeXML(data []byte) ([]string, error) {
	var doc struct {
		Array *struct {
			Items []struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			} `xml:",any"`
		} `xml:"array"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", errPlist, err)
	}
	if doc.Array == nil {
		return nil, fmt.Errorf("%w: not an array", errPlist)
	}
	out := make([]string, 0, len(doc.Array.Items))
	for _, item := range doc.Array.Items {
		if item.XMLName.Local != "string" {
			return nil, fmt.Errorf("%w: holds something other than text", errPlist)
		}
		out = append(out, item.Text)
	}
	return out, nil
}
