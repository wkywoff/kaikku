package lyrics

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// LRCLIB — открытая база текстов (lrclib.net) с таймингом по строкам.
// Запрос точный: исполнитель, название, альбом и длительность (сервис сам
// допускает расхождение в пару секунд).
type LRCLIB struct{}

var lrclibAPI = "https://lrclib.net/api" // переменная — для тестов

func (LRCLIB) Name() string { return "lrclib" }

func (LRCLIB) Find(ctx context.Context, t Track) (*Lyrics, error) {
	if t.Artist == "" || t.Title == "" {
		return nil, nil
	}
	q := url.Values{"artist_name": {t.Artist}, "track_name": {t.Title}}
	if t.Album != "" {
		q.Set("album_name", t.Album)
	}
	if t.Duration > 0 {
		q.Set("duration", strconv.Itoa(int(t.Duration+0.5)))
	}
	var r struct {
		Instrumental bool   `json:"instrumental"`
		Plain        string `json:"plainLyrics"`
		Synced       string `json:"syncedLyrics"`
	}
	// Просят представляться: имя приложения и адрес.
	err := fetchJSON(ctx, lrclibAPI+"/get?"+q.Encode(), map[string]string{"User-Agent": "kaikku (https://github.com/wkywoff/kaikku)"}, &r)
	var se *statusError
	if errors.As(err, &se) && se.Code == 404 {
		return nil, nil
	}
	if err != nil || r.Instrumental {
		return nil, err
	}
	if l := parseLRC(r.Synced, t.Duration); l != nil {
		return l, nil
	}
	return plainText(r.Plain), nil
}
