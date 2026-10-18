package lyrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Deezer — тексты самого Deezer (лицензия LyricFind): у части песен тайминг
// по словам или по строкам, у остальных — текст. Ищется точно по Deezer ID
// трека. Метод закрытый (GraphQL веб-плеера), но анонимный: короткий токен
// выдаёт auth.deezer.com любому гостю сайта.
type Deezer struct {
	mu  sync.Mutex
	jwt string
	exp time.Time
}

var ( // переменные — для тестов
	deezerAuth = "https://auth.deezer.com/login/anonymous?jo=p&rto=c"
	deezerPipe = "https://pipe.deezer.com/api"
)

const deezerLyricsQuery = `query Lyrics($id: String!) { track(trackId: $id) { lyrics {
  text writers
  synchronizedLines { line milliseconds duration }
  synchronizedWordByWordLines { start end words { start end word } }
} } }`

func (d *Deezer) Name() string { return "deezer" }

func (d *Deezer) Find(ctx context.Context, t Track) (*Lyrics, error) {
	if t.DeezerID == "" || strings.Trim(t.DeezerID, "0123456789") != "" {
		return nil, nil // не трек Deezer (свой, загруженный — k…)
	}
	var r struct {
		Data struct {
			Track *struct {
				Lyrics *struct {
					Text    string `json:"text"`
					Writers string `json:"writers"`
					Lines   []struct {
						Line     string `json:"line"`
						Ms       int    `json:"milliseconds"`
						Duration int    `json:"duration"`
					} `json:"synchronizedLines"`
					Words []struct {
						Start int `json:"start"`
						End   int `json:"end"`
						Words []struct {
							Start int    `json:"start"`
							End   int    `json:"end"`
							Word  string `json:"word"`
						} `json:"words"`
					} `json:"synchronizedWordByWordLines"`
				} `json:"lyrics"`
			} `json:"track"`
		} `json:"data"`
		Errors []struct {
			Type string `json:"type"`
		} `json:"errors"`
	}
	body := map[string]any{"operationName": "Lyrics", "query": deezerLyricsQuery, "variables": map[string]string{"id": t.DeezerID}}
	for attempt := 0; ; attempt++ {
		jwt, err := d.token(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		err = postJSON(ctx, deezerPipe, map[string]string{"Authorization": "Bearer " + jwt}, body, &r)
		var se *statusError
		if errors.As(err, &se) && se.Code == 401 && attempt == 0 {
			continue // токен истёк раньше срока — новый
		}
		if err != nil {
			return nil, err
		}
		break
	}
	for _, e := range r.Errors {
		if e.Type == "LyricsNotFoundError" {
			return nil, nil
		}
	}
	if r.Data.Track == nil || r.Data.Track.Lyrics == nil {
		if len(r.Errors) > 0 {
			return nil, fmt.Errorf("deezer lyrics: %s", r.Errors[0].Type)
		}
		return nil, nil
	}
	src := r.Data.Track.Lyrics
	var l *Lyrics
	switch {
	case len(src.Words) > 0:
		l = &Lyrics{Sync: SyncWord}
		for _, ln := range src.Words {
			line := Line{Start: ms(ln.Start), End: ms(ln.End)}
			for _, w := range ln.Words {
				if txt := strings.TrimSpace(w.Word); txt != "" {
					line.Words = append(line.Words, Word{Text: txt, Start: ms(w.Start), End: ms(w.End)})
				}
			}
			if line.Text = joinWords(line.Words); line.Text != "" {
				l.Lines = append(l.Lines, line)
			}
		}
	case len(src.Lines) > 0:
		l = &Lyrics{Sync: SyncLine}
		for _, ln := range src.Lines {
			if txt := strings.Join(strings.Fields(ln.Line), " "); txt != "" {
				l.Lines = append(l.Lines, Line{Start: ms(ln.Ms), End: ms(ln.Ms + ln.Duration), Text: txt})
			}
		}
		fillEnds(l.Lines, t.Duration)
	default:
		l = plainText(src.Text)
	}
	if l == nil || len(l.Lines) == 0 {
		return nil, nil
	}
	l.Writers = src.Writers
	return l, nil
}

func ms(v int) float64 { return float64(v) / 1000 }

// token — анонимный токен Deezer (живёт несколько минут).
func (d *Deezer) token(ctx context.Context, renew bool) (string, error) {
	d.mu.Lock()
	if !renew && d.jwt != "" && time.Now().Before(d.exp) {
		t := d.jwt
		d.mu.Unlock()
		return t, nil
	}
	d.mu.Unlock()
	var r struct {
		JWT string `json:"jwt"`
	}
	if err := fetchJSON(ctx, deezerAuth, nil, &r); err != nil {
		return "", fmt.Errorf("deezer auth: %w", err)
	}
	if r.JWT == "" {
		return "", errors.New("deezer auth: no token")
	}
	exp := time.Now().Add(4 * time.Minute)
	if parts := strings.Split(r.JWT, "."); len(parts) == 3 {
		var c struct {
			Exp int64 `json:"exp"`
		}
		if b, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil && json.Unmarshal(b, &c) == nil && c.Exp > 0 {
			exp = time.Unix(c.Exp, 0).Add(-30 * time.Second)
		}
	}
	d.mu.Lock()
	d.jwt, d.exp = r.JWT, exp
	d.mu.Unlock()
	return r.JWT, nil
}
