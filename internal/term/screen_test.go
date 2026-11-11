package term

import "testing"

func TestScreenWrapAndScroll(t *testing.T) {
	s := NewScreen(3, 2)
	for _, r := range "abcdefg" {
		s.Put(r)
	}
	if got := string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune, s.Cells[0][2].Rune}); got != "def" {
		t.Fatalf("top row = %q, want def", got)
	}
	if got := string([]rune{s.Cells[1][0].Rune, s.Cells[1][1].Rune, s.Cells[1][2].Rune}); got != "g  " {
		t.Fatalf("bottom row = %q, want g  ", got)
	}
}

func TestScreenEraseLineAndDisplay(t *testing.T) {
	s := NewScreen(4, 2)
	for _, r := range "abcdefgh" {
		s.Put(r)
	}
	s.Move(2, 0)
	s.eraseLineFromCursor()
	if s.Cells[0][0].Rune != 'a' || s.Cells[0][1].Rune != 'b' || s.Cells[0][2].Rune != ' ' {
		t.Fatal("eraseLineFromCursor did not preserve prefix and clear suffix")
	}
	s.Move(1, 0)
	s.eraseDisplayFromCursor()
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			if y == 0 && x == 0 {
				if s.Cells[y][x].Rune != 'a' {
					t.Fatal("erase changed cell before cursor")
				}
				continue
			}
			if s.Cells[y][x].Rune != ' ' {
				t.Fatalf("cell (%d,%d) not erased", x, y)
			}
		}
	}
}

func TestScreenResizePreservesCellsAndClampsCursor(t *testing.T) {
	s := NewScreen(3, 2)
	for _, r := range "abcdef" {
		s.Put(r)
	}
	s.Resize(4, 3)
	if s.Width != 4 || s.Height != 3 {
		t.Fatalf("size = %dx%d, want 4x3", s.Width, s.Height)
	}
	if got := string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune, s.Cells[0][2].Rune}); got != "abc" {
		t.Fatalf("preserved row = %q, want abc", got)
	}
	if s.Cells[2][3].Rune != ' ' {
		t.Fatal("new cells must be blank")
	}
	s.Resize(2, 1)
	if s.Width != 2 || s.Height != 1 || s.X != 1 || s.Y != 0 {
		t.Fatalf("shrunk size/cursor = %dx%d (%d,%d)", s.Width, s.Height, s.X, s.Y)
	}
	if got := string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune}); got != "ab" {
		t.Fatalf("shrunk row = %q, want ab", got)
	}
}

func TestScreenMoveClampsCoordinates(t *testing.T) {
	s := NewScreen(5, 3)
	s.Move(-10, 99)
	if s.X != 0 || s.Y != 2 {
		t.Fatalf("cursor = (%d,%d), want (0,2)", s.X, s.Y)
	}
	s.Move(99, -10)
	if s.X != 4 || s.Y != 0 {
		t.Fatalf("cursor = (%d,%d), want (4,0)", s.X, s.Y)
	}
}

func TestScreenWideRuneOccupiesTwoCells(t *testing.T) {
	s := NewScreen(8, 2)
	for _, r := range "目录:" {
		s.Put(r)
	}
	if s.X != 5 {
		t.Fatalf("cursor X = %d, want 5", s.X)
	}
	cells := s.Cells[0][:5]
	if cells[0].Rune != '目' || cells[1].Rune != 0 || cells[2].Rune != '录' || cells[3].Rune != 0 || cells[4].Rune != ':' {
		t.Fatalf("wide cells = %+v", cells)
	}
}

func TestScreenWideRuneWrapsWhenNoRoom(t *testing.T) {
	s := NewScreen(4, 2)
	for _, r := range "abc目" {
		s.Put(r)
	}
	if s.X != 2 || s.Y != 1 {
		t.Fatalf("cursor = (%d,%d), want (2,1)", s.X, s.Y)
	}
	if s.Cells[1][0].Rune != '目' || s.Cells[1][1].Rune != 0 {
		t.Fatalf("wrapped wide cells = %+v", s.Cells[1][:2])
	}
}

