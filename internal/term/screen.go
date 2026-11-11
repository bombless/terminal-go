package term

// Cell is one terminal character cell.
type Cell struct {
	Rune   rune
	FG, BG uint32
	Faint  bool
}

// Screen stores a fixed-size terminal grid with scrollback history.
type Screen struct {
	Width, Height int
	Cells         [][]Cell
	History       [][]Cell
	MaxHistory    int
	X, Y          int
	FG, BG        uint32
	Faint         bool
	CursorVisible bool

	isAlt      bool
	altCells   [][]Cell
	altX, altY int
}

func NewScreen(width, height int) *Screen {
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 25
	}
	s := &Screen{
		Width:         width,
		Height:        height,
		FG:            7,
		BG:            0,
		CursorVisible: true,
		MaxHistory:    10000,
	}
	s.Cells = make([][]Cell, height)
	for y := range s.Cells {
		s.Cells[y] = make([]Cell, width)
		for x := range s.Cells[y] {
			s.Cells[y][x] = s.blankCell()
		}
	}
	return s
}
func (s *Screen) BlankCell() Cell {
	return Cell{Rune: ' ', FG: s.FG, BG: s.BG, Faint: s.Faint}
}
func (s *Screen) blankCell() Cell {
	return s.BlankCell()
}
func (s *Screen) Put(r rune) {
	switch r {
	case '\r':
		s.X = 0
		return
	case '\n':
		s.Y++
		if s.Y >= s.Height {
			s.scroll()
			s.Y = s.Height - 1
		}
		return
	case '\b':
		if s.X > 0 {
			s.X--
		}
		return
	case '\t':
		s.X = (s.X/8 + 1) * 8
		if s.X >= s.Width {
			s.X = s.Width - 1
		}
		return
	}
	if r < 32 {
		return
	}
	width := RuneWidth(r)
	if width > s.Width {
		width = 1
	}
	if s.X+width > s.Width {
		s.X = 0
		s.Y++
		if s.Y >= s.Height {
			s.scroll()
			s.Y = s.Height - 1
		}
	}
	s.clearWideAt(s.Y, s.X)
	if width == 2 {
		s.clearWideAt(s.Y, s.X+1)
	}
	s.Cells[s.Y][s.X] = Cell{Rune: r, FG: s.FG, BG: s.BG, Faint: s.Faint}
	if width == 2 {
		s.Cells[s.Y][s.X+1] = Cell{Rune: 0, FG: s.FG, BG: s.BG, Faint: s.Faint}
	}
	s.X += width
}
func (s *Screen) Clear() { s.eraseDisplay(); s.X, s.Y = 0, 0 }
func (s *Screen) Move(x, y int) {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= s.Width {
		x = s.Width - 1
	}
	if y >= s.Height {
		y = s.Height - 1
	}
	s.X, s.Y = x, y
}

// clearWideAt removes any half of a wide glyph covering (x,y) so overwriting
// one cell cannot leave a dangling half behind.
func (s *Screen) clearWideAt(y, x int) {
	if x < 0 || x >= s.Width {
		return
	}
	if s.Cells[y][x].Rune == 0 && x > 0 && RuneWidth(s.Cells[y][x-1].Rune) == 2 {
		s.Cells[y][x-1] = s.blankCell()
	}
	if RuneWidth(s.Cells[y][x].Rune) == 2 && x+1 < s.Width {
		s.Cells[y][x+1] = s.blankCell()
	}
}

// Resize changes the terminal grid while preserving overlapping cells.
func (s *Screen) Resize(width, height int) {
	if width < 1 || height < 1 {
		return
	}
	cells := make([][]Cell, height)
	for y := 0; y < height; y++ {
		cells[y] = make([]Cell, width)
		for x := range cells[y] {
			cells[y][x] = s.blankCell()
		}
		if y < s.Height {
			copyWidth := width
			if copyWidth > s.Width {
				copyWidth = s.Width
			}
			copy(cells[y], s.Cells[y][:copyWidth])
		}
	}
	s.Width, s.Height, s.Cells = width, height, cells
	if s.altCells != nil {
		altCells := make([][]Cell, height)
		for y := 0; y < height; y++ {
			altCells[y] = make([]Cell, width)
			for x := range altCells[y] {
				altCells[y][x] = s.blankCell()
			}
			if y < len(s.altCells) {
				copyWidth := width
				if copyWidth > len(s.altCells[y]) {
					copyWidth = len(s.altCells[y])
				}
				copy(altCells[y], s.altCells[y][:copyWidth])
			}
		}
		s.altCells = altCells
	}
	s.Move(s.X, s.Y)
}

func (s *Screen) TotalLines() int {
	return len(s.History) + s.Height
}

func (s *Screen) LineAt(absY int) []Cell {
	h := len(s.History)
	if absY < 0 {
		return nil
	}
	if absY < h {
		return s.History[absY]
	}
	cellY := absY - h
	if cellY < s.Height {
		return s.Cells[cellY]
	}
	return nil
}

func (s *Screen) CellAt(x, absY int) Cell {
	line := s.LineAt(absY)
	if line == nil || x < 0 || x >= len(line) {
		return s.blankCell()
	}
	return line[x]
}

