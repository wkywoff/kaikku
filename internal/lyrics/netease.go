package lyrics

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// NetEase — NetEase Cloud Music (music.163.com): большая база текстов с
// таймингом по строкам, в том числе русских. Публичные методы поиска и
// текста работают без токенов. Песня ищется по исполнителю и названию;
// ответ сверяется по названию, исполнителю и длительности.
type NetEase struct{}

var neteaseAPI = "https://music.163.com/api" // переменная — для тестов

var neteaseHeaders = map[string]string{"Referer": "https://music.163.com/"}

func (NetEase) Name() string { return "netease" }

func (n NetEase) Find(ctx context.Context, t Track) (*Lyrics, error) {
	id, err := n.search(ctx, t)
	if err != nil || id == 0 {
		return nil, err
	}
	var r struct {
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		NoLyric     bool `json:"nolyric"`
		Uncollected bool `json:"uncollected"`
	}
	if err := fetchJSON(ctx, fmt.Sprintf("%s/song/lyric?id=%d&lv=1&kv=1&tv=-1", neteaseAPI, id), neteaseHeaders, &r); err != nil {
		return nil, err
	}
	if r.NoLyric || r.Uncollected || strings.TrimSpace(r.Lrc.Lyric) == "" {
		return nil, nil
	}
	if l := parseLRC(r.Lrc.Lyric, t.Duration, neteaseCredit); l != nil {
		return l, nil
	}
	return plainText(r.Lrc.Lyric), nil
}

// neteaseCredit — служебные строки NetEase: авторы («作词 : …»),
// «инструментал» и подобное.
func neteaseCredit(s string) bool {
	for _, p := range []string{"作词", "作曲", "编曲", "制作", "混音", "母带", "录音", "和声", "吉他", "贝斯", "鼓", "监制", "出品", "发行", "OP", "SP", "纯音乐"} {
		if strings.HasPrefix(s, p) && (strings.Contains(s, ":") || strings.Contains(s, "：") || p == "纯音乐") {
			return true
		}
	}
	return false
}

func (NetEase) search(ctx context.Context, t Track) (int64, error) {
	q := url.Values{"s": {t.Artist + " " + t.Title}, "type": {"1"}, "limit": {"10"}}
	var r struct {
		Result struct {
			Songs []struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Duration int    `json:"duration"`
				Artists  []struct {
					Name string `json:"name"`
				} `json:"artists"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := fetchJSON(ctx, neteaseAPI+"/search/get?"+q.Encode(), neteaseHeaders, &r); err != nil {
		return 0, err
	}
	for _, s := range r.Result.Songs {
		if !sameSong(t, s.Name, float64(s.Duration)/1000) {
			continue
		}
		for _, a := range s.Artists {
			if sameArtist(t.Artist, a.Name) {
				return s.ID, nil
			}
		}
	}
	return 0, nil
}

// sameArtist: имена совпадают (одно содержит другое) после нормализации.
func sameArtist(want, got string) bool {
	a, b := norm(want), norm(got)
	return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a))
}
