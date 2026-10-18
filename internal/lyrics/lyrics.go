// Package lyrics finds song lyrics, preferably time-synced, and keeps what
// it found in the database.
//
// Sources (Service.Providers), none of them needs a token: Deezer (exact
// by Deezer ID; word or line timing for some songs, otherwise text),
// NetEase Cloud Music and LRCLIB (line timing, found by artist, title and
// length). Word timing wins at once; otherwise the best timing found, with
// NetEase and LRCLIB preferred over Deezer's line timing (Service.better).
package lyrics

import (
	"context"
	"strings"
)

// Sync — насколько подробно текст привязан ко времени.
type Sync string

const (
	SyncSyllable Sync = "syllable" // по слогам (сейчас ни один источник не даёт)
	SyncWord     Sync = "word"     // по словам
	SyncLine     Sync = "line"     // по строкам
	SyncNone     Sync = "none"     // текст без времени
)

// Rank: чем подробнее тайминг, тем лучше; 0 — текста нет.
func (s Sync) Rank() int {
	switch s {
	case SyncSyllable:
		return 4
	case SyncWord:
		return 3
	case SyncLine:
		return 2
	case SyncNone:
		return 1
	}
	return 0
}

// Word — слово или слог. Время — в секундах от начала трека.
type Word struct {
	Text  string  `json:"t"`
	Start float64 `json:"s"`
	End   float64 `json:"e"`
	// Glue: следующий кусок продолжает это же слово (слоги), пробела нет.
	Glue bool `json:"g,omitempty"`
}

// Line — строка текста. Words пусто, если тайминг только по строкам.
type Line struct {
	Start float64 `json:"s"`
	End   float64 `json:"e"`
	Text  string  `json:"t"`
	Words []Word  `json:"w,omitempty"`
	BG    []Word  `json:"bg,omitempty"` // подпевки (если источник их отделяет)
	Right bool    `json:"r,omitempty"`  // второй голос дуэта — справа
}

type Lyrics struct {
	Source  string `json:"source"` // deezer | netease | lrclib
	Sync    Sync   `json:"sync"`
	Lines   []Line `json:"lines"`
	Writers string `json:"writers,omitempty"`
}

// Track — что ищем. Duration — в секундах.
type Track struct {
	DeezerID string
	ISRC     string
	Artist   string
	Title    string
	Album    string
	Duration float64
}

// Provider — источник текстов. Find возвращает nil, nil, если текста нет;
// ошибка — источник сейчас недоступен (такой ответ не запоминается).
type Provider interface {
	Name() string
	Find(ctx context.Context, t Track) (*Lyrics, error)
}

// joinWords собирает текст строки из кусков с учётом Glue.
func joinWords(ws []Word) string {
	var b strings.Builder
	for i, w := range ws {
		b.WriteString(w.Text)
		if i < len(ws)-1 && !w.Glue {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// fillEnds проставляет конец строк, у которых его нет: начало следующей
// или конец трека.
func fillEnds(lines []Line, duration float64) {
	for i := range lines {
		if lines[i].End > lines[i].Start {
			continue
		}
		if i+1 < len(lines) {
			lines[i].End = lines[i+1].Start
		} else if duration > lines[i].Start {
			lines[i].End = duration
		} else {
			lines[i].End = lines[i].Start + 5
		}
	}
}

// norm — для сравнения названий: нижний регистр, без знаков и скобок с
// пометками («feat.», «Remastered»).
func norm(s string) string {
	s = strings.ToLower(s)
	if i := strings.IndexAny(s, "(["); i > 0 {
		s = s[:i]
	}
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 'а' && r <= 'я' || r == 'ё' || r > 0x2E80 {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// sameSong: названия совпадают (одно содержит другое) и длительность в
// пределах допуска. Защита от «похожих» ответов источников.
func sameSong(t Track, title string, durationS float64) bool {
	a, b := norm(t.Title), norm(title)
	if a == "" || b == "" || !(strings.Contains(a, b) || strings.Contains(b, a)) {
		return false
	}
	if t.Duration > 0 && durationS > 0 {
		d := t.Duration - durationS
		if d < -4 || d > 4 {
			return false
		}
	}
	return true
}
