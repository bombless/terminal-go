// Package kitty implements a bounded inline subset of the Kitty Graphics Protocol.
package kitty

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/bombless/terminal-go/internal/term"
)

const (
	maxEncodedBytes = 24 << 20
	maxImageBytes   = 32 << 20
	maxPixels       = 16 << 20
	maxImages       = 128
	maxPlacements   = 4096
)

type Placement struct {
	ImageID             uint32
	X, Y, Columns, Rows int
}
type transfer struct {
	params map[string]string
	data   []byte
	id     uint32
	action string
	quiet  string
}
type Manager struct {
	mu         sync.RWMutex
	images     map[uint32]*image.NRGBA
	placements []Placement
	pending    *transfer
	nextID     uint32
	bytes      int
}

func NewManager() *Manager { return &Manager{images: make(map[uint32]*image.NRGBA), nextID: 1} }

// Handle consumes one complete Kitty APC. Cursor coordinates are sampled at
// completion, including for chunked transfers.
func (m *Manager) Handle(apc term.APC, cursorX, cursorY int) (string, bool) {
	if apc.Command != 'G' {
		return "", false
	}
	p, err := parseParams(apc.Control)
	if err != nil {
		return response(0, "EINVAL: "+err.Error()), false
	}
	action := p["a"]
	if action == "" {
		action = "t"
	}
	quiet := p["q"]
	if quiet != "" && quiet != "0" && quiet != "1" && quiet != "2" {
		return response(0, "EINVAL: invalid response suppression"), false
	}
	id, err := uintParam(p, "i", 0)
	if err != nil {
		return maybeResponse(quiet, 0, "EINVAL: invalid image id"), false
	}
	if medium := p["t"]; medium != "" && medium != "d" {
		return maybeResponse(quiet, id, "ENOTSUP: transmission medium"), false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch action {
	case "q":
		if p["e"] != "" {
			return maybeResponse(quiet, id, "EINVAL: feature query unsupported"), false
		}
		return maybeResponse(quiet, id, "OK"), false
	case "d":
		if id == 0 {
			m.images = make(map[uint32]*image.NRGBA)
			m.placements = nil
			m.bytes = 0
		} else {
			if img := m.images[id]; img != nil {
				m.bytes -= len(img.Pix)
				delete(m.images, id)
			}
			m.removePlacements(id)
		}
		return maybeResponse(quiet, id, "OK"), true
	case "p":
		img := m.images[id]
		if img == nil {
			return maybeResponse(quiet, id, "ENOENT: image not found"), false
		}
		if len(m.placements) >= maxPlacements {
			return maybeResponse(quiet, id, "E2BIG: placement limit"), false
		}
		m.placements = append(m.placements, m.makePlacement(id, img, p, cursorX, cursorY))
		return maybeResponse(quiet, id, "OK"), true
	case "t", "T":
		if p["o"] != "" {
			return maybeResponse(quiet, id, "ENOTSUP: compression"), false
		}
		if id == 0 {
			id = m.allocateID()
		}
		more, e := uintParam(p, "m", 0)
		if e != nil || more > 1 {
			m.pending = nil
			return maybeResponse(quiet, id, "EINVAL: invalid chunk flag"), false
		}
		if m.pending == nil {
			m.pending = &transfer{params: p, data: make([]byte, 0, len(apc.Payload)), id: id, action: action, quiet: quiet}
		} else if len(p) > 2 {
			m.pending = nil
			return maybeResponse(quiet, id, "EINVAL: unexpected parameters in continuation"), false
		}
		if len(m.pending.data)+len(apc.Payload) > maxEncodedBytes {
			m.pending = nil
			return maybeResponse(quiet, id, "E2BIG: image transfer limit"), false
		}
		m.pending.data = append(m.pending.data, apc.Payload...)
		if more == 1 {
			return "", false
		}
		t := m.pending
		m.pending = nil
		id, action, quiet = t.id, t.action, t.quiet
		imageID, decodeErr := m.decodeTransfer(id, t)
		if decodeErr != nil {
			return maybeResponse(quiet, id, "EINVAL: "+decodeErr.Error()), false
		}
		changed := false
		if action == "T" {
			if len(m.placements) >= maxPlacements {
				return maybeResponse(quiet, imageID, "E2BIG: placement limit"), false
			}
			m.placements = append(m.placements, m.makePlacement(imageID, m.images[imageID], t.params, cursorX, cursorY))
			changed = true
		}
		return maybeResponse(quiet, imageID, "OK"), changed
	default:
		return maybeResponse(quiet, id, "ENOTSUP: action"), false
	}
}

func (m *Manager) decodeTransfer(id uint32, t *transfer) (uint32, error) {
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(t.data)))
	n, err := base64.StdEncoding.Strict().Decode(decoded, t.data)
	if err != nil {
		return id, fmt.Errorf("invalid base64 payload")
	}
	decoded = decoded[:n]
	format, err := uintParam(t.params, "f", 32)
	if err != nil {
		return id, fmt.Errorf("invalid pixel format")
	}
	var src image.Image
	switch format {
	case 100:
		cfg, e := png.DecodeConfig(bytes.NewReader(decoded))
		if e != nil {
			return id, fmt.Errorf("invalid PNG: %w", e)
		}
		if !validDimensions(cfg.Width, cfg.Height) {
			return id, fmt.Errorf("image dimensions exceed limits")
		}
		src, err = png.Decode(bytes.NewReader(decoded))
	case 24, 32:
		w, e1 := uintParam(t.params, "s", 0)
		h, e2 := uintParam(t.params, "v", 0)
		if e1 != nil || e2 != nil || w == 0 || h == 0 || !validDimensions(int(w), int(h)) {
			return id, fmt.Errorf("invalid raw image dimensions")
		}
		channels := 4
		if format == 24 {
			channels = 3
		}
		if uint64(w)*uint64(h)*uint64(channels) != uint64(len(decoded)) {
			return id, fmt.Errorf("raw payload length does not match dimensions")
		}
		raw := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
		for i, j := 0, 0; i < len(decoded); i, j = i+channels, j+4 {
			raw.Pix[j], raw.Pix[j+1], raw.Pix[j+2] = decoded[i], decoded[i+1], decoded[i+2]
			raw.Pix[j+3] = 255
			if channels == 4 {
				raw.Pix[j+3] = decoded[i+3]
			}
		}
		src = raw
	default:
		return id, fmt.Errorf("unsupported pixel format %d", format)
	}
	if err != nil {
		return id, fmt.Errorf("decode image: %w", err)
	}
	b := src.Bounds()
	if !validDimensions(b.Dx(), b.Dy()) {
		return id, fmt.Errorf("image dimensions exceed limits")
	}
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			rgba.SetNRGBA(x, y, color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA))
		}
	}
	oldBytes := 0
	if old := m.images[id]; old != nil {
		oldBytes = len(old.Pix)
	}
	if len(rgba.Pix) > maxImageBytes || m.bytes-oldBytes+len(rgba.Pix) > maxImageBytes || (m.images[id] == nil && len(m.images) >= maxImages) {
		return id, fmt.Errorf("image cache quota exceeded")
	}
	m.bytes -= oldBytes
	m.images[id] = rgba
	m.bytes += len(rgba.Pix)
	return id, nil
}

