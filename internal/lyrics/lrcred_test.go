package lyrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testTTMLWords = `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:lrc="http://lrc.red/lyric-ttml-internal" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" lrc:timing="Word" xml:lang="ru"><head><metadata>` +
	`<ttm:agent type="person" xml:id="v1"><ttm:name type="full">Первый</ttm:name></ttm:agent>` +
	`<ttm:agent type="person" xml:id="v2"><ttm:name type="full">Второй</ttm:name></ttm:agent>` +
	`<ttm:agent type="group" xml:id="v1000"/>` +
	`<sourceMetadata xmlns="http://lrc.red/lyric-ttml-internal"><songwriters><songwriter>Автор Один</songwriter><songwriter>Автор Два</songwriter></songwriters></sourceMetadata></metadata></head>` +
	`<body dur="3:00.000"><div begin="1.000" end="1:05.500" lrc:songPart="Verse">` +
	`<p begin="1.000" end="3.000" ttm:agent="v1"><span begin="1.000" end="1.500">Раз</span> <span begin="1.500" end="2.000">сло</span><span begin="2.000" end="3.000">ги</span>` +
	`<span ttm:role="x-bg"><span begin="2.500" end="2.800">(эхо</span> <span begin="2.800" end="3.000">эхо)</span></span>` +
	`<span ttm:role="x-translation" xml:lang="en">One syllables</span></p>` +
	`<p begin="1:04.000" end="1:05.500" ttm:agent="v2"><span begin="1:04.000" end="1:05.500">Второй</span></p>` +
	`<p begin="1:06.000" end="1:07.000" ttm:agent="v1000"><span begin="1:06.000" end="1:07.000">Вместе</span></p>` +
	`</div></body></tt>`

func TestParseTTML_WordsSyllablesBackingVoices(t *testing.T) {
	l, err := parseTTML([]byte(testTTMLWords), 180)
	if err != nil || l == nil {
		t.Fatalf("%+v %v", l, err)
	}
	if l.Sync != SyncWord || len(l.Lines) != 3 || l.Writers != "Автор Один, Автор Два" {
		t.Fatalf("got %+v", l)
	}
	first := l.Lines[0]
	if first.Text != "Раз слоги" || first.Start != 1 || first.End != 3 || first.Right {
		t.Errorf("first line: %+v", first)
	}
	if len(first.Words) != 3 || first.Words[0].Glue || !first.Words[1].Glue || first.Words[2].Glue {
		t.Errorf("glue: %+v", first.Words)
	}
	if len(first.BG) != 2 || first.BG[0].Text != "(эхо" || first.BG[1].Start != 2.8 {
		t.Errorf("backing vocals: %+v", first.BG)
	}
	// Второй голос — справа, «все вместе» — нет.
	if l.Lines[1].Start != 64 || !l.Lines[1].Right || l.Lines[2].Right {
		t.Errorf("voices: %+v / %+v", l.Lines[1], l.Lines[2])
	}
}

func TestParseTTML_LineTimingAndPlain(t *testing.T) {
	lines := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:lrc="http://lrc.red/lyric-ttml-internal" lrc:timing="Line"><body><div>` +
		`<p begin="15.5s" end="17.9s">Первая  строка</p><p begin="18.5s">Вторая</p></div></body></tt>`
	l, err := parseTTML([]byte(lines), 30)
	if err != nil || l == nil || l.Sync != SyncLine || len(l.Lines) != 2 {
		t.Fatalf("%+v %v", l, err)
	}
	if l.Lines[0].Text != "Первая строка" || l.Lines[0].Start != 15.5 || l.Lines[1].End != 30 {
		t.Errorf("lines: %+v", l.Lines)
	}
	plain := `<tt xmlns="http://www.w3.org/ns/ttml"><body><div><p>Без времени</p></div></body></tt>`
	if l, err := parseTTML([]byte(plain), 0); err != nil || l == nil || l.Sync != SyncNone || l.Lines[0].Text != "Без времени" {
		t.Errorf("plain: %+v %v", l, err)
	}
}

func TestLRCRed_FindsByISRCWithContact(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if !strings.Contains(r.Header.Get("User-Agent"), "me@example.com") {
			http.Error(w, "who are you", 403)
			return
		}
		if r.URL.Path == "/s/GBAAA0000001.ttml" {
			io.WriteString(w, testTTMLWords)
			return
		}
		http.Error(w, `{"error":"not found"}`, 404)
	}))
	defer srv.Close()
	old := lrcredAPI
	lrcredAPI = srv.URL
	defer func() { lrcredAPI = old }()

	p := LRCRed{Contact: "me@example.com"}
	ctx := context.Background()
	if l, err := p.Find(ctx, Track{ISRC: "gbaaa0000001"}); err != nil || l == nil || l.Sync != SyncWord {
		t.Errorf("found: %+v %v", l, err)
	}
	if l, err := p.Find(ctx, Track{ISRC: "GBAAA0000002"}); l != nil || err != nil {
		t.Errorf("missing: %+v %v", l, err)
	}
	// Без ISRC или с мусором вместо него — не спрашиваем.
	for _, isrc := range []string{"", "GB/../../x1", "short"} {
		if l, err := p.Find(ctx, Track{ISRC: isrc}); l != nil || err != nil {
			t.Errorf("%q: %+v %v", isrc, l, err)
		}
	}
	if len(asked) != 2 {
		t.Errorf("requests: %v", asked)
	}
}
