package api

import "testing"

// Оттенок совпадает с превью в окне правки (frontend: lib/placeholder.js,
// test/placeholder.test.js — те же значения).
func TestPlaceholderHue(t *testing.T) {
	for id, want := range map[string]float64{"k123": 200, "k7": 36} {
		if got := placeholderHue(id); got != want {
			t.Errorf("placeholderHue(%q) = %v, want %v", id, got, want)
		}
	}
	a, _ := placeholderCover("k1")
	b, _ := placeholderCover("k2")
	if len(a) < 1000 || string(a) == string(b) {
		t.Errorf("placeholders: %d bytes, same for different releases: %v", len(a), string(a) == string(b))
	}
}
