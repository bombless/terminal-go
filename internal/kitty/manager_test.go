package kitty

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/bombless/terminal-go/internal/term"
)

func pngPayload(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 128})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return []byte(base64.StdEncoding.EncodeToString(b.Bytes()))
}

func apc(control string, payload []byte) term.APC {
	return term.APC{Command: 'G', Control: control, Payload: payload}
}

func TestChunkedPNGTransmitAndDisplay(t *testing.T) {
	m := NewManager()
	data := pngPayload(t)
	if reply, changed := m.Handle(apc("a=T,f=100,i=77,m=1", data[:len(data)/2]), 2, 3); reply != "" || changed {
		t.Fatalf("first chunk = %q, %v", reply, changed)
	}
	reply, changed := m.Handle(apc("m=0", data[len(data)/2:]), 2, 3)
	if reply != "\x1b_Gi=77;OK\x1b\\" || !changed {
		t.Fatalf("final chunk = %q, %v", reply, changed)
	}
	pl, imgs := m.Snapshot()
	if len(pl) != 1 || pl[0].ImageID != 77 || pl[0].X != 2 || pl[0].Y != 3 {
		t.Fatalf("placements = %+v", pl)
	}
	if got := imgs[77]; got == nil || got.Bounds().Dx() != 2 || got.NRGBAAt(1, 0).A != 128 {
		t.Fatalf("decoded PNG = %#v", got)
	}
}

func TestParserAnchorsKittyPlacementAndAdvancesCursor(t *testing.T) {
	screen := term.NewScreen(80, 30)
	parser := term.NewParser(screen)
	manager := NewManager()
	screen.OnScroll = func(lines int) { manager.ShiftPlacements(-lines) }
	parser.OnAPC = func(command term.APC) {
		_, _, columns, rows := manager.HandleWithCursor(command, screen.X, screen.Y)
		screen.AdvanceCursor(columns, rows)
	}

	// Simulate diagnostics followed by a Kitty placement at row 28, column 1.
	parser.Feed([]byte("\x1b[28;1H"))
	stream := append([]byte("\x1b_Ga=T,f=100,i=88,c=4,r=2;"), pngPayload(t)...)
	stream = append(stream, 0x1b, '\\')
	parser.Feed(stream)

	placements, images := manager.Snapshot()
	if len(placements) != 1 || images[88] == nil {
		t.Fatalf("placements=%+v image=%v", placements, images[88])
	}
	if got := placements[0]; got.X != 0 || got.Y != 27 || got.Columns != 4 || got.Rows != 2 {
		t.Fatalf("placement = %+v, want cursor-anchored (0,27) size 4x2", got)
	}
	if screen.X != 4 || screen.Y != 29 {
		t.Fatalf("cursor = (%d,%d), want (4,29) after default C=0 advance", screen.X, screen.Y)
	}
}

func TestPlacementScrollsWithContentAndCursorStaysOnScreen(t *testing.T) {
	screen := term.NewScreen(104, 30)
	parser := term.NewParser(screen)
	manager := NewManager()
	screen.OnScroll = func(lines int) { manager.ShiftPlacements(-lines) }
	parser.OnAPC = func(command term.APC) {
		_, _, columns, rows := manager.HandleWithCursor(command, screen.X, screen.Y)
		screen.AdvanceCursor(columns, rows)
	}

	// Reproduce the remote tview layout: cursor at row 27, image 29x22 cells.
	parser.Feed([]byte("\x1b[28;1H"))
	stream := append([]byte("\x1b_Ga=T,f=100,i=7,c=29,r=22;"), pngPayload(t)...)
	stream = append(stream, 0x1b, '\\')
	parser.Feed(stream)

	placements, _ := manager.Snapshot()
	if len(placements) != 1 {
		t.Fatalf("placements = %+v", placements)
	}
	if got := placements[0]; got.Y != 7 {
		t.Fatalf("placement Y = %d, want 7 after 20 lines of scrolling", got.Y)
	}
	if len(screen.History) != 20 {
		t.Fatalf("history lines = %d, want 20", len(screen.History))
	}
	if screen.X != 29 || screen.Y != 29 {
		t.Fatalf("cursor = (%d,%d), want (29,29)", screen.X, screen.Y)
	}
}

