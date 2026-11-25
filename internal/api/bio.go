package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	bioMaxRunes    = 20000
	bioTTL         = 30 * 24 * time.Hour
	bioEmptyTTL    = 7 * 24 * time.Hour
	bioFetchTime   = 15 * time.Second
	artistPageTags = 5
)

type artistBio struct {
	Text   string `json:"text"`
	Source string `json:"source"`
	URL    string `json:"url,omitempty"`
}

func bioLang(r *http.Request) string {
	if r.URL.Query().Get("lang") == "ru" {
		return "ru"
	}
	return "en"
}

func (s *Server) bioOwner(id string) string {
	var owner string
	s.DB.QueryRow(`SELECT coalesce(owner_id::text, '') FROM artists WHERE deezer_id = $1`, id).Scan(&owner)
	return owner
}

func (s *Server) handleArtistBio(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}
	v := viewerOf(r)
	owner := ""
	if isLocalID(id) {
		owner = s.bioOwner(id)
	}
	var tags []artistTag
	var tagsCustom bool
	tagsDone := make(chan struct{})
	go func() {
		defer close(tagsDone)
		guard("artist tags", func() error { tags, tagsCustom = s.artistTags(r.Context(), id); return nil })
	}()
	bio := s.artistBio(r.Context(), id, bioLang(r))
	<-tagsDone
	writeJSON(w, http.StatusOK, map[string]any{
		"text": bio.Text, "source": bio.Source, "url": bio.URL,
		"editable":    v.editable(owner),
		"tags":        tagNames(tags),
		"tags_custom": tagsCustom,
	})
}

func (s *Server) artistBio(ctx context.Context, id, lang string) artistBio {
	var b artistBio
	var updated time.Time
	err := s.DB.QueryRow(
		`SELECT text, source, url, updated_at FROM artist_bios WHERE deezer_id = $1 AND lang IN ('', $2) ORDER BY lang LIMIT 1`, id, lang,
	).Scan(&b.Text, &b.Source, &b.URL, &updated)
	if err == nil {
		ttl := bioTTL
		if b.Source == "none" {
			ttl = bioEmptyTTL
		}
		if b.Source == "custom" || time.Since(updated) < ttl {
			return b
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("bio of %s: %v", id, err)
	}
	if isLocalID(id) {
		return artistBio{Source: "none"}
	}
	fresh, ok := s.fetchBio(ctx, id, lang)
	if !ok && fresh.Text == "" {
		if err == nil {
			return b
		}
		return fresh
	}
	if _, err := s.DB.Exec(
		`INSERT INTO artist_bios (deezer_id, lang, text, source, url) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (deezer_id, lang) DO UPDATE SET text = EXCLUDED.text, source = EXCLUDED.source, url = EXCLUDED.url, updated_at = now()`,
		id, lang, fresh.Text, fresh.Source, fresh.URL,
	); err != nil {
		log.Printf("bio of %s: save: %v", id, err)
	}
	return fresh
}

func (s *Server) fetchBio(ctx context.Context, id, lang string) (artistBio, bool) {
	ctx, cancel := context.WithTimeout(ctx, bioFetchTime)
	defer cancel()
	ok := true
	var dz artistBio
	if s.DeezerSite != nil {
		text, err := s.DeezerSite.ArtistBio(ctx, id, lang)
		if err != nil {
			log.Printf("bio of %s: deezer: %v", id, err)
			ok = false
		}
		dz = artistBio{Text: bioText(text), Source: "deezer", URL: "https://www.deezer.com/artist/" + id}
		if dz.Text != "" && inLang(dz.Text, lang) {
			return dz, ok
		}
	}
	var lf artistBio
	if s.Lastfm != nil {
		if a, err := s.catalogArtist(id); err != nil {
			ok = false
		} else if text, link, err := s.Lastfm.ArtistBio(ctx, a.Name, lang); err != nil {
			log.Printf("bio of %s: last.fm: %v", id, err)
			ok = false
		} else {
			lf = artistBio{Text: bioText(text), Source: "lastfm", URL: link}
		}
	}
	switch {
	case lf.Text != "" && inLang(lf.Text, lang):
		return lf, ok
	case dz.Text != "":
		return dz, ok
	case lf.Text != "":
		return lf, ok
	}
	return artistBio{Source: "none"}, ok
}

func (s *Server) handlePutArtistBio(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bioEditor(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || utf8.RuneCountInString(req.Text) > bioMaxRunes {
		writeError(w, http.StatusBadRequest, "text is up to 20000 characters")
		return
	}
	text := normalizeBio(req.Text)
	if _, err := s.DB.Exec(
		`INSERT INTO artist_bios (deezer_id, lang, text, source) VALUES ($1, '', $2, 'custom')
		 ON CONFLICT (deezer_id, lang) DO UPDATE SET text = EXCLUDED.text, updated_at = now()`, id, text,
	); err != nil {
		serverError(w, r, "could not save", err)
		return
	}
	s.handleArtistBio(w, r)
}

func (s *Server) handleDeleteArtistBio(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bioEditor(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM artist_bios WHERE deezer_id = $1 AND lang = ''`, id); err != nil {
		serverError(w, r, "could not reset", err)
		return
	}
	s.handleArtistBio(w, r)
}

func (s *Server) bioEditor(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return "", false
	}
	owner := ""
	if isLocalID(id) {
		if _, err := s.catalogArtist(id); err != nil {
			writeCatalogError(w, "artist lookup failed", err)
			return "", false
		}
		owner = s.bioOwner(id)
	}
	if !viewerOf(r).mayEdit(owner) {
		writeError(w, http.StatusForbidden, "only the artist's creator or an admin can change it")
		return "", false
	}
	return id, true
}

var (
	lastfmTail = regexp.MustCompile(`(?s)\s*<a href="https?://www\.last\.fm/[^"]*">[^<]*</a>\.?[^<]*$`)
	bioBreak   = regexp.MustCompile(`(?i)<br\s*/?>`)
	bioPara    = regexp.MustCompile(`(?i)</p\s*>`)
	bioTag     = regexp.MustCompile(`<[^>]*>`)
	bioBlank   = regexp.MustCompile(`\n{3,}`)
)

func bioText(s string) string {
	s = lastfmTail.ReplaceAllString(s, "")
	s = bioBreak.ReplaceAllString(s, "\n")
	s = bioPara.ReplaceAllString(s, "\n\n")
	s = bioTag.ReplaceAllString(s, "")
	return normalizeBio(html.UnescapeString(s))
}

func normalizeBio(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", " ", " ").Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}
	s = strings.TrimSpace(bioBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
	if utf8.RuneCountInString(s) > bioMaxRunes {
		s = string([]rune(s)[:bioMaxRunes])
	}
	return s
}

func inLang(s, lang string) bool {
	var cyr, lat int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
		case unicode.Is(unicode.Latin, r):
			lat++
		}
	}
	if lang == "ru" {
		return cyr > lat
	}
	return lat > cyr
}
