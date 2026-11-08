package term

import "testing"

func TestParserKeepsCSIStateAcrossFeed(t *testing.T) {
	s := NewScreen(8, 3)
	p := NewParser(s)
	p.Feed([]byte("abc\x1b["))
	p.Feed([]byte("2;3H"))
	p.Feed([]byte("X"))
	if got := s.Cells[1][2].Rune; got != 'X' {
		t.Fatalf("cell(2,1) = %q, want X", got)
	}
	if s.X != 3 || s.Y != 1 {
		t.Fatalf("cursor = (%d,%d), want (3,1)", s.X, s.Y)
	}
}

func TestParserDecodesSplitUTF8(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	p.Feed([]byte{0xe4, 0xb8})
	if s.Cells[0][0].Rune != ' ' {
		t.Fatal("incomplete UTF-8 sequence was emitted early")
	}
	p.Feed([]byte{0xad})
	if got := s.Cells[0][0].Rune; got != '中' {
		t.Fatalf("decoded rune = %q, want 中", got)
	}
}

func TestParserCursorEraseAndSGR(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	p.Feed([]byte("abcdef\x1b[1;3H\x1b[31;44mZ\x1b[K"))
	if got := s.Cells[0][2]; got.Rune != 'Z' || got.FG != 1 || got.BG != 4 {
		t.Fatalf("styled cell = %+v, want Z with fg=1 bg=4", got)
	}
	for x := 3; x < s.Width; x++ {
		if got := s.Cells[0][x].Rune; got != ' ' {
			t.Fatalf("cell %d after erase = %q, want space", x, got)
		}
	}
}

func TestParserEraseDisplayPreservesCursor(t *testing.T) {
	s := NewScreen(4, 2)
	p := NewParser(s)
	p.Feed([]byte("abcd\x1b[2;3H\x1b[2J"))
	if s.X != 2 || s.Y != 1 {
		t.Fatalf("cursor after ED 2 = (%d,%d), want (2,1)", s.X, s.Y)
	}
	for y := range s.Cells {
		for x := range s.Cells[y] {
			if s.Cells[y][x].Rune != ' ' {
				t.Fatalf("cell (%d,%d) was not erased", x, y)
			}
		}
	}
}

func TestParserExtendedColorsAndOSC(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	p.Feed([]byte("\x1b]0;this title must not appear\x07\x1b[38;5;196;48;2;1;2;3mA"))
	cell := s.Cells[0][0]
	if cell.Rune != 'A' {
		t.Fatalf("OSC text leaked into screen: got %q", cell.Rune)
	}
	if cell.FG != 196 {
		t.Fatalf("256-color foreground = %d, want 196", cell.FG)
	}
	wantBG := trueColorFlag | 0x010203
	if cell.BG != wantBG {
		t.Fatalf("truecolor background = %#x, want %#x", cell.BG, wantBG)
	}
}

func TestParserSaveRestoreCursorAndResetColors(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	p.Feed([]byte("\x1b[32;45m\x1b[1;4H\x1b7\x1b[1;1H\x1b8X\x1b[0mY"))
	if got := s.Cells[0][3]; got.Rune != 'X' || got.FG != 2 || got.BG != 5 {
		t.Fatalf("restored cursor cell = %+v, want X with fg=2 bg=5", got)
	}
	if got := s.Cells[0][4]; got.Rune != 'Y' || got.FG != 7 || got.BG != 0 {
		t.Fatalf("reset cell = %+v, want Y with default colors", got)
	}
}

func TestParserDECTCEMCursorVisibility(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	if !s.CursorVisible {
		t.Fatal("CursorVisible should default to true")
	}
	p.Feed([]byte("\x1b[?25l"))
	if s.CursorVisible {
		t.Fatal("CursorVisible should be false after DECRST 25")
	}
	p.Feed([]byte("\x1b[?25h"))
	if !s.CursorVisible {
		t.Fatal("CursorVisible should be true after DECSET 25")
	}
}
