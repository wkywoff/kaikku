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

// Свои релизы (загрузка, internal/api/uploads_audio.go): файл, который
// прислал человек, а не YouTube. Он может быть любым, поэтому сначала
// ffprobe проверяет, что это обычный аудиофайл (не плейлист, который ffmpeg
// пошёл бы скачивать), а сохраняется он тем же AAC в .m4a, что и всё
// остальное: громкость — поправкой в базе (PrepareAudio), без «запекания».

// UploadInfo — что ffprobe узнал о загруженном файле.
type UploadInfo struct {
	Format     string // первое имя из format_name: flac, mp3, wav, mov (m4a/mp4), ogg…
	Codec      string // кодек звука
	SampleRate int
	Channels   int
	Duration   float64 // секунды
	Tags       UploadTags
	// Картинка в файле (обложка во FLAC/MP3/M4A) и её размер.
	HasCover       bool
	CoverW, CoverH int
}

// UploadTags — теги, по которым фронт заполняет пустые поля релиза.
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

// Форматы, которые принимаются (первое имя format_name у ffprobe). Всё
// остальное — в том числе hls, concat и прочие «плейлисты», которые ffmpeg
// открывает как ссылки на другие файлы, — отказ.
var uploadFormats = map[string]bool{
	"flac": true, "mp3": true, "wav": true, "w64": true, "aiff": true, "ogg": true,
	"mov": true /* m4a, mp4, alac */, "matroska": true /* mka, webm */, "aac": true,
}

// minProbeScore — насколько ffprobe уверен в формате (100 — узнал по
// содержимому, 1 — только по расширению).
const minProbeScore = 25

var losslessCodecs = map[string]bool{"flac": true, "alac": true, "wavpack": true, "ape": true, "tta": true}

// Только локальные файлы: ffprobe/ffmpeg по умолчанию открыли бы и ссылки.
var fileOnly = []string{"-protocol_whitelist", "file"}

// ProbeUpload проверяет загруженный файл и читает его теги.
func ProbeUpload(ctx context.Context, path string) (*UploadInfo, error) {
	args := append(append([]string{"-v", "error"}, fileOnly...), "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	return parseProbe(out)
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
	// Не узнав содержимое, ffprobe верит расширению: текст в «.flac» — это
	// «raw FLAC» с оценкой 1, без частоты и каналов.
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
			add(s.Tags) // теги Ogg/Opus — у потока, не у файла
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

// readTags сводит разные названия тегов (ID3, Vorbis, MP4) к одному виду.
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
		if i := strings.IndexByte(v, '/'); i >= 0 { // «3/12»
			v = v[:i]
		}
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	lyrics := first("lyrics", "unsyncedlyrics", "unsynced_lyrics")
	if lyrics == "" {
		for k, v := range t { // ID3 USLT: «lyrics-eng», «lyrics-XXX»
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

// Битрейт своих загрузок: из lossless — 256k (исходник того стоит),
// из остального — как при перекодировании скачанного.
const losslessBitrate = "256k"

// PrepareUpload writes an uploaded file's audio to dstPath as AAC in .m4a
// like PrepareAudio, but evens out what a human's file can have and YouTube
// never sends: more than 48 kHz (not a valid AAC rate past 96) and more
// than two channels go down to 48 kHz stereo. It returns the loudness, the
// playback gain and the duration of the result.
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
	if clipped { // громкость теперь у среза, а не у исходника
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

// maxCoverSide — больше этого картинку из файла не берём: распаковка
// съела бы память (как JPEG-бомба у загрузки обложки, api/handlers_edit.go).
const maxCoverSide = 4096

// ExtractCover saves the picture embedded in an uploaded file as a square
// JPEG of at most 1000 px — the shape of every other cover.
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
