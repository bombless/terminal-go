package term

import "testing"

func TestScreenWrapAndScroll(t *testing.T) {
	s := NewScreen(3, 2)
	for _, r := range "abcdefg" { s.Put(r) }
	if got := string([]rune{s.Cells[0][0].Rune, s.Cells[0][1].Rune, s.Cells[0][2].Rune}); got != "def" { t.Fatalf("top row = %q, want def", got) }
	if got := string([]rune{s.Cells[1][0].Rune, s.Cells[1][1].Rune, s.Cells[1][2].Rune}); got != "g  " { t.Fatalf("bottom row = %q, want g  ", got) }
}

func TestScreenEraseLineAndDisplay(t *testing.T) {
	s := NewScreen(4, 2)
	for _, r := range "abcdefgh" { s.Put(r) }
	s.Move(2, 0)
	s.eraseLineFromCursor()
	if s.Cells[0][0].Rune != 'a' || s.Cells[0][1].Rune != 'b' || s.Cells[0][2].Rune != ' ' { t.Fatal("eraseLineFromCursor did not preserve prefix and clear suffix") }
	s.Move(1, 0)
	s.eraseDisplayFromCursor()
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			if y == 0 && x == 0 { if s.Cells[y][x].Rune != 'a' { t.Fatal("erase changed cell before cursor") }; continue }
			if s.Cells[y][x].Rune != ' ' { t.Fatalf("cell (%d,%d) not erased", x, y) }
		}
	}
}

func TestScreenMoveClampsCoordinates(t *testing.T) {
	s := NewScreen(5, 3)
	s.Move(-10, 99)
	if s.X != 0 || s.Y != 2 { t.Fatalf("cursor = (%d,%d), want (0,2)", s.X, s.Y) }
	s.Move(99, -10)
	if s.X != 4 || s.Y != 0 { t.Fatalf("cursor = (%d,%d), want (4,0)", s.X, s.Y) }
}
