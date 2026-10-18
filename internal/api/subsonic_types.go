package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
)

// ID в Subsonic — строка с типом впереди: getCoverArt и getMusicDirectory
// получают ID без указания, чей он.
const (
	subTrack    = "t"
	subAlbum    = "al"
	subArtist   = "ar"
	subPlaylist = "pl"
)

// parseSubID splits "al123" into ("al", "123"); kind "" — not ours.
func parseSubID(id string) (kind, raw string) {
	for _, k := range []string{subAlbum, subArtist, subPlaylist, subTrack} {
		if rest, ok := strings.CutPrefix(id, k); ok && rest != "" {
			return k, rest
		}
	}
	return "", ""
}

// catalogParam — Deezer ID (или свой k…) из параметра name с ожидаемым
// типом kind.
func (c *subCtx) catalogParam(name, kind string) (string, error) {
	v := c.param(name)
	if v == "" {
		return "", subMissing(name)
	}
	k, raw := parseSubID(v)
	if k != kind || (kind != subPlaylist && !validCatalogID(raw)) {
		return "", subFail(subErrNotFound, "no such "+name+": "+v)
	}
	return raw, nil
}

type subArtistRef struct {
	ID   string `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type subReplayGain struct {
	TrackGain float64 `xml:"trackGain,attr" json:"trackGain"`
}

// subSong — Child: трек, а с isDir — альбом в «папках» (getMusicDirectory,
// getAlbumList, search2).
type subSong struct {
	ID             string         `xml:"id,attr" json:"id"`
	Parent         string         `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	IsDir          bool           `xml:"isDir,attr" json:"isDir"`
	Title          string         `xml:"title,attr" json:"title"`
	Album          string         `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist         string         `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Track          int            `xml:"track,attr,omitempty" json:"track,omitempty"`
	Year           int            `xml:"year,attr,omitempty" json:"year,omitempty"`
	CoverArt       string         `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Size           int64          `xml:"size,attr,omitempty" json:"size,omitempty"`
	ContentType    string         `xml:"contentType,attr,omitempty" json:"contentType,omitempty"`
	Suffix         string         `xml:"suffix,attr,omitempty" json:"suffix,omitempty"`
	Duration       int            `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	BitRate        int            `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	Path           string         `xml:"path,attr,omitempty" json:"path,omitempty"`
	DiscNumber     int            `xml:"discNumber,attr,omitempty" json:"discNumber,omitempty"`
	Created        string         `xml:"created,attr,omitempty" json:"created,omitempty"`
	Starred        string         `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	AlbumID        string         `xml:"albumId,attr,omitempty" json:"albumId,omitempty"`
	ArtistID       string         `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	Type           string         `xml:"type,attr,omitempty" json:"type,omitempty"`
	MediaType      string         `xml:"mediaType,attr,omitempty" json:"mediaType,omitempty"`
	DisplayArtist  string         `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	ExplicitStatus string         `xml:"explicitStatus,attr,omitempty" json:"explicitStatus,omitempty"`
	ReplayGain     *subReplayGain `xml:"replayGain,omitempty" json:"replayGain,omitempty"`
	Artists        []subArtistRef `xml:"artists,omitempty" json:"artists,omitempty"`
}

