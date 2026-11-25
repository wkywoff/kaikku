package lyrics

import (
	"context"
	"strings"
)

type Sync string

const (
	SyncSyllable Sync = "syllable"
	SyncWord     Sync = "word"
	SyncLine     Sync = "line"
	SyncNone     Sync = "none"
)

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

type Word struct {
	Text  string  `json:"t"`
	Start float64 `json:"s"`
	End   float64 `json:"e"`
	Glue  bool    `json:"g,omitempty"`
}

type Line struct {
	Start float64 `json:"s"`
	End   float64 `json:"e"`
	Text  string  `json:"t"`
	Words []Word  `json:"w,omitempty"`
	BG    []Word  `json:"bg,omitempty"`
	Right bool    `json:"r,omitempty"`
}

type Lyrics struct {
	Source  string `json:"source"`
	Sync    Sync   `json:"sync"`
	Lines   []Line `json:"lines"`
	Writers string `json:"writers,omitempty"`
}

type Track struct {
	DeezerID string
	ISRC     string
	Artist   string
	Title    string
	Album    string
	Duration float64
}

type Provider interface {
	Name() string
	Find(ctx context.Context, t Track) (*Lyrics, error)
}

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
