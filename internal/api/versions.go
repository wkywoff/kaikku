package api

import (
	"database/sql"
	"net/http"
	"slices"

	"kaikku/internal/deezer"
)

// Версии релиза (админ, миграция 0021): Deezer держит делюкс, clean и
// переиздания отдельными релизами, и в дискографии торчат дубли. Версия
// привязывается к основному релизу: пропадает из дискографии, поиска и
// новинок (visibleAlbums) и показывается на странице основного — и на
// страницах других его версий — в «Других версиях».

// albumVersions returns the other releases of the same group as a: its main
// release and the main's versions, a itself left out. a must be overlaid
// (VersionOf set).
func (s *Server) albumVersions(r *http.Request, a *deezer.Album) []deezer.Album {
	root := a.ID
	if a.VersionOf != "" {
		root = a.VersionOf
	}
	ids, err := s.idList(`SELECT deezer_id FROM albums WHERE version_of = $1 ORDER BY released_at DESC NULLS LAST, deezer_id`, root)
	if err != nil {
		return []deezer.Album{}
	}
	if root != a.ID {
		ids = append([]string{root}, ids...)
	}
	out := make([]deezer.Album, 0, len(ids))
	for _, id := range ids {
		if id == a.ID {
			continue
		}
		if v, err := s.Deezer.GetAlbum(id); err == nil {
			out = append(out, *v)
		}
	}
	s.overlayFor(nil, albumPtrs(out), nil)
	if !showHidden(r) {
		out = slices.DeleteFunc(out, func(v deezer.Album) bool { return v.Hidden })
	}
	return out
}

// saveVersionOf makes the library album albumID a version of the release
// main (Deezer ID); "" unlinks it. Linking to a version links to its main
// release; linking to one of the album's own versions makes that one the
// main release of the group. The album's versions follow it.
func (s *Server) saveVersionOf(albumID, main string) error {
	var self string
	if err := s.DB.QueryRow(`SELECT deezer_id FROM albums WHERE id = $1`, albumID).Scan(&self); err != nil {
		return err
	}
	if main == "" {
		_, err := s.DB.Exec(`UPDATE albums SET version_of = NULL WHERE id = $1`, albumID)
		return err
	}
	if !validDeezerID(main) {
		return &badRequest{"version_of must be a Deezer album ID"}
	}
	var up sql.NullString
	if s.DB.QueryRow(`SELECT version_of FROM albums WHERE deezer_id = $1`, main).Scan(&up) == nil && up.Valid && up.String != self {
		main = up.String
	}
	if main == self {
		return &badRequest{"an album can't be a version of itself"}
	}
	if _, err := s.Deezer.GetAlbum(main); err != nil {
		return &badRequest{"album not found on Deezer"}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Был версией этого — теперь основной; версии этого — к новому основному; сам — версия.
	if _, err := tx.Exec(`UPDATE albums SET version_of = NULL WHERE deezer_id = $1`, main); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE albums SET version_of = $1 WHERE version_of = $2 OR deezer_id = $2`, main, self); err != nil {
		return err
	}
	return tx.Commit()
}