// subAlbumID3 — AlbumID3 (с песнями — AlbumWithSongsID3).
type subAlbumID3 struct {
	ID             string         `xml:"id,attr" json:"id"`
	Name           string         `xml:"name,attr" json:"name"`
	Artist         string         `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	ArtistID       string         `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	CoverArt       string         `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	SongCount      int            `xml:"songCount,attr" json:"songCount"`
	Duration       int            `xml:"duration,attr" json:"duration"`
	Created        string         `xml:"created,attr" json:"created"`
	Starred        string         `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	Year           int            `xml:"year,attr,omitempty" json:"year,omitempty"`
	DisplayArtist  string         `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	IsCompilation  bool           `xml:"isCompilation,attr,omitempty" json:"isCompilation,omitempty"`
	ExplicitStatus string         `xml:"explicitStatus,attr,omitempty" json:"explicitStatus,omitempty"`
	ReleaseTypes   []string       `xml:"releaseTypes,omitempty" json:"releaseTypes,omitempty"`
	Artists        []subArtistRef `xml:"artists,omitempty" json:"artists,omitempty"`
	Songs          []subSong      `xml:"song" json:"song,omitempty"`
}

// subArtistID3 — ArtistID3 (с альбомами — ArtistWithAlbumsID3).
type subArtistID3 struct {
	ID             string        `xml:"id,attr" json:"id"`
	Name           string        `xml:"name,attr" json:"name"`
	CoverArt       string        `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	ArtistImageURL string        `xml:"artistImageUrl,attr,omitempty" json:"artistImageUrl,omitempty"`
	AlbumCount     int           `xml:"albumCount,attr" json:"albumCount"`
	Starred        string        `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	Albums         []subAlbumID3 `xml:"album" json:"album,omitempty"`
}

// subArtistPlain — Artist (без ID3: getIndexes, getStarred, search2).
type subArtistPlain struct {
	ID      string `xml:"id,attr" json:"id"`
	Name    string `xml:"name,attr" json:"name"`
	Starred string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
}

func subTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// releaseCreated — дата выхода как created (клиенты сортируют по ней
// «новое»); без даты — сейчас.
func releaseCreated(date string) string {
	if t, err := time.Parse("2006-01-02", date); err == nil && t.Year() > 1 {
		return subTime(t)
	}
	return subTime(time.Now())
}

func releaseYear(date string) int {
	if t, err := time.Parse("2006-01-02", date); err == nil {
		return t.Year()
	}
	return 0
}

func joinRefNames(refs []deezer.ArtistRef) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}

func subRefs(refs []deezer.ArtistRef) []subArtistRef {
	var out []subArtistRef
	for _, r := range refs {
		out = append(out, subArtistRef{ID: subArtist + r.ID, Name: r.Name})
	}
	return out
}

// pathPart — кусок пути без «/»: клиенты раскладывают офлайн-копии по нему.
func pathPart(s string) string {
	return strings.NewReplacer("/", "_", `\`, "_").Replace(strings.TrimSpace(s))
}

// --- что из этого есть у человека и на сервере ---

// libInfo — скачанный трек: размер файла и громкость.
type libInfo struct {
	size int64
	gain float64
}

func (c *subCtx) libraryInfo(ids []string) map[string]libInfo {
	out := map[string]libInfo{}
	if len(ids) == 0 {
		return out
	}
	rows, err := c.s.DB.Query(`SELECT deezer_id, storage_key, gain_db FROM tracks WHERE deezer_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, key string
		var info libInfo
		if rows.Scan(&id, &key, &info.gain) != nil {
			continue
		}
		if c.s.StorageDir != "" {
			if st, err := os.Stat(filepath.Join(c.s.StorageDir, key)); err == nil {
				info.size = st.Size()
			}
		}
		out[id] = info
	}
	return out
}

// starred — когда человек добавил ids в table (liked_tracks, saved_albums,
// followed_artists).
func (c *subCtx) starred(table string, ids []string) map[string]time.Time {
	out := map[string]time.Time{}
	if len(ids) == 0 {
		return out
	}
	rows, err := c.s.DB.Query(`SELECT deezer_id, added_at FROM `+table+` WHERE user_id = $1 AND deezer_id = ANY($2)`,
		c.userID, pq.Array(ids))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at time.Time
		if rows.Scan(&id, &at) == nil {
			out[id] = at
		}
	}
	return out
}

func starredAt(m map[string]time.Time, id string) string {
	if at, ok := m[id]; ok {
		return subTime(at)
	}
	return ""
}

// --- перевод ---

// Наш звук — AAC около 128 кбит/с; размер нескачанного — по длительности.
const subBitRate = 128

