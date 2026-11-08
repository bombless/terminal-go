package term

import "testing"

func TestColorRefANSIPalette(t *testing.T) {
	tests := []struct{ index, want uint32 }{
		{0, 0x00000000}, {1, 0x000000CD}, {7, 0x00E5E5E5},
		{15, 0x00FFFFFF}, {16, 0x00000000}, {196, 0x000000FF},
		{231, 0x00FFFFFF}, {232, 0x00080808}, {255, 0x00EEEEEE},
	}
	for _, tt := range tests {
		if got := ColorRef(tt.index); got != tt.want {
			t.Errorf("ColorRef(%d) = %#08x, want %#08x", tt.index, got, tt.want)
		}
	}
}

func TestColorRefTaggedRGB(t *testing.T) {
	tests := []struct {
		name        string
		color, want uint32
	}{
		{"selection background", 0x01A56E2B, 0x002B6EA5},
		{"selection foreground", 0x01FFFFFF, 0x00FFFFFF},
		{"truecolor", 0x01010203, 0x00030201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ColorRef(tt.color); got != tt.want {
				t.Errorf("ColorRef(%#08x) = %#08x, want %#08x", tt.color, got, tt.want)
			}
		})
	}
}