func (s *Screen) ClearHistory() {
	s.History = nil
}

func (s *Screen) IsAlt() bool {
	return s.isAlt
}

func (s *Screen) EnterAltScreen() {
	if s.isAlt {
		return
	}
	s.isAlt = true
	s.altX, s.altY = s.X, s.Y
	s.altCells = make([][]Cell, s.Height)
	for y := range s.altCells {
		s.altCells[y] = make([]Cell, s.Width)
		for x := range s.altCells[y] {
			s.altCells[y][x] = s.blankCell()
		}
	}
	s.Cells, s.altCells = s.altCells, s.Cells
	s.X, s.Y = 0, 0
}

func (s *Screen) ExitAltScreen() {
	if !s.isAlt {
		return
	}
	s.isAlt = false
	s.Cells, s.altCells = s.altCells, s.Cells
	s.X, s.Y = s.altX, s.altY
	s.altCells = nil
}

func (s *Screen) ScrollUp(n int) {
	if n <= 0 {
		n = 1
	}
	for i := 0; i < n; i++ {
		s.scroll()
	}
}

func (s *Screen) ScrollDown(n int) {
	if n <= 0 {
		n = 1
	}
	if n > s.Height {
		n = s.Height
	}
	for y := s.Height - 1; y >= n; y-- {
		s.Cells[y] = s.Cells[y-n]
	}
	for y := 0; y < n; y++ {
		s.Cells[y] = make([]Cell, s.Width)
		for x := range s.Cells[y] {
			s.Cells[y][x] = s.blankCell()
		}
	}
}

func (s *Screen) InsertLines(n int) {
	if n <= 0 {
		n = 1
	}
	if s.Y < 0 || s.Y >= s.Height {
		return
	}
	for y := s.Height - 1; y >= s.Y+n; y-- {
		s.Cells[y] = s.Cells[y-n]
	}
	for y := s.Y; y < s.Y+n && y < s.Height; y++ {
		s.Cells[y] = make([]Cell, s.Width)
		for x := range s.Cells[y] {
			s.Cells[y][x] = s.blankCell()
		}
	}
}

func (s *Screen) DeleteLines(n int) {
	if n <= 0 {
		n = 1
	}
	if s.Y < 0 || s.Y >= s.Height {
		return
	}
	for y := s.Y; y < s.Height-n; y++ {
		s.Cells[y] = s.Cells[y+n]
	}
	for y := s.Height - n; y < s.Height; y++ {
		if y >= s.Y {
			s.Cells[y] = make([]Cell, s.Width)
			for x := range s.Cells[y] {
				s.Cells[y][x] = s.blankCell()
			}
		}
	}
}

func (s *Screen) ReverseIndex() {
	if s.Y > 0 {
		s.Y--
	} else {
		s.ScrollDown(1)
	}
}

func (s *Screen) eraseRow(y, first, last int) {
	if y < 0 || y >= s.Height {
		return
	}
	if first < 0 {
		first = 0
	}
	if last >= s.Width {
		last = s.Width - 1
	}
	if first > last {
		return
	}
	// Do not leave half of a wide glyph outside the erased range.
	if first > 0 && RuneWidth(s.Cells[y][first-1].Rune) == 2 {
		s.Cells[y][first-1] = s.blankCell()
	}
	if last+1 < s.Width && s.Cells[y][last+1].Rune == 0 && RuneWidth(s.Cells[y][last].Rune) == 2 {
		s.Cells[y][last+1] = s.blankCell()
	}
	for x := first; x <= last; x++ {
		s.Cells[y][x] = s.blankCell()
	}
}
func (s *Screen) eraseLine()           { s.eraseRow(s.Y, 0, s.Width-1) }
func (s *Screen) eraseLineFromCursor() { s.eraseRow(s.Y, s.X, s.Width-1) }
func (s *Screen) eraseLineToCursor()   { s.eraseRow(s.Y, 0, s.X) }
func (s *Screen) eraseDisplay() {
	for y := 0; y < s.Height; y++ {
		s.eraseRow(y, 0, s.Width-1)
	}
}
func (s *Screen) eraseDisplayFromCursor() {
	s.eraseRow(s.Y, s.X, s.Width-1)
	for y := s.Y + 1; y < s.Height; y++ {
		s.eraseRow(y, 0, s.Width-1)
	}
}
func (s *Screen) eraseDisplayToCursor() {
	for y := 0; y < s.Y; y++ {
		s.eraseRow(y, 0, s.Width-1)
	}
	s.eraseRow(s.Y, 0, s.X)
}
func (s *Screen) scroll() {
	if !s.isAlt {
		saved := make([]Cell, s.Width)
		copy(saved, s.Cells[0])
		s.History = append(s.History, saved)
		if s.MaxHistory > 0 && len(s.History) > s.MaxHistory {
			s.History = s.History[len(s.History)-s.MaxHistory:]
		}
	}
	copy(s.Cells, s.Cells[1:])
	row := make([]Cell, s.Width)
	for x := range row {
		row[x] = s.blankCell()
	}
	s.Cells[s.Height-1] = row
}
