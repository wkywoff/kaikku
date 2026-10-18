package api

import (
	"bytes"
	"database/sql"
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

// Превью ссылок (Telegram, iMessage, WhatsApp): ссылка на альбом, артиста
// или публичный плейлист — страница фронта (index.html) с og-тегами этого
// альбома. Боты, которые строят превью, не входят на сайт и не выполняют
// скрипты, поэтому теги вставляет сервер. Сам сайт по-прежнему только для
// своих: здесь видны лишь название и обложка — как в любой ссылке Spotify.
// Подписи — по-английски: превью строит бот мессенджера, один раз на всех
// в чате, и языка читателя не знает.
// Приватный плейлист, скрытый релиз, чужой черновик или ошибка — страница
// без подробностей (только «kaikku»). nginx шлёт сюда /album/…, /artist/…
// и /playlist/… (docker/nginx.conf.template), остальное отдаёт сам.

const ogTTL = 10 * time.Minute

type ogMeta struct {
	Title, Description, Image, Type string
}

// handleOG — index.html с og-тегами для kind (album | artist | playlist).
func (s *Server) handleOG(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := s.frontIndex.read(s.FrontendIndex)
		if err != nil {
			log.Printf("og: %v", err)
			http.Error(w, "frontend is not available", http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		meta, err := cached(&s.cache, "og:"+kind+":"+id, ogTTL, func() (ogMeta, error) { return s.ogMetaFor(kind, id), nil })
		if err != nil || meta.Title == "" {
			meta = ogMeta{}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache") // как index.html у nginx: после деплоя — новая сборка
		w.Write(injectOG(page, s.ogTags(meta, r.URL.Path)))
	}
}

func (s *Server) ogMetaFor(kind, id string) ogMeta {
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
			           WHERE i.playlist_id = p.id AND COALESCE(i.meta->>'cover_url', '') <> '' ORDER BY i.position LIMIT 1)),
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

// ogTags — блок тегов; пустой meta — только «kaikku» и иконка.
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

// injectOG ставит теги на место блока <!--og-->…<!--/og--> (index.html), а
// если его нет — перед </head>.
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

// fileCache — index.html фронта, перечитывается, когда файл сменился (деплой).
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