func TestCursorPolicyOneKeepsCursorInPlace(t *testing.T) {
	screen := term.NewScreen(80, 30)
	parser := term.NewParser(screen)
	manager := NewManager()
	parser.OnAPC = func(command term.APC) {
		_, _, columns, rows := manager.HandleWithCursor(command, screen.X, screen.Y)
		screen.AdvanceCursor(columns, rows)
	}

	parser.Feed([]byte("\x1b[10;5H"))
	stream := append([]byte("\x1b_Ga=T,f=100,i=9,C=1,c=4,r=2;"), pngPayload(t)...)
	stream = append(stream, 0x1b, '\\')
	parser.Feed(stream)

	if screen.X != 4 || screen.Y != 9 {
		t.Fatalf("cursor = (%d,%d), want unchanged (4,9) for C=1", screen.X, screen.Y)
	}
}

func TestTransmitThenPlaceAndDelete(t *testing.T) {
	m := NewManager()
	reply, changed := m.Handle(apc("a=t,f=100,i=9", pngPayload(t)), 0, 0)
	if reply != "\x1b_Gi=9;OK\x1b\\" || changed {
		t.Fatalf("transmit = %q, %v", reply, changed)
	}
	reply, changed = m.Handle(apc("a=p,i=9,c=4,r=2", nil), 5, 6)
	if reply != "\x1b_Gi=9;OK\x1b\\" || !changed {
		t.Fatalf("place = %q, %v", reply, changed)
	}
	pl, _ := m.Snapshot()
	if len(pl) != 1 || pl[0].Columns != 4 || pl[0].Rows != 2 {
		t.Fatalf("placement = %+v", pl)
	}
	_, changed = m.Handle(apc("a=d,i=9", nil), 0, 0)
	pl, imgs := m.Snapshot()
	if !changed || len(pl) != 0 || len(imgs) != 0 {
		t.Fatalf("delete left placements=%v images=%v", pl, len(imgs))
	}
}

func TestInvalidBase64AndUnsupportedMedium(t *testing.T) {
	m := NewManager()
	if reply, _ := m.Handle(apc("a=T,f=100,i=1", []byte("%%%")), 0, 0); reply == "" {
		t.Fatal("invalid base64 must return an error")
	}
	if reply, _ := m.Handle(apc("a=T,f=100,t=f,i=1", nil), 0, 0); reply == "" {
		t.Fatal("file transport must be rejected")
	}
	_, imgs := m.Snapshot()
	if len(imgs) != 0 {
		t.Fatalf("invalid transfers cached %d images", len(imgs))
	}
}

func TestQueryAndQuietResponse(t *testing.T) {
	m := NewManager()
	if reply, _ := m.Handle(apc("a=q,i=42", nil), 0, 0); reply != "\x1b_Gi=42;OK\x1b\\" {
		t.Fatalf("query response = %q", reply)
	}
	if reply, _ := m.Handle(apc("a=q,i=42,q=1", nil), 0, 0); reply != "" {
		t.Fatalf("quiet query response = %q", reply)
	}
	if reply, _ := m.Handle(apc("a=q,i=42,q=2", nil), 0, 0); reply != "" {
		t.Fatalf("q=2 should suppress successful responses, got %q", reply)
	}
	if reply, _ := m.Handle(apc("a=p,i=42,q=2", nil), 0, 0); reply == "" {
		t.Fatal("q=2 must still report errors")
	}
}

func TestDuplicateAndMalformedParameters(t *testing.T) {
	if _, err := parseParams("a=T,a=t"); err == nil {
		t.Fatal("duplicate parameters accepted")
	}
	if _, err := parseParams("a"); err == nil {
		t.Fatal("parameter without value accepted")
	}
}

