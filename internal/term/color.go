package term

// ColorRef converts an ANSI palette index or tagged 24-bit RGB value to a
// Windows GDI COLORREF (0x00BBGGRR). Truecolor uses the parser's high-byte tag.
func ColorRef(color uint32) uint32 {
	var rgb uint32
	if color&0xFF000000 != 0 {
		rgb = color & 0xFFFFFF
	} else {
		base := [...]uint32{0x000000, 0xCD0000, 0x00CD00, 0xCDCD00, 0x0000EE, 0xCD00CD, 0x00CDCD, 0xE5E5E5, 0x7F7F7F, 0xFF0000, 0x00FF00, 0xFFFF00, 0x5C5CFF, 0xFF00FF, 0x00FFFF, 0xFFFFFF}
		switch {
		case color < uint32(len(base)):
			rgb = base[color]
		case color < 232:
			n := color - 16
			levels := [...]uint32{0, 95, 135, 175, 215, 255}
			rgb = levels[n/36]<<16 | levels[(n/6)%6]<<8 | levels[n%6]
		case color < 256:
			v := uint32(8 + (color-232)*10)
			rgb = v<<16 | v<<8 | v
		}
	}
	return (rgb&0xFF)<<16 | (rgb & 0xFF00) | (rgb>>16)&0xFF
}

// DimColorRef returns the faint variant of color for SGR 2. Each channel is
// halved, which matches how dim text reads against a dark background.
func DimColorRef(color uint32) uint32 {
	return (ColorRef(color) >> 1) & 0x007F7F7F
}
