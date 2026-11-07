package term

// Cell is one terminal character cell.
type Cell struct { Rune rune; FG, BG uint8 }

// Screen stores a fixed-size terminal grid.
type Screen struct { Width, Height int; Cells [][]Cell; X, Y int; FG, BG uint8 }

func NewScreen(width, height int) *Screen {
 if width < 1 { width = 80 }; if height < 1 { height = 25 }
 s := &Screen{Width:width, Height:height, FG:7, BG:0}; s.Cells=make([][]Cell,height)
 for y:=range s.Cells { s.Cells[y]=make([]Cell,width); for x:=range s.Cells[y] { s.Cells[y][x]=Cell{Rune:' ',FG:7} } }; return s
}
func (s *Screen) Put(r rune) {
 switch r { case '\r': s.X=0; return; case '\n': s.Y++; if s.Y>=s.Height { s.scroll(); s.Y=s.Height-1 }; return; case '\b': if s.X>0 { s.X-- }; return; case '\t': s.X=(s.X/8+1)*8; if s.X>=s.Width { s.X=s.Width-1 }; return }
 if r<32 { return }; if s.X>=s.Width { s.X=0; s.Y++; if s.Y>=s.Height { s.scroll(); s.Y=s.Height-1 } }
 s.Cells[s.Y][s.X]=Cell{Rune:r,FG:s.FG,BG:s.BG}; s.X++
}
func (s *Screen) Clear() { for y:=range s.Cells { for x:=range s.Cells[y] { s.Cells[y][x]=Cell{Rune:' ',FG:s.FG,BG:s.BG} } }; s.X,s.Y=0,0 }
func (s *Screen) Move(x,y int) { if x<0{x=0}; if y<0{y=0}; if x>=s.Width{x=s.Width-1}; if y>=s.Height{y=s.Height-1}; s.X,s.Y=x,y }
func (s *Screen) scroll() { copy(s.Cells,s.Cells[1:]); row:=make([]Cell,s.Width); for x:=range row { row[x]=Cell{Rune:' ',FG:s.FG,BG:s.BG} }; s.Cells[s.Height-1]=row }
