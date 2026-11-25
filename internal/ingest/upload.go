package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type UploadInfo struct {
	Format         string
	Codec          string
	SampleRate     int
	Channels       int
	Duration       float64
	Tags           UploadTags
	HasCover       bool
	CoverW, CoverH int
}

type UploadTags struct {
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Date        string `json:"date,omitempty"`
	Track       int    `json:"track,omitempty"`
	Disc        int    `json:"disc,omitempty"`
	Lyrics      string `json:"lyrics,omitempty"`
}

var uploadFormats = map[string]bool{
	"flac": true, "mp3": true, "wav": true, "w64": true, "aiff": true, "ogg": true,
	"mov": true, "matroska": true, "aac": true,
}

const minProbeScore = 25

var losslessCodecs = map[string]bool{"flac": true, "alac": true, "wavpack": true, "ape": true, "tta": true}

var fileOnly = []string{"-protocol_whitelist", "file"}

func ProbeUpload(ctx context.Context, path string) (*UploadInfo, error) {
	args := append(append([]string{"-v", "error"}, fileOnly...), "-print_format", "json", "-show_format", "-show_streams", path)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &probeOutput{limit: maxProbeOutput, over: cancel}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdout = out
	if err := cmd.Run(); out.exceeded {
		return nil, fmt.Errorf("ffprobe: output over %d bytes", maxProbeOutput)
	} else if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	return parseProbe(out.buf)
}

const maxProbeOutput = 8 << 20

type probeOutput struct {
	buf      []byte
	limit    int
	over     func()
	exceeded bool
}

func (o *probeOutput) Write(p []byte) (int, error) {
	if !o.exceeded {
		if len(o.buf)+len(p) > o.limit {
			o.exceeded = true
			o.buf = nil
			o.over()
		} else {
			o.buf = append(o.buf, p...)
		}
	}
	return len(p), nil
}

