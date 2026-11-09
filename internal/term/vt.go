package term

import "unicode/utf8"

const (
	stateGround uint8 = iota
	stateEscape
	stateCSI
	stateOSC
	stateOSCEscape
)

const trueColorFlag uint32 = 1 << 24

// Parser incrementally consumes UTF-8 and ANSI/VT sequences. Its state is
// retained across Feed calls so sequences may be split across pipe reads.
type Parser struct {
	screen         *Screen
	state          uint8
	params         []int
	value          int
	haveValue      bool
	private        bool
	text           []byte
	bold           bool
	savedX, savedY int
}

func NewParser(s *Screen) *Parser { return &Parser{screen: s} }

func (p *Parser) Feed(data []byte) {
	for _, b := range data {
		p.feedByte(b)
	}
}

func (p *Parser) flushText(final bool) {
	for len(p.text) > 0 {
		if !utf8.FullRune(p.text) {
			if !final {
				return
			}
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
	switch p.state {
	case stateGround:
		if b == 0x1b {
			p.flushText(true)
			p.state = stateEscape
			return
		}
		if b >= 0x80 {
			p.text = append(p.text, b)
			p.flushText(false)
			return
		}
		p.flushText(true)
		p.screen.Put(rune(b))
	case stateEscape:
		switch b {
		case '[':
			p.state, p.params, p.value, p.haveValue, p.private = stateCSI, p.params[:0], 0, false, false
		case ']':
			p.state = stateOSC
		case '7':
			p.savedX, p.savedY = p.screen.X, p.screen.Y
			p.state = stateGround
		case '8':
			p.screen.Move(p.savedX, p.savedY)
			p.state = stateGround
		case 'D':
			p.screen.Put('\n')
			p.state = stateGround // IND
		case 'E':
			p.screen.Put('\r')
			p.screen.Put('\n')
			p.state = stateGround // NEL
		case 'M':
			p.screen.Move(p.screen.X, p.screen.Y-1)
			p.state = stateGround // RI
		case '\\':
			p.state = stateGround
		default:
			p.state = stateGround
		}
	case stateCSI:
		switch {
		case b >= '0' && b <= '9':
			if p.value < 1000000 {
				p.value = p.value*10 + int(b-'0')
			}
			p.haveValue = true
		case b == ';':
			p.pushParam()
		case b == '?' && len(p.params) == 0 && !p.haveValue:
			p.private = true
		case b >= 0x40 && b <= 0x7e:
			p.pushParam()
			if !p.private {
				p.execute(b)
			} else {
				p.executePrivate(b)
			}
			p.state = stateGround
		case b == 0x1b:
			p.state = stateEscape
		default: // Ignore unsupported CSI intermediates.
		}
	case stateOSC:
		if b == 0x07 {
			p.state = stateGround
		} else if b == 0x1b {
			p.state = stateOSCEscape
		}
	case stateOSCEscape:
		if b == '\\' {
			p.state = stateGround
		} else if b != 0x1b {
			p.state = stateOSC
		}
	}
}

func (p *Parser) pushParam() {
	if p.haveValue {
		p.params = append(p.params, p.value)
	} else {
		p.params = append(p.params, 0)
	}
	p.value, p.haveValue = 0, false
}

func (p *Parser) param(i, fallback int) int {
	if i >= len(p.params) || p.params[i] == 0 {
		return fallback
	}
	return p.params[i]
}

func (p *Parser) execute(final byte) {
	s := p.screen
	switch final {
	case 'A':
		s.Move(s.X, s.Y-p.param(0, 1))
	case 'B':
		s.Move(s.X, s.Y+p.param(0, 1))
	case 'C':
		s.Move(s.X+p.param(0, 1), s.Y)
	case 'D':
		s.Move(s.X-p.param(0, 1), s.Y)
	case 'E':
		s.Move(0, s.Y+p.param(0, 1))
	case 'F':
		s.Move(0, s.Y-p.param(0, 1))
	case 'G', '`':
		s.Move(p.param(0, 1)-1, s.Y)
	case 'd':
		s.Move(s.X, p.param(0, 1)-1)
	case 'H', 'f':
		s.Move(p.param(1, 1)-1, p.param(0, 1)-1)
	case 'J':
		switch p.param(0, 0) {
		case 0:
			s.eraseDisplayFromCursor()
		case 1:
			s.eraseDisplayToCursor()
		case 2, 3:
			s.eraseDisplay()
		}
	case 'K':
		switch p.param(0, 0) {
		case 0:
			s.eraseLineFromCursor()
		case 1:
			s.eraseLineToCursor()
		case 2:
			s.eraseLine()
		}
	case 'm':
		p.sgr()
	case 's':
		p.savedX, p.savedY = s.X, s.Y
	case 'u':
		s.Move(p.savedX, p.savedY)
	}
}

func (p *Parser) executePrivate(final byte) {
	switch final {
	case 'h': // DECSET
		for _, param := range p.params {
			if param == 25 {
				p.screen.CursorVisible = true
			}
		}
	case 'l': // DECRST
		for _, param := range p.params {
			if param == 25 {
				p.screen.CursorVisible = false
			}
		}
	}
}

func (p *Parser) sgr() {
	if len(p.params) == 0 {
		p.params = []int{0}
	}
	for i := 0; i < len(p.params); i++ {
		n := p.params[i]
		switch {
		case n == 0:
			p.screen.FG, p.screen.BG, p.bold = 7, 0, false
			p.screen.Faint = false
		case n == 1:
			p.bold = true
			if p.screen.FG < 8 {
				p.screen.FG += 8
			}
		case n == 2:
			p.screen.Faint = true
		case n == 22:
			p.bold = false
			p.screen.Faint = false
			if p.screen.FG >= 8 && p.screen.FG < 16 {
				p.screen.FG -= 8
			}
		case n == 39:
			p.screen.FG = 7
		case n == 49:
			p.screen.BG = 0
		case n >= 30 && n <= 37:
			p.screen.FG = uint32(n - 30)
			if p.bold {
				p.screen.FG += 8
			}
		case n >= 40 && n <= 47:
			p.screen.BG = uint32(n - 40)
		case n >= 90 && n <= 97:
			p.screen.FG = uint32(n - 90 + 8)
		case n >= 100 && n <= 107:
			p.screen.BG = uint32(n - 100 + 8)
		case (n == 38 || n == 48) && i+1 < len(p.params):
			color, next, ok := parseExtendedColor(p.params, i+1)
			if ok {
				if n == 38 {
					p.screen.FG = color
				} else {
					p.screen.BG = color
				}
				i = next
			}
		}
	}
}

func parseExtendedColor(params []int, i int) (uint32, int, bool) {
	if i >= len(params) {
		return 0, i, false
	}
	switch params[i] {
	case 5:
		if i+1 >= len(params) {
			return 0, i, false
		}
		n := params[i+1]
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return uint32(n), i + 1, true
	case 2:
		if i+3 >= len(params) {
			return 0, i, false
		}
		r, g, b := clampColor(params[i+1]), clampColor(params[i+2]), clampColor(params[i+3])
		return trueColorFlag | uint32(r<<16|g<<8|b), i + 3, true
	}
	return 0, i, false
}

func clampColor(n int) int {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}