func (c *subCtx) songs(tracks []deezer.Track) []subSong {
	ids := make([]string, 0, len(tracks))
	for _, t := range tracks {
		ids = append(ids, t.ID)
	}
	lib, liked := c.libraryInfo(ids), c.starred("liked_tracks", ids)
	out := make([]subSong, 0, len(tracks))
	for _, t := range tracks {
		if t.Hidden {
			continue
		}
		dur := t.DurationMs / 1000
		s := subSong{
			ID: subTrack + t.ID, Title: t.Title, Album: t.Album, Artist: t.Artist,
			Track: t.TrackNo, DiscNumber: t.DiscNo, Duration: dur,
			Size: int64(dur) * subBitRate * 1000 / 8, ContentType: "audio/mp4", Suffix: "m4a", BitRate: subBitRate,
			Path:    pathPart(t.Artist) + "/" + pathPart(t.Album) + "/" + pathPart(t.Title) + ".m4a",
			Starred: starredAt(liked, t.ID), Type: "music", MediaType: "song",
			DisplayArtist: t.Artist, Artists: subRefs(t.Contributors),
		}
		s.CoverArt = coverID(t.CoverURL, subTrack+t.ID)
		if t.AlbumID != "" {
			s.Parent, s.AlbumID, s.CoverArt = subAlbum+t.AlbumID, subAlbum+t.AlbumID, coverID(t.CoverURL, subAlbum+t.AlbumID)
		}
		if t.ArtistID != "" {
			s.ArtistID = subArtist + t.ArtistID
		}
		if len(t.Contributors) > 1 {
			s.DisplayArtist = joinRefNames(t.Contributors)
		}
		if t.Explicit {
			s.ExplicitStatus = "explicit"
		}
		if info, ok := lib[t.ID]; ok {
			if info.size > 0 {
				s.Size = info.size
			}
			// Наш уровень — −14 LUFS, ReplayGain считает от −18.
			s.ReplayGain = &subReplayGain{TrackGain: info.gain - 4}
		}
		out = append(out, s)
	}
	return out
}

func (c *subCtx) albums(albums []deezer.Album) []subAlbumID3 {
	ids := make([]string, 0, len(albums))
	for _, a := range albums {
		ids = append(ids, a.ID)
	}
	saved := c.starred("saved_albums", ids)
	out := make([]subAlbumID3, 0, len(albums))
	for _, a := range albums {
		if a.Hidden {
			continue
		}
		out = append(out, c.album(a, saved))
	}
	return out
}

func (c *subCtx) album(a deezer.Album, saved map[string]time.Time) subAlbumID3 {
	out := subAlbumID3{
		ID: subAlbum + a.ID, Name: a.Title, Artist: a.ArtistName, CoverArt: coverID(a.CoverURL, subAlbum+a.ID),
		SongCount: a.NbTracks, Duration: a.DurationSec, Created: releaseCreated(a.ReleaseDate),
		Year: releaseYear(a.ReleaseDate), Starred: starredAt(saved, a.ID),
		DisplayArtist: a.ArtistName, IsCompilation: a.RecordType == "compile", Artists: subRefs(a.Contributors),
	}
	if a.ArtistID != "" {
		out.ArtistID = subArtist + a.ArtistID
	}
	if len(a.Contributors) > 1 {
		out.DisplayArtist = joinRefNames(a.Contributors)
	}
	if a.Explicit {
		out.ExplicitStatus = "explicit"
	}
	switch a.RecordType {
	case "single":
		out.ReleaseTypes = []string{"Single"}
	case "ep":
		out.ReleaseTypes = []string{"EP"}
	case "compile":
		out.ReleaseTypes = []string{"Compilation"}
	default:
		out.ReleaseTypes = []string{"Album"}
	}
	return out
}

