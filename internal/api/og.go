package api

import (
	"bytes"
	"database/sql"
	"errors"
	"html"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

const ogTTL = 10 * time.Minute

const ogMissesPerMinute = 30

var errOGBusy = errors.New("too many link previews")

type ogMissBudget struct {
	mu    sync.Mutex
	start time.Time
	used  int
}

func (b *ogMissBudget) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.Sub(b.start) >= time.Minute {
		b.start, b.used = now, 0
	}
	if b.used >= ogMissesPerMinute {
		return false
	}
	b.used++
	return true
}

type ogMeta struct {
	Title, Description, Image, Type string
}

func (s *Server) handleOG(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := s.frontIndex.read(s.FrontendIndex)
		if err != nil {
			log.Printf("og: %v", err)
			http.Error(w, "frontend is not available", http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		key, path, track := kind+":"+id, r.URL.Path, ""
		if tr := r.URL.Query().Get("track"); kind == "album" && validCatalogID(id) && validCatalogID(tr) {
			key, path, track = "track:"+id+":"+tr, path+"?track="+tr, tr
		}
		meta, err := cached(&s.ogCache, key, ogTTL, func() (ogMeta, error) {
			if !s.ogMisses.take(time.Now()) {
				return ogMeta{}, errOGBusy
			}
			return s.ogMetaFor(kind, id, track), nil
		})
		if err != nil || meta.Title == "" {
			meta = ogMeta{}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(injectOG(page, s.ogTags(meta, path)))
	}
}

func (s *Server) ogMetaFor(kind, id, track string) ogMeta {
	switch kind {
	case "album":
		if !validCatalogID(id) {
			return ogMeta{}
		}
		a, err := s.catalogAlbum(viewer{}, id)
		if err != nil || a == nil {
			return ogMeta{}
		}
		s.overlayFor(nil, []*deezer.Album{a}, nil)
		if a.Hidden {
			return ogMeta{}
		}
		if track != "" {
			if m := s.ogTrackMeta(a, track); m.Title != "" {
				return m
			}
		}
		desc := []string{ogRecordType(a.RecordType)}
		if a.ArtistName != "" {
			desc = append(desc, a.ArtistName)
		}
		if len(a.ReleaseDate) >= 4 {
			desc = append(desc, a.ReleaseDate[:4])
		}
		return ogMeta{Title: a.Title, Description: strings.Join(desc, " · "), Image: a.CoverURL, Type: "music.album"}
	case "artist":
		if !validCatalogID(id) {
			return ogMeta{}
		}
		a, err := s.catalogArtist(id)
		if err != nil || a == nil {
			return ogMeta{}
		}
		desc := "Artist"
		if tags, err := s.tagsOf([]string{id}); err == nil && len(tags[id]) > 0 {
			var names []string
			for _, t := range tags[id][:min(3, len(tags[id]))] {
				names = append(names, t.Name)
			}
			desc += " · " + strings.Join(names, ", ")
		}
		return ogMeta{Title: a.Name, Description: desc, Image: a.PictureURL, Type: "profile"}
	case "playlist":
		var title, owner string
		var cover sql.NullString
		var n int
		err := s.DB.QueryRow(`
			SELECT p.title, u.username, COALESCE('/media/' || p.cover_key,
			         (SELECT i.meta->>'cover_url' FROM playlist_items i
			           WHERE i.playlist_id = p.id AND COALESCE(i.meta->>'cover_url', '') <> ''
			             AND NOT EXISTS (SELECT 1 FROM tracks t JOIN albums a ON a.id = t.album_id
			                              WHERE t.deezer_id = i.deezer_id AND (a.draft OR a.hidden))
			           ORDER BY i.position LIMIT 1)),
			       (SELECT count(*) FROM playlist_items i WHERE i.playlist_id = p.id)
			  FROM playlists p JOIN users u ON u.id = p.user_id
			 WHERE p.id::text = $1 AND p.is_public`, id).Scan(&title, &owner, &cover, &n)
		if err != nil {
			return ogMeta{}
		}
		return ogMeta{Title: title, Description: "Playlist · " + owner + " · " + songsCount(n), Image: cover.String, Type: "music.playlist"}
	}
	return ogMeta{}
}

func (s *Server) ogTrackMeta(a *deezer.Album, id string) ogMeta {
	t, err := s.catalogTrack(viewer{}, id)
	if err != nil || t == nil {
		return ogMeta{}
	}
	if t.AlbumID != a.ID {
		var listed bool
		if s.DB.QueryRow(`SELECT $2 = ANY(tracklist) FROM albums WHERE deezer_id = $1 AND tracklist IS NOT NULL`,
			a.ID, id).Scan(&listed) != nil || !listed {
			return ogMeta{}
		}
	}
	desc := []string{"Song"}
	if names := joinRefNames(t.Contributors); names != "" {
		desc = append(desc, names)
	} else if t.Artist != "" {
		desc = append(desc, t.Artist)
	}
	if !strings.EqualFold(a.Title, t.Title) {
		desc = append(desc, a.Title)
	}
	image := a.CoverURL
	if image == "" {
		image = t.CoverURL
	}
	return ogMeta{Title: t.Title, Description: strings.Join(desc, " · "), Image: image, Type: "music.song"}
}

func ogRecordType(t string) string {
	switch t {
	case "single":
		return "Single"
	case "ep":
		return "EP"
	case "compile":
		return "Compilation"
	}
	return "Album"
}

func songsCount(n int) string {
	if n == 1 {
		return "1 song"
	}
	return strconv.Itoa(n) + " songs"
}

func (s *Server) ogTags(m ogMeta, path string) string {
	base := strings.TrimRight(s.PublicURL, "/")
	abs := func(u string) string {
		if strings.HasPrefix(u, "/") {
			return base + u
		}
		return u
	}
	if m.Title == "" {
		m = ogMeta{Title: "kaikku", Image: "/icon-512.png", Type: "website"}
	}
	if m.Image == "" {
		m.Image = "/icon-512.png"
	}
	var b strings.Builder
	tag := func(prop, val string) {
		if val != "" {
			b.WriteString(`<meta property="` + prop + `" content="` + html.EscapeString(val) + `" />`)
		}
	}
	tag("og:site_name", "kaikku")
	tag("og:title", m.Title)
	tag("og:description", m.Description)
	tag("og:type", m.Type)
	tag("og:image", abs(m.Image))
	if base != "" {
		tag("og:url", base+path)
	}
	b.WriteString(`<meta name="twitter:card" content="summary" />`)
	return b.String()
}

var (
	ogStart = []byte("<!--og-->")
	ogEnd   = []byte("<!--/og-->")
)

func injectOG(page []byte, tags string) []byte {
	if i := bytes.Index(page, ogStart); i >= 0 {
		if j := bytes.Index(page[i:], ogEnd); j >= 0 {
			return append(append(append([]byte{}, page[:i]...), tags...), page[i+j+len(ogEnd):]...)
		}
	}
	if i := bytes.Index(page, []byte("</head>")); i >= 0 {
		return append(append(append([]byte{}, page[:i]...), tags...), page[i:]...)
	}
	return page
}

type fileCache struct {
	mu    sync.Mutex
	mtime time.Time
	size  int64
	data  []byte
}

func (c *fileCache) read(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data != nil && st.ModTime().Equal(c.mtime) && st.Size() == c.size {
		return c.data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c.data, c.mtime, c.size = data, st.ModTime(), st.Size()
	return data, nil
}