type probeStream struct {
	CodecType   string            `json:"codec_type"`
	CodecName   string            `json:"codec_name"`
	SampleRate  string            `json:"sample_rate"`
	Channels    int               `json:"channels"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	Disposition map[string]int    `json:"disposition"`
	Tags        map[string]string `json:"tags"`
}

func parseProbe(out []byte) (*UploadInfo, error) {
	var p struct {
		Streams []probeStream `json:"streams"`
		Format  struct {
			FormatName string            `json:"format_name"`
			Duration   string            `json:"duration"`
			ProbeScore int               `json:"probe_score"`
			Tags       map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ffprobe output: %w", err)
	}
	info := &UploadInfo{Format: strings.Split(p.Format.FormatName, ",")[0]}
	if !uploadFormats[info.Format] {
		return nil, fmt.Errorf("unsupported format %q", p.Format.FormatName)
	}
	if p.Format.ProbeScore < minProbeScore {
		return nil, fmt.Errorf("not recognised as %s (probe score %d)", info.Format, p.Format.ProbeScore)
	}
	info.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	tags := map[string]string{}
	add := func(m map[string]string) {
		for k, v := range m {
			k = strings.ToLower(strings.ReplaceAll(k, " ", "_"))
			if _, ok := tags[k]; !ok && strings.TrimSpace(v) != "" {
				tags[k] = strings.TrimSpace(v)
			}
		}
	}
	add(p.Format.Tags)
	audio := false
	for _, s := range p.Streams {
		switch {
		case s.CodecType == "audio" && !audio:
			audio = true
			info.Codec = s.CodecName
			info.SampleRate, _ = strconv.Atoi(s.SampleRate)
			info.Channels = s.Channels
			add(s.Tags)
		case s.CodecType == "video" && (s.Disposition["attached_pic"] == 1 || s.CodecName == "mjpeg" || s.CodecName == "png"):
			if !info.HasCover {
				info.HasCover, info.CoverW, info.CoverH = true, s.Width, s.Height
			}
		}
	}
	if !audio || info.SampleRate <= 0 || info.Channels <= 0 {
		return nil, fmt.Errorf("no audio stream")
	}
	info.Tags = readTags(tags)
	return info, nil
}

func readTags(t map[string]string) UploadTags {
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := t[k]; v != "" {
				return v
			}
		}
		return ""
	}
	number := func(keys ...string) int {
		v := first(keys...)
		if i := strings.IndexByte(v, '/'); i >= 0 {
			v = v[:i]
		}
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	lyrics := first("lyrics", "unsyncedlyrics", "unsynced_lyrics")
	if lyrics == "" {
		for k, v := range t {
			if strings.HasPrefix(k, "lyrics-") {
				lyrics = v
				break
			}
		}
	}
	return UploadTags{
		Title:       first("title"),
		Artist:      first("artist"),
		AlbumArtist: first("album_artist", "albumartist"),
		Album:       first("album"),
		Date:        first("date", "originaldate", "year", "tdrc", "tyer"),
		Track:       number("track", "tracknumber"),
		Disc:        number("disc", "discnumber"),
		Lyrics:      lyrics,
	}
}

const losslessBitrate = "256k"

func PrepareUpload(ctx context.Context, srcPath, dstPath string, info *UploadInfo) (lufs, gainDB float64, durationMs int, err error) {
	lufs, peak, err := measureLoudness(ctx, srcPath)
	if err != nil {
		return 0, 0, 0, err
	}
	args := append([]string{"-nostdin", "-y", "-v", "error"}, fileOnly...)
	args = append(args, "-i", srcPath, "-map", "0:a:0", "-vn", "-map_metadata", "-1")
	resample := info.SampleRate > 48000
	downmix := info.Channels > 2
	copied := info.Codec == "aac" && !resample && !downmix
	switch {
	case copied:
		args = append(args, "-c:a", "copy")
	default:
		bitrate := fallbackBitrate
		if losslessCodecs[info.Codec] || strings.HasPrefix(info.Codec, "pcm_") {
			bitrate = losslessBitrate
		}
		args = append(args, "-c:a", "aac", "-b:a", bitrate)
		if resample {
			args = append(args, "-ar", "48000")
		}
		if downmix {
			args = append(args, "-ac", "2")
		}
	}
	clipped := !copied && peak > 0
	if clipped {
		args = append(args, "-af", clipOversFilter)
	}
	tmpPath := dstPath + ".part"
	defer os.Remove(tmpPath)
	args = append(args, "-movflags", "+faststart", "-f", "ipod", tmpPath)
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return 0, 0, 0, fmt.Errorf("ffmpeg: %w (output: %s)", err, truncate(string(out), 2000))
	}
	durationMs, err = probeDurationMs(ctx, tmpPath)
	if err != nil {
		return 0, 0, 0, err
	}
	if clipped {
		if lufs, peak, err = measureLoudness(ctx, tmpPath); err != nil {
			return 0, 0, 0, err
		}
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return 0, 0, 0, fmt.Errorf("moving audio into place: %w", err)
	}
	return lufs, playbackGain(lufs, peak), durationMs, nil
}

func probeDurationMs(ctx context.Context, path string) (int, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration: %w", err)
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || sec <= 0 {
		return 0, fmt.Errorf("ffprobe duration %q", strings.TrimSpace(string(out)))
	}
	return int(sec * 1000), nil
}

const maxCoverSide = 4096

func ExtractCover(ctx context.Context, srcPath, dstPath string, info *UploadInfo) error {
	if !info.HasCover {
		return fmt.Errorf("no picture in the file")
	}
	if info.CoverW > maxCoverSide || info.CoverH > maxCoverSide || info.CoverW < 64 || info.CoverH < 64 {
		return fmt.Errorf("picture of %d×%d", info.CoverW, info.CoverH)
	}
	tmpPath := dstPath + ".part"
	defer os.Remove(tmpPath)
	args := append([]string{"-nostdin", "-y", "-v", "error"}, fileOnly...)
	args = append(args, "-i", srcPath, "-map", "0:v:0", "-frames:v", "1",
		"-vf", "crop='min(iw,ih)':'min(iw,ih)',scale='min(1000,iw)':-2",
		"-c:v", "mjpeg", "-q:v", "3", "-f", "image2", tmpPath)
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg cover: %w (output: %s)", err, truncate(string(out), 1000))
	}
	return os.Rename(tmpPath, dstPath)
}