// albumDirs — альбомы как папки (Child с isDir).
func (c *subCtx) albumDirs(albums []deezer.Album) []subSong {
	var out []subSong
	for _, a := range c.albums(albums) {
		out = append(out, subSong{
			ID: a.ID, Parent: a.ArtistID, IsDir: true, Title: a.Name, Album: a.Name, Artist: a.Artist,
			Year: a.Year, CoverArt: a.CoverArt, Created: a.Created, Starred: a.Starred, AlbumID: a.ID, ArtistID: a.ArtistID,
		})
	}
	return out
}

func (c *subCtx) artists(artists []deezer.Artist, albumCount map[string]int) []subArtistID3 {
	ids := make([]string, 0, len(artists))
	for _, a := range artists {
		ids = append(ids, a.ID)
	}
	followed := c.starred("followed_artists", ids)
	out := make([]subArtistID3, 0, len(artists))
	for _, a := range artists {
		out = append(out, subArtistID3{
			ID: subArtist + a.ID, Name: a.Name, CoverArt: coverID(a.PictureURL, subArtist+a.ID),
			ArtistImageURL: c.s.publicImageURL(a.PictureURL), AlbumCount: albumCount[a.ID],
			Starred: starredAt(followed, a.ID),
		})
	}
	return out
}

func artistsPlain(in []subArtistID3) []subArtistPlain {
	out := make([]subArtistPlain, 0, len(in))
	for _, a := range in {
		out = append(out, subArtistPlain{ID: a.ID, Name: a.Name, Starred: a.Starred})
	}
	return out
}

var deezerCDNRe = regexp.MustCompile(`^https://(?:cdn-images|e-cdns-images)\.dzcdn\.net/images/`)

// publicImageURL — картинка по полному адресу нашего сайта: приложения
// берут artistImageUrl без входа, а CDN Deezer открывается не у всех.
func (s *Server) publicImageURL(u string) string {
	switch {
	case u == "":
		return ""
	case deezerCDNRe.MatchString(u):
		u = deezerCDNRe.ReplaceAllString(u, "/api/img/")
	case !strings.HasPrefix(u, "/"):
		return u
	}
	return strings.TrimRight(s.PublicURL, "/") + u
}

// coverID — ID для getCoverArt. Картинка Deezer — прямо её путь
// (img_<вид>_<хеш>_<файл>), своя — ключ файла (med_<папка>_<имя>): так
// getCoverArt отдаёт её без запросов к Deezer (приложения просят обложки
// десятками разом). Без картинки — ID объекта (al…, ar…): getCoverArt
// найдёт её сам.
func coverID(u, fallback string) string {
	if m := deezerImageRe.FindStringSubmatch(u); m != nil {
		return "img_" + m[1] + "_" + m[2] + "_" + m[3]
	}
	if key, ok := strings.CutPrefix(u, "/media/"); ok && coverKeyRe.MatchString(key) {
		dir, name := "", strings.TrimSuffix(strings.TrimPrefix(key, "covers/"), ".jpg")
		if i := strings.IndexByte(name, '/'); i >= 0 {
			dir, name = name[:i], name[i+1:]
		}
		return "med_" + dir + "_" + name
	}
	return fallback
}

// coverFromID — адрес картинки из ID coverID; ok=false — это ID объекта.
func coverFromID(id string) (string, bool) {
	if rest, ok := strings.CutPrefix(id, "img_"); ok {
		parts := strings.SplitN(rest, "_", 3)
		if len(parts) == 3 && imageKindRe.MatchString(parts[0]) && imageHashRe.MatchString(parts[1]) {
			return "https://cdn-images.dzcdn.net/images/" + parts[0] + "/" + parts[1] + "/1000x1000-" + parts[2], true
		}
		return "", true
	}
	if rest, ok := strings.CutPrefix(id, "med_"); ok {
		dir, name, _ := strings.Cut(rest, "_")
		key := "covers/" + name + ".jpg"
		if dir != "" {
			key = "covers/" + dir + "/" + name + ".jpg"
		}
		if coverKeyRe.MatchString(key) {
			return "/media/" + key, true
		}
		return "", true
	}
	return "", false
}
