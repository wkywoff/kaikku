package ingest

import "testing"

// Вывод ffmpeg -af ebur128=peak=sample:framelog=quiet (ffmpeg 7.1), урезан.
const ebur128Output = `size=N/A time=00:01:39.32 bitrate=N/A speed= 199x
[Parsed_ebur128_0 @ 0x7f46b0001540] Summary:

  Integrated loudness:
    I:          -6.1 LUFS
    Threshold: -16.4 LUFS

  Loudness range:
    LRA:         3.5 LU
    Threshold: -26.3 LUFS
    LRA low:    -8.7 LUFS
    LRA high:   -5.1 LUFS

  Sample peak:
    Peak:        1.9 dBFS
[out#0/null @ 0x55fe6d657f00] video:0KiB audio:35518KiB`

func TestParseEBUR128(t *testing.T) {
	lufs, peak, err := parseEBUR128(ebur128Output)
	if err != nil {
		t.Fatal(err)
	}
	if lufs != -6.1 || peak != 1.9 {
		t.Errorf("got I=%v peak=%v, want -6.1 / 1.9", lufs, peak)
	}
	if _, _, err := parseEBUR128("no summary here"); err == nil {
		t.Error("expected an error without a summary")
	}
}

func TestPlaybackGain(t *testing.T) {
	cases := []struct {
		name             string
		lufs, peak, want float64
	}{
		{"loud master is turned down to -14", -6.1, 1.9, -7.9},
		{"quiet track is raised", -20, -9, 6},
		{"raise limited by the peak", -20, -3, 2},
		{"silence doesn't explode", -70, -70, 12},
		{"quiet master with full-scale peaks is left alone", -14.5, -0.3, 0},
	}
	for _, c := range cases {
		if got := playbackGain(c.lufs, c.peak); got != c.want {
			t.Errorf("%s: playbackGain(%v, %v) = %v, want %v", c.name, c.lufs, c.peak, got, c.want)
		}
	}
}
