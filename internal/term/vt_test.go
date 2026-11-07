package term

import "testing"

func TestParserKeepsCSIStateAcrossFeed(t *testing.T) {
	s := NewScreen(8, 3)
	p := NewParser(s)
	p.Feed([]byte("abc\x1b["))
	p.Feed([]byte("2;3H"))
	p.Feed([]byte("X"))
	if got := s.Cells[1][2].Rune; got != 'X' { t.Fatalf("cell(2,1) = %q, want X", got) }
	if s.X != 3 || s.Y != 1 { t.Fatalf("cursor = (%d,%d), want (3,1)", s.X, s.Y) }
}
func TestParserDecodesSplitUTF8(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	p.Feed([]byte{0xe4, 0xb8})
	if s.Cells[0][0].Rune != ' ' { t.Fatal("incomplete UTF-8 sequence was emitted early") }
	p.Feed([]byte{0xad})
	if got := s.Cells[0][0].Rune; got != '中' { t.Fatalf("decoded rune = %q, want 中", got) }
}
func TestParserCursorEraseAndSGR(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	p.Feed([]byte("abcdef\x1b[1;3H\x1b[31;44mZ\x1b[K"))
	if got := s.Cells[0][2]; got.Rune != 'Z' || got.FG != 1 || got.BG != 4 { t.Fatalf("styled cell = %+v, want Z with fg=1 bg=4", got) }
	for x := 3; x < s.Width; x++ { if got := s.Cells[0][x].Rune; got != ' ' { t.Fatalf("cell %d after erase = %q, want space", x, got) } }
}
func TestParserEraseDisplayPreservesCursor(t *testing.T) {
	s := NewScreen(4, 2)
	p := NewParser(s)
	p.Feed([]byte("abcd\x1b[2;3H\x1b[2J"))
	if s.X != 2 || s.Y != 1 { t.Fatalf("cursor after ED 2 = (%d,%d), want (2,1)", s.X, s.Y) }
	for y := range s.Cells { for x := range s.Cells[y] { if s.Cells[y][x].Rune != ' ' { t.Fatalf("cell (%d,%d) was not erased", x, y) } } }
}
