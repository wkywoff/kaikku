package api

import (
	"bytes"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
)

// Заглушка обложки своего релиза: его публикуют без своей картинки, и
// сервер рисует её сам — градиент в цвете от ID релиза и пластинка
// посередине, без текста (название поменяют, а картинка останется). Она
// сохраняется как обычная обложка (covers/custom/…), так что есть везде:
// в списках, превью ссылок, Subsonic-приложениях, на экране блокировки.
// Свою картинку можно поставить потом, заглушка тогда удаляется.
// Тот же цвет берёт превью в окне правки (frontend: lib/placeholder.js).

const placeholderSide = 1000

// placeholderHue — оттенок заглушки, 0…359: FNV-1a от ID и перемешивание
// битов (fmix32 из MurmurHash3) — у соседних k1, k2… без него оттенки
// ложились кучками.
func placeholderHue(id string) float64 {
	f := fnv.New32a()
	f.Write([]byte(id))
	h := f.Sum32()
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return float64(h % 360)
}

// hsl → RGB, s и l — 0…1.
func hsl(h, s, l float64) (r, g, b float64) {
	c := (1 - math.Abs(2*l-1)) * s
	hp := math.Mod(h, 360) / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	switch {
	case hp < 1:
		r, g = c, x
	case hp < 2:
		r, g = x, c
	case hp < 3:
		g, b = c, x
	case hp < 4:
		g, b = x, c
	case hp < 5:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := l - c/2
	return r + m, g + m, b + m
}

// placeholderCover рисует заглушку для релиза id и отдаёт её JPEG.
func placeholderCover(id string) ([]byte, error) {
	hue := placeholderHue(id)
	r1, g1, b1 := hsl(hue, 0.55, 0.46)    // верхний левый угол
	r2, g2, b2 := hsl(hue+40, 0.60, 0.20) // нижний правый
	lr, lg, lb := hsl(hue, 0.50, 0.70)    // наклейка пластинки

	const n = placeholderSide
	const c = n / 2.0
	const disc, label, hole = n * 0.31, n * 0.105, n * 0.012
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	// cover — сколько пикселя внутри круга радиуса rad (сглаженный край).
	cover := func(d, rad float64) float64 { return math.Max(0, math.Min(1, rad-d+0.5)) }
	mix := func(a, b, t float64) float64 { return a + (b-a)*t }
	for y := range n {
		for x := range n {
			t := (float64(x) + float64(y)) / (2 * n)
			r, g, b := mix(r1, r2, t), mix(g1, g2, t), mix(b1, b2, t)
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			d := math.Hypot(dx, dy)
			if a := cover(d, disc); a > 0 {
				// Пластинка: тёмный круг с бороздками и бликом сверху слева.
				k := 0.62
				if d > label+n*0.02 && d < disc-n*0.015 {
					k -= 0.05 * (0.5 + 0.5*math.Sin(d*0.9))
				}
				k -= 0.06 * math.Max(0, -(dx+dy)/(math.Sqrt2*disc))
				r, g, b = mix(r, r*(1-k), a), mix(g, g*(1-k), a), mix(b, b*(1-k), a)
			}
			if a := cover(d, label); a > 0 {
				r, g, b = mix(r, lr, a), mix(g, lg, a), mix(b, lb, a)
			}
			if a := cover(d, hole); a > 0 {
				r, g, b = mix(r, r2*0.6, a), mix(g, g2*0.6, a), mix(b, b2*0.6, a)
			}
			img.SetRGBA(x, y, color.RGBA{uint8(r*255 + 0.5), uint8(g*255 + 0.5), uint8(b*255 + 0.5), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writePlaceholderCover рисует заглушку для релиза id и кладёт её в
// хранилище; ответ — её ключ (covers/custom/….jpg).
func (s *Server) writePlaceholderCover(id string) (string, error) {
	body, err := placeholderCover(id)
	if err != nil {
		return "", err
	}
	key := filepath.Join("covers", "custom", randomKey()+".jpg")
	path := filepath.Join(s.StorageDir, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return key, os.WriteFile(path, body, 0o644)
}
