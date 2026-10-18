package api

import (
	"database/sql"
	"net/http"
	"slices"
	"sort"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
)

// Наложение ручных правок админа на ответы Deezer. Страницы артиста/альбома,
// поиск и подборки строятся из ответов Deezer, а правки лежат у нас:
//   - имя и фото артиста, название/дата/тип/обложка альбома, название и
//     номер трека — строки artists/albums/tracks (флаги edited/*_custom,
//     миграция 0006) и track_edits (0010);
//   - исполнители релиза и его скрытие — albums.artist_ids/hidden (0018);
//   - версия другого релиза — albums.version_of (0021, versions.go);
//   - исполнители трека и метка explicit — track_overrides (0018).
//
// Свои релизы (загрузка, local.go) идут тем же путём: их строки помечены
// edited/cover_custom/picture_custom, исполнители — в artist_ids и
// track_overrides, а черновик для списков — то же, что скрытый релиз.

type artistOverride struct {
	libraryID  string
	name       string // "" — не правилось
	picture    string // "" — своей картинки нет
	fullName   string // имя всегда — для исполнителей, подставленных правкой
	refPicture string // картинка для подставленных: своя, скачанная или Deezer
}

type albumOverride struct {
	libraryID   string
	edited      bool
	title       string
	releaseDate string
	recordType  string
	cover       string
	artists     []string // nil — исполнители как у Deezer
	from        []string // исполнители Deezer на момент правки
	hidden      bool
	versionOf   string
}

type trackOverride struct {
	title           string
	trackNo, discNo int // 0 — не меняли
}

type trackCredits struct {
	explicit *bool
	artists  []string
}

type overlay struct {
	artists map[string]artistOverride
	albums  map[string]albumOverride
	tracks  map[string]trackOverride
	credits map[string]trackCredits
}

// loadOverlay reads library rows for the given Deezer IDs. Errors leave an
// empty overlay: правки — украшение, страница без них всё равно рабочая.
func (s *Server) loadOverlay(artistIDs, albumIDs, trackIDs []string) *overlay {
	o := &overlay{
		artists: map[string]artistOverride{},
		albums:  map[string]albumOverride{},
		tracks:  map[string]trackOverride{},
		credits: map[string]trackCredits{},
	}

	if rows, err := s.DB.Query(
		`SELECT deezer_id, id, edited, title, coalesce(to_char(released_at, 'YYYY-MM-DD'), ''), coalesce(record_type, ''),
		        cover_custom, cover_key, artist_ids, credits_from, hidden OR draft, coalesce(version_of, '')
		   FROM albums WHERE deezer_id = ANY($1)`,
		pq.Array(albumIDs)); err == nil {
		for rows.Next() {
			var dz string
			var ov albumOverride
			var custom bool
			var cover sql.NullString
			if rows.Scan(&dz, &ov.libraryID, &ov.edited, &ov.title, &ov.releaseDate, &ov.recordType, &custom, &cover,
				pq.Array(&ov.artists), pq.Array(&ov.from), &ov.hidden, &ov.versionOf) != nil {
				continue
			}
			if custom && cover.Valid {
				ov.cover = "/media/" + cover.String
			}
			o.albums[dz] = ov
			artistIDs = append(artistIDs, ov.artists...)
		}
		rows.Close()
	}

	// Правки скачанных треков — в их строках, нескачанных — в track_edits.
	if rows, err := s.DB.Query(
		`SELECT deezer_id, title, track_no, disc_no FROM tracks WHERE edited AND deezer_id = ANY($1)
		 UNION ALL
		 SELECT deezer_id, title, track_no, disc_no FROM track_edits WHERE deezer_id = ANY($1)`, pq.Array(trackIDs)); err == nil {
		for rows.Next() {
			var dz string
			var ov trackOverride
			var trackNo, discNo sql.NullInt64
			if rows.Scan(&dz, &ov.title, &trackNo, &discNo) == nil {
				ov.trackNo, ov.discNo = int(trackNo.Int64), int(discNo.Int64)
				o.tracks[dz] = ov
			}
		}
		rows.Close()
	}

	if rows, err := s.DB.Query(
		`SELECT deezer_id, explicit, artist_ids FROM track_overrides WHERE deezer_id = ANY($1)`, pq.Array(trackIDs)); err == nil {
		for rows.Next() {
			var dz string
			var explicit sql.NullBool
			var tc trackCredits
			if rows.Scan(&dz, &explicit, pq.Array(&tc.artists)) != nil {
				continue
			}
			if explicit.Valid {
				tc.explicit = &explicit.Bool
			}
			o.credits[dz] = tc
			artistIDs = append(artistIDs, tc.artists...)
		}
		rows.Close()
	}

	// Артисты — последними: правки исполнителей выше добавили своих.
	if rows, err := s.DB.Query(
		`SELECT deezer_id, id, edited, name, picture_custom, picture_key, coalesce(picture_url, '') FROM artists WHERE deezer_id = ANY($1)`,
		pq.Array(artistIDs)); err == nil {
		for rows.Next() {
			var dz, id, name, deezerPicture string
			var edited, custom bool
			var pic sql.NullString
			if rows.Scan(&dz, &id, &edited, &name, &custom, &pic, &deezerPicture) != nil {
				continue
			}
			ov := artistOverride{libraryID: id, fullName: name, refPicture: deezerPicture}
			if edited {
				ov.name = name
			}
			if pic.Valid {
				ov.refPicture = "/media/" + pic.String
				if custom {
					ov.picture = ov.refPicture
				}
			}
			o.artists[dz] = ov
		}
		rows.Close()
	}
	return o
}

