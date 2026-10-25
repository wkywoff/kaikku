package ingest

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProbe(t *testing.T) {
	info, err := parseProbe([]byte(`{"streams":[
		{"codec_type":"audio","codec_name":"vorbis","sample_rate":"44100","channels":2,"tags":{"TITLE":"Fog","ARTIST":"тихийпричал","TRACKNUMBER":"3/12","LYRICS":"la"}},
		{"codec_type":"video","codec_name":"mjpeg","width":600,"height":600,"disposition":{"attached_pic":1}}],
		"format":{"format_name":"ogg","duration":"181.5","probe_score":100,"tags":{"ALBUM":"Ash","album_artist":"тихийпричал","DATE":"2023-05-01","disc":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := UploadTags{Title: "Fog", Artist: "тихийпричал", AlbumArtist: "тихийпричал", Album: "Ash", Date: "2023-05-01", Track: 3, Disc: 1, Lyrics: "la"}
	if info.Tags != want || info.Codec != "vorbis" || info.SampleRate != 44100 || info.Duration != 181.5 || !info.HasCover || info.CoverW != 600 {
		t.Errorf("info = %+v", info)
	}

	for _, bad := range []string{
		`{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"format_name":"hls"}}`,
		`{"streams":[{"codec_type":"audio","codec_name":"pcm_s16le"}],"format":{"format_name":"concat"}}`,
		`{"streams":[{"codec_type":"video","codec_name":"h264"}],"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","probe_score":100}}`,
		`{"streams":[{"codec_type":"audio","codec_name":"flac","sample_rate":"0","channels":0}],"format":{"format_name":"flac","probe_score":1}}`,
		`{"streams":[{"codec_type":"audio","codec_name":"flac","sample_rate":"0","channels":0}],"format":{"format_name":"flac","probe_score":100}}`,
	} {
		if _, err := parseProbe([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestReadTags_ID3Lyrics(t *testing.T) {
	tags := readTags(map[string]string{"lyrics-eng": "words", "tyer": "2019", "track": "7"})
	if tags.Lyrics != "words" || tags.Date != "2019" || tags.Track != 7 {
		t.Errorf("tags = %+v", tags)
	}
}

// Файл человека — не то, что шлёт YouTube: 96 кГц и 5.1 становятся 48 кГц
// стерео AAC, обложка из файла — квадратным JPEG, плейлист — отказ.
func TestPrepareUpload(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	ctx := context.Background()
	src := filepath.Join(dir, "hires.flac")
	cover := filepath.Join(dir, "pic.png")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg %v: %v %s", args, err, out)
		}
	}
	run("-f", "lavfi", "-i", "color=c=red:s=300x200:d=1", "-frames:v", "1", cover)
	run("-f", "lavfi", "-i", "sine=f=440:r=96000:d=2", "-i", cover,
		"-filter_complex", "[0:a]pan=5.1|c0=c0|c1=c0|c2=c0|c3=c0|c4=c0|c5=c0[a]", "-map", "[a]", "-map", "1:v",
		"-c:v", "png", "-disposition:v", "attached_pic", "-metadata", "title=Hi Res", "-metadata", "album=Test EP", src)

	info, err := ProbeUpload(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "flac" || info.SampleRate != 96000 || info.Channels != 6 || info.Tags.Title != "Hi Res" || !info.HasCover {
		t.Fatalf("info = %+v", info)
	}
	dst := filepath.Join(dir, "out.m4a")
	_, _, ms, err := PrepareUpload(ctx, src, dst, info)
	if err != nil {
		t.Fatal(err)
	}
	if ms < 1900 || ms > 2100 {
		t.Errorf("duration %d ms", ms)
	}
	out, _ := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_rate,channels", "-of", "csv=p=0", dst).Output()
	if got := strings.TrimSpace(string(out)); got != "aac,48000,2" {
		t.Errorf("output stream = %q, want aac,48000,2", got)
	}

	jpg := filepath.Join(dir, "cover.jpg")
	if err := ExtractCover(ctx, src, jpg, info); err != nil {
		t.Fatal(err)
	}
	out, _ = exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_name,width,height", "-of", "csv=p=0", jpg).Output()
	if got := strings.TrimSpace(string(out)); got != "mjpeg,200,200" {
		t.Errorf("cover = %q, want a 200×200 JPEG", got)
	}

	// Float WAV с пиками +12 dBFS: без среза кодер терял ~1 дБ сверх того,
	// что срезал бы плеер, и громкость файла расходилась с gain_db.
	hot := filepath.Join(dir, "hot.wav")
	run("-f", "lavfi", "-i", "anoisesrc=d=5:c=pink:a=0.5:r=44100", "-af", "volume=12dB", "-c:a", "pcm_f32le", hot)
	hotInfo, err := ProbeUpload(ctx, hot)
	if err != nil {
		t.Fatal(err)
	}
	hotDst := filepath.Join(dir, "hot.m4a")
	lufs, gain, _, err := PrepareUpload(ctx, hot, hotDst, hotInfo)
	if err != nil {
		t.Fatal(err)
	}
	cut := filepath.Join(dir, "cut.wav") // то же, но без сжатия
	run("-i", hot, "-af", clipOversFilter, "-c:a", "pcm_f32le", cut)
	wantLufs, _, err := measureLoudness(ctx, cut)
	if err != nil {
		t.Fatal(err)
	}
	gotLufs, _, err := measureLoudness(ctx, hotDst)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(gotLufs-wantLufs) > 0.3 || math.Abs(gotLufs-lufs) > 0.1 {
		t.Errorf("encoded file: %v LUFS, want %v; reported %v LUFS", gotLufs, wantLufs, lufs)
	}
	if math.Abs(gotLufs+gain-targetLUFS) > 0.3 {
		t.Errorf("gain %v brings %v LUFS to %v, want %v", gain, gotLufs, gotLufs+gain, targetLUFS)
	}

	playlist := filepath.Join(dir, "evil.flac")
	os.WriteFile(playlist, []byte("#EXTM3U\n#EXTINF:10,\nhttp://example.com/x.flac\n"), 0o644)
	if _, err := ProbeUpload(ctx, playlist); err == nil {
		t.Error("a playlist passed as audio")
	}
}
