package text

import "testing"

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"fits", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"ansi-free truncation", "hello world", 8, "hello w…"},
		{"narrow width", "hello", 1, "…"},
		{"zero width", "hello", 0, "…"},
		{"empty string", "", 5, ""},
		{"wide runes fit", "中文", 4, "中文"},
		{"wide runes truncate", "中文测试", 3, "中…"},
		{"wide rune does not split", "中文测试", 4, "中…"},
		{"styled", "\x1b[1mhello world\x1b[0m", 8, "\x1b[1mhello w…\x1b[0m"},
		{"single wide rune narrow width", "中", 1, "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Truncate(tc.s, tc.w); got != tc.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
			}
		})
	}
}
