package term

// Parser handles a practical subset of ANSI/VT escape sequences.
type Parser struct { screen *Screen; state uint8; params []int; value int; haveValue bool }
func NewParser(s *Screen) *Parser { return &Parser{screen:s} }
func (p *Parser) Feed(data []byte) { for _,b:=range data { p.feedByte(b) } }
func (p *Parser) feedByte(b byte) {
 switch p.state {
 case 0: if b==0x1b { p.state=1; return }; p.screen.Put(rune(b))
 case 1: if b=='[' { p.state=2; p.params=nil; p.value=0; p.haveValue=false } else { p.state=0 }
 case 2: if b>='0'&&b<='9' { p.value=p.value*10+int(b-'0'); p.haveValue=true; return }; if b==';' { p.pushParam(); return }; p.pushParam(); p.execute(b); p.state=0
 }
}
func (p *Parser) pushParam() { if p.haveValue { p.params=append(p.params,p.value) } else { p.params=append(p.params,0) }; p.value=0; p.haveValue=false }
func (p *Parser) param(i,fallback int) int { if i>=len(p.params)||p.params[i]==0 { return fallback }; return p.params[i] }
func (p *Parser) execute(final byte) { s:=p.screen; switch final {
 case 'H','f': s.Move(p.param(1,1)-1,p.param(0,1)-1)
 case 'J': if p.param(0,0)==2 || p.param(0,0)==0 { s.Clear() }
 case 'K': for x:=s.X;x<s.Width;x++ { s.Cells[s.Y][x]=Cell{Rune:' ',FG:s.FG,BG:s.BG} }
 case 'm': for _,n:=range p.params { if n==0 { s.FG=7;s.BG=0 } else if n>=30&&n<=37 { s.FG=uint8(n-30) } else if n>=40&&n<=47 { s.BG=uint8(n-40) } else if n>=90&&n<=97 { s.FG=uint8(n-90+8) } }
 } }