func (o *overlay) artist(a *deezer.Artist) {
	ov, ok := o.artists[a.ID]
	if !ok {
		return
	}
	a.LibraryID = ov.libraryID
	if ov.name != "" {
		a.Name = ov.name
	}
	if ov.picture != "" {
		a.PictureURL = ov.picture
	}
}

func (o *overlay) refs(refs []deezer.ArtistRef) {
	for i := range refs {
		if ov, ok := o.artists[refs[i].ID]; ok {
			if ov.name != "" {
				refs[i].Name = ov.name
			}
			if ov.picture != "" {
				refs[i].PictureURL = ov.picture
			}
		}
	}
}

// creditRefs builds the artist list a credit edit names: who Deezer already
// listed keeps their name and photo, the rest come from the artists table.
func (o *overlay) creditRefs(ids []string, known []deezer.ArtistRef) []deezer.ArtistRef {
	out := make([]deezer.ArtistRef, 0, len(ids))
	for _, id := range ids {
		ref := deezer.ArtistRef{ID: id}
		if i := slices.IndexFunc(known, func(k deezer.ArtistRef) bool { return k.ID == id }); i >= 0 {
			ref = known[i]
		}
		if ov, ok := o.artists[id]; ok {
			if ref.Name == "" {
				ref.Name = ov.fullName
			}
			if ref.PictureURL == "" {
				ref.PictureURL = ov.refPicture
			}
		}
		out = append(out, ref)
	}
	return out
}

// replaceCredits carries a release's credit edit over to one of its
// tracks: artists removed from the release are replaced by the added ones
// (in the place of the first removed, once); everyone else on the track
// stays. A release by A, B, C changed to A, B, D: a track by C becomes by D,
// a track by A (feat. X) is left alone. nil — nothing to change.
func replaceCredits(cur, from, to []string) []string {
	removed := slices.DeleteFunc(slices.Clone(from), func(id string) bool { return slices.Contains(to, id) })
	added := slices.DeleteFunc(slices.Clone(to), func(id string) bool { return slices.Contains(from, id) })
	var out []string
	replaced := false
	for _, id := range cur {
		if slices.Contains(removed, id) {
			if !replaced {
				out = append(out, added...)
				replaced = true
			}
			continue
		}
		out = append(out, id)
	}
	if !replaced {
		return nil
	}
	seen := map[string]bool{}
	out = slices.DeleteFunc(out, func(id string) bool {
		dup := seen[id]
		seen[id] = true
		return dup
	})
	if len(out) == 0 {
		return nil // убрали единственного, никого не добавив, — оставить как у Deezer
	}
	return out
}

func trackRefs(t *deezer.Track) []deezer.ArtistRef {
	if len(t.Contributors) > 0 {
		return t.Contributors
	}
	return []deezer.ArtistRef{{ID: t.ArtistID, Name: t.Artist}}
}

func albumRefs(a *deezer.Album) []deezer.ArtistRef {
	if len(a.Contributors) > 0 {
		return a.Contributors
	}
	return []deezer.ArtistRef{{ID: a.ArtistID, Name: a.ArtistName}}
}

func refIDs(refs []deezer.ArtistRef) []string {
	ids := make([]string, len(refs))
	for i, r := range refs {
		ids[i] = r.ID
	}
	return ids
}

func (o *overlay) album(a *deezer.Album) {
	ov, ok := o.albums[a.ID]
	if ok && len(ov.artists) > 0 {
		refs := o.creditRefs(ov.artists, albumRefs(a))
		a.Contributors, a.ArtistID, a.ArtistName = refs, refs[0].ID, refs[0].Name
	}
	if aov, found := o.artists[a.ArtistID]; found && aov.name != "" {
		a.ArtistName = aov.name
	}
	o.refs(a.Contributors)
	if !ok {
		return
	}
	a.LibraryID = ov.libraryID
	a.Hidden = ov.hidden
	a.VersionOf = ov.versionOf
	if ov.edited {
		a.Title = ov.title
		a.ReleaseDate = ov.releaseDate
		if ov.recordType != "" {
			a.RecordType = ov.recordType
		}
	}
	if ov.cover != "" {
		a.CoverURL = ov.cover
	}
}

