package term

// RuneWidth reports how many terminal cells r occupies. East Asian wide and
// fullwidth runes occupy two cells; everything else occupies one.
func RuneWidth(r rune) int {
	if r < 0x1100 {
		return 1
	}
	for _, interval := range wideRanges {
		if r < interval[0] {
			return 1
		}
		if r <= interval[1] {
			return 2
		}
	}
	return 1
}

// wideRanges covers the BMP East Asian Wide (W) and Fullwidth (F) ranges.
// Supplementary-plane ideographs and emoji are not listed because the renderer
// currently draws only BMP cells.
var wideRanges = [...][2]rune{
	{0x1100, 0x115F}, // Hangul Jamo
	{0x2E80, 0x303E}, // CJK radicals, Kangxi, CJK symbols and punctuation
	{0x3041, 0x33FF}, // Hiragana, Katakana, Bopomofo, CJK compatibility
	{0x3400, 0x4DBF}, // CJK unified ideographs extension A
	{0x4E00, 0x9FFF}, // CJK unified ideographs
	{0xA000, 0xA4CF}, // Yi syllables and radicals
	{0xA960, 0xA97F}, // Hangul Jamo extended A
	{0xAC00, 0xD7A3}, // Hangul syllables
	{0xF900, 0xFAFF}, // CJK compatibility ideographs
	{0xFE10, 0xFE19}, // Vertical forms
	{0xFE30, 0xFE6F}, // CJK compatibility forms, small form variants
	{0xFF00, 0xFF60}, // Fullwidth forms
	{0xFFE0, 0xFFE6}, // Fullwidth signs
}
