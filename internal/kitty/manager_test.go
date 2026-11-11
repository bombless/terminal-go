package kitty

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
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