func (m *Manager) makePlacement(id uint32, img *image.NRGBA, p map[string]string, x, y int) Placement {
	c, _ := uintParam(p, "c", 0)
	r, _ := uintParam(p, "r", 0)
	if c == 0 {
		c = uint32(math.Max(1, math.Ceil(float64(img.Bounds().Dx())/10)))
	}
	if r == 0 {
		r = uint32(math.Max(1, math.Ceil(float64(img.Bounds().Dy())/20)))
	}
	return Placement{ImageID: id, X: x, Y: y, Columns: int(c), Rows: int(r)}
}
func (m *Manager) removePlacements(id uint32) {
	out := m.placements[:0]
	for _, p := range m.placements {
		if p.ImageID != id {
			out = append(out, p)
		}
	}
	m.placements = out
}
func (m *Manager) allocateID() uint32 {
	for {
		id := m.nextID
		m.nextID++
		if m.nextID == 0 {
			m.nextID = 1
		}
		if id != 0 && m.images[id] == nil {
			return id
		}
	}
}

// Snapshot returns a stable placement list and immutable cached pixel pointers.
func (m *Manager) Snapshot() ([]Placement, map[uint32]*image.NRGBA) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := append([]Placement(nil), m.placements...)
	imgs := make(map[uint32]*image.NRGBA, len(m.images))
	for id, img := range m.images {
		imgs[id] = img
	}
	return p, imgs
}

func parseParams(control string) (map[string]string, error) {
	p := make(map[string]string)
	if !utf8.ValidString(control) {
		return nil, fmt.Errorf("invalid control data")
	}
	for _, field := range strings.Split(control, ",") {
		if field == "" {
			continue
		}
		k, v, ok := strings.Cut(field, "=")
		if !ok || len(k) != 1 || len(v) > 32 {
			return nil, fmt.Errorf("malformed parameter")
		}
		if _, ok := p[k]; ok {
			return nil, fmt.Errorf("duplicate parameter %q", k)
		}
		p[k] = v
	}
	return p, nil
}
func uintParam(p map[string]string, key string, fallback uint32) (uint32, error) {
	v := p[key]
	if v == "" {
		return fallback, nil
	}
	n, e := strconv.ParseUint(v, 10, 32)
	if e != nil {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return uint32(n), nil
}
func validDimensions(w, h int) bool {
	return w > 0 && h > 0 && uint64(w)*uint64(h) <= maxPixels && uint64(w)*uint64(h)*4 <= maxImageBytes
}
func response(id uint32, status string) string {
	if id == 0 {
		return "\x1b_G" + status + "\x1b\\"
	}
	return fmt.Sprintf("\x1b_Gi=%d;%s\x1b\\", id, status)
}
func maybeResponse(q string, id uint32, status string) string {
	if q == "1" || (q == "2" && status == "OK") {
		return ""
	}
	return response(id, status)
}
