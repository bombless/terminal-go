package term

import "testing"

func TestParserKittyAPCSurvivesSplitFeeds(t *testing.T) {
	s := NewScreen(32, 2)
	p := NewParser(s)
	var got []APC
	p.OnAPC = func(a APC) { got = append(got, a) }
	p.Feed([]byte("hello\x1b_Ga=T,f=100,i=7;YWJ"))
	p.Feed([]byte("j\x1b"))
	p.Feed([]byte("\\world"))
	if len(got) != 1 {
		t.Fatalf("APC callbacks = %d, want 1", len(got))
	}
	if got[0].Command != 'G' || got[0].Control != "a=T,f=100,i=7" || string(got[0].Payload) != "YWJj" {
		t.Fatalf("APC = %+v, want Kitty graphics control and payload", got[0])
	}
	if line := string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune, s.Cells[0][2].Rune, s.Cells[0][3].Rune, s.Cells[0][4].Rune, s.Cells[0][5].Rune, s.Cells[0][6].Rune, s.Cells[0][7].Rune, s.Cells[0][8].Rune, s.Cells[0][9].Rune, s.Cells[0][10].Rune}); line != "helloworld " {
		t.Fatalf("APC leaked into terminal text: %q", line)
	}
}

func TestParserUnknownAndMalformedAPCAreSwallowed(t *testing.T) {
	s := NewScreen(20, 2)
	p := NewParser(s)
	var got []APC
	p.OnAPC = func(a APC) { got = append(got, a) }
	p.Feed([]byte("\x1b_Xunknown\x1b\\\x1b_Ga=T;\x1b\\Z"))
	if len(got) != 1 || len(got[0].Payload) != 0 {
		t.Fatalf("APC callbacks = %+v, want one empty-payload graphics APC", got)
	}
	if s.Cells[0][0].Rune != 'Z' || s.X != 1 {
		t.Fatalf("screen after APC = (%q, cursor %d), want Z at 0", s.Cells[0][0].Rune, s.X)
	}
}

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

func TestParserFaintAttribute(t *testing.T) {
	s := NewScreen(8, 2)
	p := NewParser(s)
	p.Feed([]byte("\x1b[97;2;3mA\x1b[22mB\x1b[2mC\x1b[0mD"))
	tests := []struct {
		x     int
		r     rune
		fg    uint32
		faint bool
	}{
		{0, 'A', 15, true},
		{1, 'B', 7, false},
		{2, 'C', 7, true},
		{3, 'D', 7, false},
	}
	for _, tt := range tests {
		cell := s.Cells[0][tt.x]
		if cell.Rune != tt.r || cell.FG != tt.fg || cell.Faint != tt.faint {
			t.Errorf("cell %d = %+v, want rune=%q fg=%d faint=%v", tt.x, cell, tt.r, tt.fg, tt.faint)
		}
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

func TestParserDECOriginModeUsesScrollingRegion(t *testing.T) {
	s := NewScreen(10, 8)
	p := NewParser(s)
	p.Feed([]byte("\x1b[3;6r\x1b[?6h\x1b[1;1H"))
	if s.X != 0 || s.Y != 2 {
		t.Fatalf("origin-mode home = (%d,%d), want (0,2)", s.X, s.Y)
	}
	p.Feed([]byte("\x1b[2;4H"))
	if s.X != 3 || s.Y != 3 {
		t.Fatalf("origin-mode CUP = (%d,%d), want (3,3)", s.X, s.Y)
	}
	p.Feed([]byte("\x1b[?6l\x1b[1;1H"))
	if s.X != 0 || s.Y != 0 {
		t.Fatalf("absolute-mode home = (%d,%d), want (0,0)", s.X, s.Y)
	}
}

func TestParserScrollAndAltScreen(t *testing.T) {
	s := NewScreen(4, 2)
	p := NewParser(s)
	p.Feed([]byte("12345678"))
	// Test CSI S (Scroll Up 1)
	p.Feed([]byte("\x1b[1S"))
	if len(s.History) != 1 {
		t.Fatalf("history len = %d, want 1", len(s.History))
	}
	if string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune}) != "56" {
		t.Fatalf("cell after scroll up = %q, want 56", string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune}))
	}

	// Test CSI 3 J (Clear scrollback)
	p.Feed([]byte("\x1b[3J"))
	if len(s.History) != 0 {
		t.Fatalf("history after 3J = %d, want 0", len(s.History))
	}

	// Test DECSET 1049 / DECRST 1049
	p.Feed([]byte("\x1b[2J\x1b[H"))
	p.Feed([]byte("ABCD\x1b[?1049h"))
	if !s.IsAlt() {
		t.Fatal("expected alternate screen active after ?1049h")
	}
	p.Feed([]byte("XYZ"))
	if s.Cells[0][0].Rune != 'X' {
		t.Fatalf("alt screen cell = %c, want X", s.Cells[0][0].Rune)
	}
	p.Feed([]byte("\x1b[?1049l"))
	if s.IsAlt() {
		t.Fatal("expected alternate screen inactive after ?1049l")
	}
	if s.Cells[0][0].Rune != 'A' {
		t.Fatalf("restored cell = %c, want A", s.Cells[0][0].Rune)
	}
}
