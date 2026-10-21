package lyrics

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

// LRCRed — lrc.red: тексты в TTML с таймингом по словам (у части песен по
// слогам или только по строкам), подпевками и голосами дуэтов. Ищется
// точно по ISRC. Условия сервиса: представляться (User-Agent с адресом
// для связи — Contact) и ссылаться на lrc.red там, где показан текст
// (LyricsView). Без Contact источник не включается (EnableLyrics).
type LRCRed struct {
	Contact string // email в User-Agent
}

var lrcredAPI = "https://lrc.red" // переменная — для тестов

func (LRCRed) Name() string { return "lrcred" }

func (p LRCRed) Find(ctx context.Context, t Track) (*Lyrics, error) {
	if len(t.ISRC) != 12 || strings.Trim(strings.ToUpper(t.ISRC), "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
		return nil, nil
	}
	ua := "kaikku (https://github.com/wkywoff/kaikku; " + p.Contact + ")"
	b, err := fetch(ctx, lrcredAPI+"/s/"+strings.ToUpper(t.ISRC)+".ttml", map[string]string{"User-Agent": ua})
	var se *statusError
	if errors.As(err, &se) && se.Code == 404 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseTTML(b, t.Duration)
}

// parseTTML разбирает TTML (как у Apple Music): <p> — строка, <span> в ней
// — слово или слог (между слогами одного слова нет пробела), <span
// ttm:role="x-bg"> — подпевки, ttm:agent — голос. Переводы и
// транслитерация (x-translation, x-roman) не нужны.
func parseTTML(src []byte, duration float64) (*Lyrics, error) {
	d := xml.NewDecoder(bytes.NewReader(src))
	l := &Lyrics{}
	var timing string
	var writers []string
	groups := map[string]bool{} // голоса «все вместе» — не дуэт
	var voices []string         // голоса по порядку появления
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "tt":
			timing = strings.ToLower(attr(se, "timing"))
		case "agent":
			if attr(se, "type") != "" && attr(se, "type") != "person" {
				groups[attr(se, "id")] = true
			}
		case "songwriter":
			var s string
			if err := d.DecodeElement(&s, &se); err != nil {
				return nil, err
			}
			if s = strings.TrimSpace(s); s != "" {
				writers = append(writers, s)
			}
		case "p":
			line, err := ttmlLine(d, se)
			if err != nil {
				return nil, err
			}
			if line.Text == "" && len(line.BG) == 0 {
				continue
			}
			if a := attr(se, "agent"); a != "" && !groups[a] {
				i := slices.Index(voices, a)
				if i < 0 {
					i, voices = len(voices), append(voices, a)
				}
				line.Right = i%2 == 1
			}
			l.Lines = append(l.Lines, line)
		}
	}
	if len(l.Lines) == 0 {
		return nil, nil
	}
	l.Writers = strings.Join(writers, ", ")
	words, timed := false, false
	for _, ln := range l.Lines {
		words = words || len(ln.Words) > 0
		timed = timed || ln.End > 0
	}
	switch {
	case words && timing == "syllable":
		l.Sync = SyncSyllable
	case words:
		l.Sync = SyncWord
	case timed:
		l.Sync = SyncLine
		fillEnds(l.Lines, duration)
	default:
		l.Sync = SyncNone
		for i := range l.Lines {
			l.Lines[i].Start, l.Lines[i].End = 0, 0
		}
	}
	return l, nil
}

// ttmlLine читает <p> до конца. Строка с таймингом только по строкам —
// текст прямо в <p>, без <span>.
func ttmlLine(d *xml.Decoder, p xml.StartElement) (Line, error) {
	line := Line{Start: ttmlTime(attr(p, "begin")), End: ttmlTime(attr(p, "end"))}
	var text strings.Builder
	words, err := ttmlWords(d, &line.BG, &text)
	if err != nil {
		return line, err
	}
	line.Words = words
	if len(words) > 0 {
		line.Text = joinWords(words)
		if line.End <= line.Start {
			line.Start, line.End = words[0].Start, words[len(words)-1].End
		}
	} else {
		line.Text = strings.Join(strings.Fields(text.String()), " ")
	}
	return line, nil
}

// ttmlWords читает слова до конца текущего элемента; подпевки — в bg (nil
// внутри самих подпевок), текст вне слов — в text.
func ttmlWords(d *xml.Decoder, bg *[]Word, text *strings.Builder) ([]Word, error) {
	var words []Word
	gap := true // после последнего слова был пробел
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch tok := tok.(type) {
		case xml.EndElement:
			return words, nil
		case xml.CharData:
			if strings.TrimSpace(string(tok)) == "" {
				gap = true
			} else if text != nil {
				text.Write(tok)
			}
		case xml.StartElement:
			if tok.Name.Local != "span" {
				if err := d.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			switch attr(tok, "role") {
			case "":
			case "x-bg":
				if bg != nil {
					ws, err := ttmlWords(d, nil, nil)
					if err != nil {
						return nil, err
					}
					*bg = append(*bg, ws...)
				} else if err := d.Skip(); err != nil {
					return nil, err
				}
				gap = true
				continue
			default: // перевод, транслитерация
				if err := d.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			var s string
			if err := d.DecodeElement(&s, &tok); err != nil {
				return nil, err
			}
			txt := strings.TrimSpace(s)
			if txt == "" {
				gap = true
				continue
			}
			if len(words) > 0 && !gap && s == strings.TrimLeft(s, " \t\n") {
				words[len(words)-1].Glue = true
			}
			words = append(words, Word{Text: txt, Start: ttmlTime(attr(tok, "begin")), End: ttmlTime(attr(tok, "end"))})
			gap = s != strings.TrimRight(s, " \t\n")
		}
	}
}

// ttmlTime: «45.157», «1:14.887», «1:02:03.5», «15.553s», «500ms».
func ttmlTime(s string) float64 {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return 0
	case strings.HasSuffix(s, "ms"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "ms"), 64)
		return v / 1000
	case strings.HasSuffix(s, "s"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
		return v
	}
	var t float64
	for _, part := range strings.Split(s, ":") {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0
		}
		t = t*60 + v
	}
	return t
}

// attr — значение атрибута по имени без пространства имён (begin,
// ttm:agent, xml:id…).
func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