func TestScreenOverwriteClearsWideHalf(t *testing.T) {
	s := NewScreen(4, 2)
	s.Put('目')
	s.Move(1, 0)
	s.Put('X')
	if s.Cells[0][0].Rune != ' ' || s.Cells[0][1].Rune != 'X' {
		t.Fatalf("cells = %+v, want space then X", s.Cells[0][:2])
	}
}

func TestScreenEraseClearsWideHalves(t *testing.T) {
	s := NewScreen(8, 1)
	for _, r := range "目录x" {
		s.Put(r)
	}
	s.Move(1, 0) // second half of 目
	s.eraseLineFromCursor()
	if s.Cells[0][0].Rune != ' ' || s.Cells[0][1].Rune != ' ' {
		t.Fatalf("erase from continuation left %+v", s.Cells[0][:2])
	}

	s = NewScreen(8, 1)
	for _, r := range "目录x" {
		s.Put(r)
	}
	s.Move(0, 0)
	s.eraseLineToCursor() // erases only the first half of 目
	if s.Cells[0][0].Rune != ' ' || s.Cells[0][1].Rune != ' ' {
		t.Fatalf("erase to wide first half left %+v", s.Cells[0][:2])
	}
}

func TestScreenScrollbackHistory(t *testing.T) {
	s := NewScreen(4, 2)
	s.MaxHistory = 5
	for _, r := range "12345678abcdefgh" {
		s.Put(r)
	}
	// "1234" row 0 -> pushed to History[0]
	// "5678" row 1 -> pushed to History[1]
	// "abcd" is row 0 on screen
	// "efgh" is row 1 on screen
	if len(s.History) != 2 {
		t.Fatalf("history length = %d, want 2", len(s.History))
	}
	if got := string([]rune{s.History[0][0].Rune, s.History[0][1].Rune, s.History[0][2].Rune, s.History[0][3].Rune}); got != "1234" {
		t.Fatalf("history[0] = %q, want 1234", got)
	}
	if got := string([]rune{s.History[1][0].Rune, s.History[1][1].Rune, s.History[1][2].Rune, s.History[1][3].Rune}); got != "5678" {
		t.Fatalf("history[1] = %q, want 5678", got)
	}
	if got := string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune, s.Cells[0][2].Rune, s.Cells[0][3].Rune}); got != "abcd" {
		t.Fatalf("live row 0 = %q, want abcd", got)
	}

	// Test TotalLines and LineAt
	if s.TotalLines() != 4 {
		t.Fatalf("TotalLines = %d, want 4", s.TotalLines())
	}
	line0 := s.LineAt(0)
	if string([]rune{line0[0].Rune, line0[1].Rune}) != "12" {
		t.Fatalf("LineAt(0) = %q, want 12", string([]rune{line0[0].Rune, line0[1].Rune}))
	}
	line2 := s.LineAt(2) // row 0 of screen
	if string([]rune{line2[0].Rune, line2[1].Rune}) != "ab" {
		t.Fatalf("LineAt(2) = %q, want ab", string([]rune{line2[0].Rune, line2[1].Rune}))
	}
}

func TestScreenAlternateBuffer(t *testing.T) {
	s := NewScreen(4, 2)
	for _, r := range "12345678" {
		s.Put(r)
	}
	s.EnterAltScreen()
	if !s.IsAlt() {
		t.Fatal("expected IsAlt to be true")
	}
	for _, r := range "abcd" {
		s.Put(r)
	}
	if s.Cells[0][0].Rune != 'a' {
		t.Fatalf("alt screen cell(0,0) = %c, want a", s.Cells[0][0].Rune)
	}
	s.ExitAltScreen()
	if s.IsAlt() {
		t.Fatal("expected IsAlt to be false")
	}
	if s.Cells[0][0].Rune != '1' {
		t.Fatalf("restored primary cell(0,0) = %c, want 1", s.Cells[0][0].Rune)
	}
}