func TestRawRGBAndRGBAFormats(t *testing.T) {
	tests := []struct {
		name, control string
		payload       []byte
		id            uint32
		want          color.NRGBA
	}{
		{"rgb", "a=t,f=24,s=1,v=1,i=31", []byte("/wCA"), 31, color.NRGBA{R: 255, G: 0, B: 128, A: 255}},
		{"rgba", "a=t,f=32,s=1,v=1,i=32", []byte("/wCAQA=="), 32, color.NRGBA{R: 255, G: 0, B: 128, A: 64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager()
			reply, changed := m.Handle(apc(tt.control, tt.payload), 0, 0)
			if reply == "" || changed {
				t.Fatalf("raw transmit = %q, %v; want acknowledgement without placement", reply, changed)
			}
			_, imgs := m.Snapshot()
			if got := imgs[tt.id]; got == nil || got.NRGBAAt(0, 0) != tt.want {
				t.Fatalf("decoded raw pixel = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRawPayloadLengthMismatchIsRejected(t *testing.T) {
	m := NewManager()
	if reply, changed := m.Handle(apc("a=T,f=24,s=2,v=1,i=5", []byte("AQID")), 0, 0); reply == "" || changed {
		t.Fatalf("mismatched raw payload = %q, %v; want error without placement", reply, changed)
	}
	_, imgs := m.Snapshot()
	if len(imgs) != 0 {
		t.Fatalf("mismatched raw payload cached %d images", len(imgs))
	}
}

func TestReplacingImageUpdatesCacheAccountingAndPlacement(t *testing.T) {
	m := NewManager()
	if _, _ = m.Handle(apc("a=T,f=100,i=8", pngPayload(t)), 1, 2); m.bytes == 0 {
		t.Fatal("expected decoded image bytes in cache")
	}
	firstBytes := m.bytes
	if _, changed := m.Handle(apc("a=T,f=100,i=8", pngPayload(t)), 3, 4); !changed {
		t.Fatal("replacement transmit should update its placement")
	}
	if m.bytes != firstBytes || len(m.placements) != 2 {
		t.Fatalf("after replacement bytes=%d placements=%d, want bytes=%d placements=2", m.bytes, len(m.placements), firstBytes)
	}
	if _, changed := m.Handle(apc("a=d,i=8", nil), 0, 0); !changed || m.bytes != 0 || len(m.images) != 0 || len(m.placements) != 0 {
		t.Fatalf("after delete changed=%v bytes=%d images=%d placements=%d", changed, m.bytes, len(m.images), len(m.placements))
	}
}

func TestUnknownParameterIsIgnoredAndFeatureQueryRejected(t *testing.T) {
	m := NewManager()
	if reply, _ := m.Handle(apc("a=q,i=12,z=vendor-extension", nil), 0, 0); reply != "\x1b_Gi=12;OK\x1b\\" {
		t.Fatalf("query with unknown parameter = %q, want normal OK response", reply)
	}
	if reply, _ := m.Handle(apc("a=q,i=12,e=1", nil), 0, 0); reply == "" || !strings.Contains(reply, "unsupported") {
		t.Fatalf("feature query response = %q, want explicit unsupported response", reply)
	}
}

func TestMalformedPNGDoesNotEnterCache(t *testing.T) {
	m := NewManager()
	reply, changed := m.Handle(apc("a=T,f=100,i=99", []byte("bm90LWEtcG5n")), 0, 0)
	if reply == "" || changed {
		t.Fatalf("malformed PNG = %q, %v; want error without placement", reply, changed)
	}
	_, imgs := m.Snapshot()
	if len(imgs) != 0 || m.bytes != 0 {
		t.Fatalf("malformed PNG left images=%d bytes=%d", len(imgs), m.bytes)
	}
}

func TestProtocolFixtureCompleteAndChunkedTransfers(t *testing.T) {
	fixture, err := os.ReadFile("testdata/protocol.apc")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager()
	screen := term.NewScreen(80, 25)
	parser := term.NewParser(screen)
	var replies []string
	var changed int
	parser.OnAPC = func(command term.APC) {
		reply, redraw := manager.Handle(command, screen.X, screen.Y)
		replies = append(replies, reply)
		if redraw {
			changed++
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(fixture)), "\n") {
		_, sequence, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			t.Fatalf("malformed fixture line %q", line)
		}
		parser.Feed([]byte(sequence))
	}
	placements, images := manager.Snapshot()
	if len(images) != 2 || len(placements) != 2 || changed != 2 {
		t.Fatalf("fixture results: images=%d placements=%d redraws=%d", len(images), len(placements), changed)
	}
	for _, id := range []uint32{17, 18} {
		img := images[id]
		if img == nil || img.Bounds() != image.Rect(0, 0, 1, 1) || img.NRGBAAt(0, 0) != (color.NRGBA{R: 255, A: 255}) {
			t.Fatalf("fixture image %d = %#v, want opaque red 1x1", id, img)
		}
	}
	if len(replies) != 3 || replies[0] == "" || replies[1] != "" || replies[2] == "" {
		t.Fatalf("fixture replies = %#v, want full ACK, suppressed intermediate chunk, final ACK", replies)
	}
}
