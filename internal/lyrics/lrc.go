package lyrics

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	lrcStampRe = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2}(?:[.:]\d{1,3})?)\]`)
	lrcWordRe  = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`) // «расширенный» LRC: время слов — убираем
	lrcMetaRe  = regexp.MustCompile(`^\[[a-zA-Z#]+:.*\]$`)               // [ar:…], [ti:…], [offset:…]
)

// parseLRC разбирает LRC: «[mm:ss.xx]строка», у строки может быть
// несколько отметок («[00:12.00][01:30.00]припев»). Пустая строка с
// отметкой — конец предыдущей (проигрыш). skip отбрасывает служебные строки
// (у NetEase — авторы в начале). Без единой отметки — nil.
func parseLRC(src string, duration float64, skip func(string) bool) *Lyrics {
	type entry struct {
		at   float64
		text string
	}
	var es []entry
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r", ""), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" || lrcMetaRe.MatchString(raw) {
			continue
		}
		stamps := lrcStampRe.FindAllStringSubmatchIndex(raw, -1)
		if len(stamps) == 0 || stamps[0][0] != 0 {
			continue
		}
		// Текст — после последней из подряд идущих отметок.
		end := 0
		for _, s := range stamps {
			if s[0] != end {
				break
			}
			end = s[1]
		}
		text := strings.TrimSpace(lrcWordRe.ReplaceAllString(raw[end:], ""))
		text = strings.Join(strings.Fields(text), " ")
		if text != "" && skip != nil && skip(text) {
			continue
		}
		for _, s := range stamps {
			if s[0] >= end {
				break
			}
			min, _ := strconv.Atoi(raw[s[2]:s[3]])
			sec, _ := strconv.ParseFloat(strings.Replace(raw[s[4]:s[5]], ":", ".", 1), 64)
			es = append(es, entry{float64(min)*60 + sec, text})
		}
	}
	if len(es) == 0 {
		return nil
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].at < es[j].at })
	l := &Lyrics{Sync: SyncLine}
	for i, e := range es {
		if e.text == "" {
			continue
		}
		line := Line{Start: e.at, Text: e.text}
		if i+1 < len(es) {
			line.End = es[i+1].at
		}
		l.Lines = append(l.Lines, line)
	}
	if len(l.Lines) == 0 {
		return nil
	}
	fillEnds(l.Lines, duration)
	// Все строки в одной точке — это не тайминг, а текст с нулевыми отметками.
	if l.Lines[0].Start == l.Lines[len(l.Lines)-1].Start && len(l.Lines) > 1 {
		return plainText(joinLines(l.Lines))
	}
	return l
}

func joinLines(ls []Line) string {
	var b strings.Builder
	for _, ln := range ls {
		b.WriteString(ln.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// plainText — текст без времени; пустые строки — разделители куплетов
// (подряд — одна), по краям не нужны.
func plainText(body string) *Lyrics {
	l := &Lyrics{Sync: SyncNone}
	blank := false
	for _, s := range strings.Split(strings.ReplaceAll(body, "\r", ""), "\n") {
		s = strings.Join(strings.Fields(s), " ")
		if s == "" {
			if !blank && len(l.Lines) > 0 {
				l.Lines = append(l.Lines, Line{})
			}
			blank = true
			continue
		}
		blank = false
		l.Lines = append(l.Lines, Line{Text: s})
	}
	for len(l.Lines) > 0 && l.Lines[len(l.Lines)-1].Text == "" {
		l.Lines = l.Lines[:len(l.Lines)-1]
	}
	if len(l.Lines) == 0 {
		return nil
	}
	return l
}

// SourceUpload — текст своего трека (загрузка), его дал автор.
const SourceUpload = "upload"

// FromText — текст, который автор дал своему треку: LRC с отметками
// времени (тогда он синхронный) или просто строки. nil — пусто.
func FromText(text string, duration float64) *Lyrics {
	l := parseLRC(text, duration, nil)
	if l == nil {
		l = plainText(text)
	}
	if l != nil {
		l.Source = SourceUpload
	}
	return l
}

// ToText — обратно в текст для окна загрузки: LRC, если есть время.
func ToText(l *Lyrics) string {
	if l == nil {
		return ""
	}
	var b strings.Builder
	for _, ln := range l.Lines {
		if l.Sync != SyncNone && ln.Text != "" {
			m := int(ln.Start) / 60
			fmt.Fprintf(&b, "[%02d:%05.2f]", m, ln.Start-float64(m*60))
		}
		b.WriteString(ln.Text)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
