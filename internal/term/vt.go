package term

import "unicode/utf8"

// Parser handles a practical subset of ANSI/VT escape sequences. Parser state
// is retained across Feed calls, including incomplete UTF-8 and CSI sequences.
type Parser struct {
	screen *Screen
	state uint8
	params []int
	value int
	haveValue bool
	text []byte
}

func NewParser(s *Screen) *Parser { return &Parser{screen:s} }

func (p *Parser) Feed(data []byte) {
	for _, b := range data { p.feedByte(b) }
}

func (p *Parser) flushText(final bool) {
	for len(p.text) > 0 {
		if !utf8.FullRune(p.text) {
			if !final { return }
			p.screen.Put(utf8.RuneError)
			p.text = p.text[1:]
			continue
		}
		r, size := utf8.DecodeRune(p.text)
		p.screen.Put(r)
		p.text = p.text[size:]
	}
}

func (p *Parser) feedByte(b byte) {
	if p.state == 0 {
		if b == 0x1b {
			p.flushText(true)
			p.state = 1
			return
		}
		if b >= 0x80 {
			p.text = append(p.text, b)
			p.flushText(false)
			return
		}
		p.flushText(true)
		p.screen.Put(rune(b))
		return
	}

	switch p.state {
	case 1:
		if b == '[' {
			p.state = 2
			p.params = p.params[:0]
			p.value = 0
			p.haveValue = false
		} else {
			// Unsupported two-byte escape: consume it and return to ground.
			p.state = 0
		}
	case 2:
		switch {
		case b >= '0' && b <= '9':
			if p.value < 1000000 {
				p.value = p.value*10 + int(b-'0')
			}
			p.haveValue = true
		case b == ';':
			p.pushParam()
		case b >= 0x40 && b <= 0x7e:
			p.pushParam()
			p.execute(b)
			p.state = 0
		default:
			// Ignore unsupported intermediate/private bytes without losing
			// the parser's ability to recover at the final CSI byte.
		}
	}
}

func (p *Parser) pushParam() {
	if p.haveValue { p.params = append(p.params, p.value) } else { p.params = append(p.params, 0) }
	p.value, p.haveValue = 0, false
}

func (p *Parser) param(i, fallback int) int {
	if i >= len(p.params) || p.params[i] == 0 { return fallback }
	return p.params[i]
}

func (p *Parser) execute(final byte) {
	s := p.screen
	switch final {
	case 'A': s.Move(s.X, s.Y-p.param(0, 1))
	case 'B': s.Move(s.X, s.Y+p.param(0, 1))
	case 'C': s.Move(s.X+p.param(0, 1), s.Y)
	case 'D': s.Move(s.X-p.param(0, 1), s.Y)
	case 'H', 'f': s.Move(p.param(1, 1)-1, p.param(0, 1)-1)
	case 'J':
		switch p.param(0, 0) {
		case 0: s.eraseDisplayFromCursor()
		case 1: s.eraseDisplayToCursor()
		case 2, 3: s.Clear()
		}
	case 'K':
		switch p.param(0, 0) {
		case 0: s.eraseLineFromCursor()
		case 1: s.eraseLineToCursor()
		case 2: s.eraseLine()
		}
	case 'm':
		if len(p.params) == 0 { s.FG, s.BG = 7, 0 }
		for _, n := range p.params {
			switch {
			case n == 0: s.FG, s.BG = 7, 0
			case n == 39: s.FG = 7
			case n == 49: s.BG = 0
			case n >= 30 && n <= 37: s.FG = uint8(n-30)
			case n >= 40 && n <= 47: s.BG = uint8(n-40)
			case n >= 90 && n <= 97: s.FG = uint8(n-90+8)
			case n >= 100 && n <= 107: s.BG = uint8(n-100+8)
			}
		}
	}
}