func (o *overlay) track(t *deezer.Track) {
	tc := o.credits[t.ID]
	if tc.explicit != nil {
		t.Explicit = *tc.explicit
	}
	al, hasAlbum := o.albums[t.AlbumID]
	ids := tc.artists
	if len(ids) == 0 && hasAlbum && len(al.artists) > 0 {
		if isLocalID(t.AlbumID) {
			ids = al.artists // свой релиз: исполнители трека — как у релиза
		} else {
			ids = replaceCredits(refIDs(trackRefs(t)), al.from, al.artists)
		}
	}
	if len(ids) > 0 {
		refs := o.creditRefs(ids, trackRefs(t))
		t.Contributors, t.ArtistID, t.Artist = refs, refs[0].ID, refs[0].Name
	}
	if ov, ok := o.tracks[t.ID]; ok {
		t.Title = ov.title
		if ov.trackNo > 0 {
			t.TrackNo = ov.trackNo
		}
		if ov.discNo > 0 {
			t.DiscNo = ov.discNo
		}
	}
	if ov, ok := o.artists[t.ArtistID]; ok && ov.name != "" {
		t.Artist = ov.name
	}
	o.refs(t.Contributors)
	if hasAlbum {
		t.Hidden = al.hidden
		if al.edited {
			t.Album = al.title
		}
		if al.cover != "" {
			t.CoverURL = al.cover
		}
	}
}

// overlayFor collects every Deezer ID mentioned in the response and applies
// the library's manual edits to all of them.
func (s *Server) overlayFor(artists []*deezer.Artist, albums []*deezer.Album, tracks []deezer.Track) {
	var artistIDs, albumIDs, trackIDs []string
	for _, a := range artists {
		artistIDs = append(artistIDs, a.ID)
	}
	for _, a := range albums {
		albumIDs = append(albumIDs, a.ID)
		artistIDs = append(artistIDs, a.ArtistID)
		for _, c := range a.Contributors {
			artistIDs = append(artistIDs, c.ID)
		}
	}
	for _, t := range tracks {
		trackIDs = append(trackIDs, t.ID)
		albumIDs = append(albumIDs, t.AlbumID)
		artistIDs = append(artistIDs, t.ArtistID)
		for _, c := range t.Contributors {
			artistIDs = append(artistIDs, c.ID)
		}
	}
	o := s.loadOverlay(artistIDs, albumIDs, trackIDs)
	for _, a := range artists {
		o.artist(a)
	}
	for _, a := range albums {
		o.album(a)
	}
	for i := range tracks {
		o.track(&tracks[i])
	}
}

// --- перенесённые релизы в дискографии ---

// creditedDiscography adjusts an artist's Deezer discography for credit
// edits: releases moved to the artist are added, releases moved away are
// dropped. Added ones come from Deezer (its album cache), own uploads from
// the database, newest first like the rest. For an own artist albums is nil
// and everything comes from the database.
func (s *Server) creditedDiscography(artistID string, albums []deezer.Album) []deezer.Album {
	ids := make([]string, len(albums))
	for i, a := range albums {
		ids[i] = a.ID
	}
	rows, err := s.DB.Query(
		`SELECT deezer_id, artist_ids FROM albums
		  WHERE artist_ids IS NOT NULL AND (artist_ids @> ARRAY[$1] OR deezer_id = ANY($2))`,
		artistID, pq.Array(ids))
	if err != nil {
		return albums
	}
	moved := map[string]bool{}
	var add []string
	for rows.Next() {
		var dz string
		var credited []string
		if rows.Scan(&dz, pq.Array(&credited)) != nil {
			continue
		}
		switch {
		case !slices.Contains(credited, artistID):
			moved[dz] = true
		case !slices.Contains(ids, dz):
			add = append(add, dz)
		}
	}
	rows.Close()
	if len(moved) == 0 && len(add) == 0 {
		return albums
	}
	out := slices.DeleteFunc(albums, func(a deezer.Album) bool { return moved[a.ID] })
	var own []string
	for _, dz := range add {
		if isLocalID(dz) {
			own = append(own, dz)
		} else if a, err := s.Deezer.GetAlbum(dz); err == nil {
			out = append(out, *a)
		}
	}
	if cards, err := s.localAlbumCards(own); err == nil {
		out = append(out, cards...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ReleaseDate > out[j].ReleaseDate })
	return out
}

// --- скрытые релизы ---

// showHidden — показывать ли скрытые релизы: только админу в режиме админа
// (фронт присылает заголовок, пока режим включён).
func showHidden(r *http.Request) bool {
	admin, _ := r.Context().Value(isAdminCtxKey).(bool)
	return admin && r.Header.Get("X-Admin-Mode") == "1"
}

// visibleAlbums / visibleTracks drop hidden releases and their tracks from
// catalogue lists (after overlayFor marked them). Personal lists — liked,
// playlists, saved albums — keep them: they show greyed out. Versions of
// another release (versions.go) are dropped from catalogue lists even for
// the admin: they are on the main release's page.
func visibleAlbums(r *http.Request, albums []deezer.Album) []deezer.Album {
	show := showHidden(r)
	return slices.DeleteFunc(albums, func(a deezer.Album) bool { return a.VersionOf != "" || a.Hidden && !show })
}

func visibleTracks(r *http.Request, tracks []deezer.Track) []deezer.Track {
	if showHidden(r) {
		return tracks
	}
	return slices.DeleteFunc(tracks, func(t deezer.Track) bool { return t.Hidden })
}
