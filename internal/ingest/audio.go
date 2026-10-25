package ingest

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Громкость не «запекается» в файл: исходный AAC с YouTube просто
// перепаковывается, а поправка до targetLUFS хранится в tracks.gain_db и
// применяется плеером через GainNode. Перекодирование с loudnorm занимало
// ~35 с на трек на 1 vCPU (фильтр внутри передискретизирует в 192 кГц),
// перепаковка + замер — ~1.5 с, и без повторного сжатия с потерями.
const (
	targetLUFS = -14.0
	maxPeakDB  = -1.0 // поправка не поднимает пик выше этого — без клиппинга
	minGainDB  = -30.0
	maxGainDB  = 12.0
	// Битрейт, если YouTube не отдал AAC (формат 140) и пришлось кодировать.
	fallbackBitrate = "192k"
)

// PrepareAudio writes srcPath's audio to dstPath as AAC in .m4a — remuxed
// as-is when it already is AAC, encoded otherwise — and returns the
// measured integrated loudness and the gain to apply on playback.
//
// The output goes to a temporary file renamed into place on success, so a
// killed or failed ffmpeg never leaves a truncated .m4a in storage.
func PrepareAudio(ctx context.Context, srcPath, dstPath string) (lufs, gainDB float64, err error) {
	codec, err := probeCodec(ctx, srcPath)
	if err != nil {
		return 0, 0, err
	}
	lufs, peak, err := measureLoudness(ctx, srcPath)
	if err != nil {
		return 0, 0, err
	}

	args := []string{"-nostdin", "-y", "-v", "error", "-i", srcPath, "-map", "0:a:0", "-vn"}
	if codec == "aac" {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", fallbackBitrate)
	}
	clipped := codec != "aac" && peak > 0
	if clipped {
		args = append(args, "-af", clipOversFilter)
	}
	tmpPath := dstPath + ".part"
	defer os.Remove(tmpPath) // no-op после успешного rename
	// -f ipod — то, что ffmpeg выбирает для .m4a; по расширению .part сам не выведет.
	args = append(args, "-movflags", "+faststart", "-f", "ipod", tmpPath)
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return 0, 0, fmt.Errorf("ffmpeg: %w (output: %s)", err, truncate(string(out), 2000))
	}
	if clipped { // громкость теперь у среза, а не у исходника
		if lufs, peak, err = measureLoudness(ctx, tmpPath); err != nil {
			return 0, 0, err
		}
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return 0, 0, fmt.Errorf("moving audio into place: %w", err)
	}
	return lufs, playbackGain(lufs, peak), nil
}

// playbackGain brings lufs to targetLUFS. Raising a quiet track is limited
// so the sample peak stays below maxPeakDB; the limit never turns a track
// down by itself (a quiet master with 0 dBFS peaks stays as it is).
func playbackGain(lufs, peakDB float64) float64 {
	gain := targetLUFS - lufs
	if gain > 0 {
		gain = math.Max(0, math.Min(gain, maxPeakDB-peakDB))
	}
	gain = math.Max(minGainDB, math.Min(maxGainDB, gain))
	return math.Round(gain*10) / 10
}

// clipOversFilter goes before the AAC encoder when the peak is over full
// scale. MP3, Opus and float WAV decode to floats that can go past 0 dBFS
// (an overdriven master reaches +8), and ffmpeg's encoder loses 3–6 dB on
// such passages, by a different amount every second. Any player cuts those
// peaks at 0 dBFS anyway, so the file gets the same cut, as loud as the
// track has always sounded, and 1 dB of room the encoder needs to stay
// accurate.
const clipOversFilter = "asoftclip=type=hard,volume=-1dB"

func probeCodec(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", path).Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe: %w", err)
	}
	codec := strings.TrimSpace(string(out))
	if codec == "" {
		return "", fmt.Errorf("ffprobe: no audio stream in %s", path)
	}
	return codec, nil
}

var (
	integratedRe = regexp.MustCompile(`I:\s+(-?[0-9.]+|-inf) LUFS`)
	samplePeakRe = regexp.MustCompile(`Peak:\s+(-?[0-9.]+|-inf) dBFS`)
)

// measureLoudness runs ffmpeg's ebur128 filter (EBU R128 integrated
// loudness + sample peak); ~1 s for a 3-minute track.
func measureLoudness(ctx context.Context, path string) (lufs, peakDB float64, err error) {
	out, err := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "info", "-i", path,
		"-map", "0:a:0", "-af", "ebur128=peak=sample:framelog=quiet", "-f", "null", "-").CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("loudness measure: %w (output: %s)", err, truncate(string(out), 2000))
	}
	return parseEBUR128(string(out))
}

// parseEBUR128 reads the filter's final summary (the last "I:" / "Peak:"
// values in the output). Silence reports -inf; treat it as very quiet.
func parseEBUR128(out string) (lufs, peakDB float64, err error) {
	if i := strings.LastIndex(out, "Summary:"); i >= 0 {
		out = out[i:]
	}
	i := integratedRe.FindStringSubmatch(out)
	p := samplePeakRe.FindStringSubmatch(out)
	if i == nil || p == nil {
		return 0, 0, fmt.Errorf("ebur128 summary not found in ffmpeg output")
	}
	parse := func(s string) float64 {
		if s == "-inf" {
			return -70
		}
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	return parse(i[1]), parse(p[1]), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
