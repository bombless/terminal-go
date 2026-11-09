package term

import "testing"

func TestRuneWidth(t *testing.T) {
	tests := []struct {
		r    rune
		want int
	}{
		{'A', 1},
		{':', 1},
		{'─', 1}, // box drawing stays narrow
		{'目', 2},
		{'录', 2},
		{'。', 2},
		{'あ', 2},
		{'한', 2},
		{'Ａ', 2}, // fullwidth latin
		{'ｱ', 1}, // halfwidth katakana
	}
	for _, tt := range tests {
		if got := RuneWidth(tt.r); got != tt.want {
			t.Errorf("RuneWidth(%q) = %d, want %d", tt.r, got, tt.want)
		}
	}
}
