package api

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Скачанные обложки и фото артистов (STORAGE_DIR/covers/...) хранятся в
// полном размере, обычно 1000×1000. Для списков и карточек фронт берёт
// уменьшенную копию: /api/cover/<px>/<key>. Копия делается при первом
// запросе и кладётся в STORAGE_DIR/.sized/<px>/<key>; ключи обложек
// случайные и при замене картинки меняются, так что копии не устаревают и
// кешируются браузером навсегда. Удаление обложки удаляет и копии
// (removeStorageFiles).
//
// Без авторизации, как и /media/covers/ в nginx: обложки публичные.

const sizedDir = ".sized"

// Размеры — как у картинок Deezer на фронте (imageproxy.js, sized и srcsetOf).
var coverSizes = map[int]bool{64: true, 200: true, 300: true, 320: true, 640: true}

var coverKeyRe = regexp.MustCompile(`^covers/(?:[a-z]+/)?[0-9a-f]{16,64}\.jpg$`)

// Уменьшение — несколько десятков мс CPU; на 1 vCPU не больше двух сразу.
var resizeSlots = make(chan struct{}, 2)

func (s *Server) handleSizedCover(w http.ResponseWriter, r *http.Request) {
	px, err := strconv.Atoi(r.PathValue("px"))
	key := r.PathValue("key")
	if err != nil || !coverSizes[px] || !coverKeyRe.MatchString(key) || s.StorageDir == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")

	sized := filepath.Join(s.StorageDir, sizedDir, strconv.Itoa(px), key)
	if _, err := os.Stat(sized); err == nil {
		http.ServeFile(w, r, sized)
		return
	}
	original := filepath.Join(s.StorageDir, key)
	resizeSlots <- struct{}{}
	data, err := resizeCover(original, sized, px)
	<-resizeSlots
	switch {
	case os.IsNotExist(err):
		http.NotFound(w, r)
	case err != nil:
		// Не разобрали картинку — отдаём как есть, лишь бы она показалась.
		http.ServeFile(w, r, original)
	case data == nil: // оригинал и так не больше px
		http.ServeFile(w, r, original)
	default:
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	}
}

// resizeCover writes a px×px copy of original to dst and returns its bytes;
// nil, nil if the original is already that small.
func resizeCover(original, dst string, px int) ([]byte, error) {
	f, err := os.Open(original)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Сначала размеры из заголовка: огромную картинку не распаковываем.
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide {
		return nil, fmt.Errorf("%dx%d is too large to resize", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Dx() <= px && b.Dy() <= px {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, downscale(src, px, px), &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	// Через временный файл: параллельный запрос не увидит половину картинки.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
		tmp := dst + "." + randomKey() + ".part"
		if os.WriteFile(tmp, buf.Bytes(), 0o644) == nil && os.Rename(tmp, dst) != nil {
			os.Remove(tmp)
		}
	}
	return buf.Bytes(), nil
}

// downscale shrinks src to w×h by averaging each destination pixel's box of
// source pixels — for big reductions (1000 → 64) this is as clean as a
// proper filter, and needs nothing beyond the standard library.
func downscale(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	sw, sh := b.Dx(), b.Dy()

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, (y+1)*sh/h
		if y1 == y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, (x+1)*sw/w
			if x1 == x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n int
			for sy := y0; sy < y1; sy++ {
				row := rgba.Pix[sy*rgba.Stride:]
				for sx := x0; sx < x1; sx++ {
					p := row[sx*4 : sx*4+4]
					r += int(p[0])
					g += int(p[1])
					bl += int(p[2])
					a += int(p[3])
					n++
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			o[0], o[1], o[2], o[3] = uint8(r/n), uint8(g/n), uint8(bl/n), uint8(a/n)
		}
	}
	return out
}

// sizedCopies lists the possible resized copies of a storage key.
func sizedCopies(key string) []string {
	if !coverKeyRe.MatchString(key) {
		return nil
	}
	var out []string
	for px := range coverSizes {
		out = append(out, filepath.Join(sizedDir, strconv.Itoa(px), key))
	}
	return out
}
