package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
	"kaikku/internal/lyrics"
)

func canUpload(r *http.Request) bool {
	return currentUser(r) != ""
}

type releaseRow struct {
	id        string
	libraryID string
	owner     string
	draft     bool
}

func (s *Server) releaseFor(w http.ResponseWriter, r *http.Request) (releaseRow, bool) {
	return s.releaseByID(w, r, r.PathValue("id"))
}

func (s *Server) releaseByID(w http.ResponseWriter, r *http.Request, id string) (releaseRow, bool) {
	rel := releaseRow{id: id}
	v := viewerOf(r)
	if !isLocalID(rel.id) {
		writeError(w, http.StatusNotFound, "release not found")
		return rel, false
	}
	err := s.DB.QueryRow(`SELECT id, coalesce(owner_id::text, ''), draft FROM albums WHERE deezer_id = $1`, rel.id).
		Scan(&rel.libraryID, &rel.owner, &rel.draft)
	switch {
	case errors.Is(err, sql.ErrNoRows) || err == nil && !v.mayEdit(rel.owner) && !v.sees(rel.draft, rel.owner):
		writeError(w, http.StatusNotFound, "release not found")
		return rel, false
	case err != nil:
		serverError(w, r, "query failed", err)
		return rel, false
	case !v.mayEdit(rel.owner):
		writeError(w, http.StatusForbidden, "only the release's author can change it")
		return rel, false
	}
	return rel, true
}

func editorViewer(r *http.Request) viewer {
	v := viewerOf(r)
	v.adminMode = true
	return v
}

type uploadCard struct {
	deezer.Album
	Draft bool   `json:"draft"`
	Owner string `json:"owner,omitempty"`
}

func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	v := viewerOf(r)
	rows, err := s.DB.Query(
		`SELECT a.deezer_id, a.draft, CASE WHEN a.owner_id::text = $1 THEN '' ELSE coalesce(u.username, '?') END
		   FROM albums a LEFT JOIN users u ON u.id = a.owner_id
		  WHERE a.deezer_id LIKE 'k%' AND (a.owner_id::text = $1 OR $2)
		  ORDER BY a.created_at DESC`, v.userID, v.admin)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	var ids []string
	drafts := map[string]bool{}
	owners := map[string]string{}
	for rows.Next() {
		var id, owner string
		var draft bool
		if rows.Scan(&id, &draft, &owner) == nil {
			ids = append(ids, id)
			drafts[id], owners[id] = draft, owner
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	cards, err := s.localAlbumCards(ids)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	out := make([]uploadCard, len(cards))
	for i, a := range cards {
		a.Hidden = false
		out[i] = uploadCard{a, drafts[a.ID], owners[a.ID]}
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out})
}

func (s *Server) handleCreateRelease(w http.ResponseWriter, r *http.Request) {
	if !canUpload(r) {
		writeError(w, http.StatusForbidden, "uploads are not available")
		return
	}
	var id string
	err := s.DB.QueryRow(
		`INSERT INTO albums (deezer_id, title, released_at, record_type, edited, owner_id, draft)
		 VALUES ('k' || nextval('kaikku_ids'), '', current_date, 'single', true, $1, true)
		 RETURNING deezer_id`, currentUser(r)).Scan(&id)
	if err != nil {
		serverError(w, r, "could not create the release", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type uploadTrack struct {
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	DurationMs int                `json:"duration_ms"`
	Explicit   bool               `json:"explicit"`
	Artists    []deezer.ArtistRef `json:"artists"`
	Other      bool               `json:"other,omitempty"`
	Disc       int                `json:"disc"`
}

type releaseState struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Artists     []deezer.ArtistRef `json:"artists"`
	RecordType  string             `json:"record_type"`
	ReleaseDate string             `json:"release_date"`
	CoverURL    string             `json:"cover_url"`
	Draft       bool               `json:"draft"`
	Owner       string             `json:"owner,omitempty"`
	VersionOf   *deezer.Album      `json:"version_of"`
	Tracks      []uploadTrack      `json:"tracks"`
}

func (s *Server) releaseState(r *http.Request, rel releaseRow) (*releaseState, error) {
	album, tracks, _, err := s.localAlbum(editorViewer(r), rel.id)
	if err != nil {
		return nil, err
	}
	st := &releaseState{
		ID: rel.id, Title: album.Title, Artists: album.Contributors, RecordType: album.RecordType,
		ReleaseDate: album.ReleaseDate, CoverURL: album.CoverURL, Draft: rel.draft, Tracks: []uploadTrack{},
	}
	if st.Artists == nil {
		st.Artists = []deezer.ArtistRef{}
	}
	if !viewerOf(r).owns(rel.owner) {
		if s.DB.QueryRow(`SELECT username FROM users WHERE id::text = $1`, rel.owner).Scan(&st.Owner) != nil {
			st.Owner = "?"
		}
	}
	own := map[string]bool{}
	rows, err := s.DB.Query(
		`SELECT o.deezer_id FROM track_overrides o JOIN tracks t ON t.deezer_id = o.deezer_id
		  WHERE t.album_id = $1 AND o.artist_ids IS NOT NULL`, rel.libraryID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			own[id] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, t := range tracks {
		ut := uploadTrack{ID: t.ID, Title: t.Title, DurationMs: t.DurationMs, Explicit: t.Explicit, Disc: max(t.DiscNo, 1)}
		switch {
		case t.AlbumID != rel.id:
			ut.Other = true
			if ut.Artists = t.Contributors; ut.Artists == nil {
				ut.Artists = []deezer.ArtistRef{{ID: t.ArtistID, Name: t.Artist}}
			}
		case own[t.ID]:
			ut.Artists = t.Contributors
		}
		st.Tracks = append(st.Tracks, ut)
	}
	if album.VersionOf != "" {
		if s.albumVisibleTo(viewerOf(r), album.VersionOf) {
			st.VersionOf = s.albumCard(album.VersionOf)
		}
		if st.VersionOf == nil {
			st.VersionOf = &deezer.Album{ID: album.VersionOf, Title: album.VersionOf}
		}
	}
	return st, nil
}

func (s *Server) handleGetRelease(w http.ResponseWriter, r *http.Request) {
	rel, ok := s.releaseFor(w, r)
	if !ok {
		return
	}
	st, err := s.releaseState(r, rel)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type releaseEdit struct {
	Title       string             `json:"title"`
	Artists     []string           `json:"artists"`
	RecordType  string             `json:"record_type"`
	ReleaseDate string             `json:"release_date"`
	Tracks      []releaseTrackEdit `json:"tracks"`
	VersionOf   *string            `json:"version_of"`
	Published   *bool              `json:"published"`
}

type releaseTrackEdit struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Artists  []string `json:"artists"`
	Explicit bool     `json:"explicit"`
	Disc     int      `json:"disc"`
}

const maxTitle = 300

func tooLong(s string) bool { return len([]rune(s)) > maxTitle }

func (s *Server) handleSaveRelease(w http.ResponseWriter, r *http.Request) {
	rel, ok := s.releaseFor(w, r)
	if !ok {
		return
	}
	var req releaseEdit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if tooLong(req.Title) {
		writeError(w, http.StatusBadRequest, "title must be up to 300 characters")
		return
	}
	if req.RecordType != "" && !recordTypes[req.RecordType] {
		writeError(w, http.StatusBadRequest, "record_type must be album, ep, single or compile")
		return
	}
	var released any
	if req.ReleaseDate != "" {
		if _, err := time.Parse("2006-01-02", req.ReleaseDate); err != nil {
			writeError(w, http.StatusBadRequest, "release_date must be YYYY-MM-DD")
			return
		}
		released = req.ReleaseDate
	}
	var artists []string
	if len(req.Artists) > 0 {
		var err error
		if artists, err = s.creditArtists(req.Artists); err != nil {
			writeError(w, http.StatusBadRequest, "artists: "+err.Error())
			return
		}
	}
	if len(req.Tracks) > maxTracklist {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a release can have up to %d tracks", maxTracklist))
		return
	}
	have, err := idsIn(s.DB, `SELECT deezer_id FROM tracks WHERE album_id = $1`, rel.libraryID)
	if err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	other, err := s.otherTracks(viewerOf(r), rel.id, req.Tracks, have)
	if err != nil {
		writeSaveError(w, r, err)
		return
	}
	trackNos := make([]int, len(req.Tracks))
	onDisc := map[int]int{}
	for i, t := range req.Tracks {
		if t.Disc == 0 {
			req.Tracks[i].Disc = 1
		}
		if d := req.Tracks[i].Disc; d < 1 || d > 99 || i > 0 && d < req.Tracks[i-1].Disc {
			writeError(w, http.StatusBadRequest, "discs must be 1 to 99, in order")
			return
		}
		onDisc[req.Tracks[i].Disc]++
		trackNos[i] = onDisc[req.Tracks[i].Disc]
	}
	trackArtists := make([][]string, len(req.Tracks))
	for i, t := range req.Tracks {
		if other[t.ID] {
			continue
		}
		req.Tracks[i].Title = strings.TrimSpace(t.Title)
		if tooLong(t.Title) {
			writeError(w, http.StatusBadRequest, "track titles must be up to 300 characters")
			return
		}
		if t.Artists != nil {
			var err error
			if trackArtists[i], err = s.creditArtists(t.Artists); err != nil {
				writeError(w, http.StatusBadRequest, "track artists: "+err.Error())
				return
			}
		}
	}

	if req.VersionOf != nil {
		if err := s.saveVersionOf(viewerOf(r), rel.libraryID, *req.VersionOf); err != nil {
			writeSaveError(w, r, err)
			return
		}
	}

	tx, err := s.DB.Begin()
	if err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	defer tx.Rollback()
	var cover sql.NullString
	if err := tx.QueryRow(`SELECT cover_key FROM albums WHERE id = $1 FOR UPDATE`, rel.libraryID).Scan(&cover); err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	if have, err = idsIn(tx, `SELECT deezer_id FROM tracks WHERE album_id = $1`, rel.libraryID); err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	sent := make([]string, len(req.Tracks))
	var mine []string
	for i, t := range req.Tracks {
		sent[i] = t.ID
		if !other[t.ID] {
			mine = append(mine, t.ID)
		}
	}
	if !sameSet(have, mine) {
		writeError(w, http.StatusConflict, "the release's tracks changed meanwhile, reload the page")
		return
	}
	var tracklist []string
	var discs []int64
	if len(other) > 0 {
		tracklist = sent
		for _, t := range req.Tracks {
			discs = append(discs, int64(t.Disc))
		}
	}
	var discsArg any
	if discs != nil {
		discsArg = pq.Array(discs)
	}
	if _, err := tx.Exec(
		`UPDATE albums SET title = $2, artist_ids = $3, record_type = NULLIF($4, ''), released_at = $5, tracklist = $6, tracklist_discs = $7
		  WHERE id = $1`,
		rel.libraryID, req.Title, nullArray(artists), req.RecordType, released, nullArray(tracklist), discsArg); err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	for i, t := range req.Tracks {
		if other[t.ID] {
			continue
		}
		if _, err := tx.Exec(`UPDATE tracks SET track_no = $2, disc_no = $3 WHERE deezer_id = $1`,
			t.ID, trackNos[i], req.Tracks[i].Disc); err != nil {
			serverError(w, r, "save failed", err)
			return
		}
		if err := saveOwnTrack(tx, t.ID, req.Tracks[i].Title, t.Explicit, trackArtists[i]); err != nil {
			serverError(w, r, "save failed", err)
			return
		}
	}
	var placeholder string
	defer func() {
		if placeholder != "" {
			s.removeStorageFiles(placeholder)
		}
	}()
	if req.Published != nil {
		if *req.Published {
			if missing := missingForPublish(req, other, artists); len(missing) > 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the release is not complete", "missing": missing})
				return
			}
			if !cover.Valid && s.StorageDir != "" {
				if placeholder, err = s.writePlaceholderCover(rel.id); err != nil {
					serverError(w, r, "save failed", err)
					return
				}
				if _, err := tx.Exec(`UPDATE albums SET cover_key = $2, cover_custom = true WHERE id = $1`, rel.libraryID, placeholder); err != nil {
					serverError(w, r, "save failed", err)
					return
				}
			}
		}
		if _, err := tx.Exec(`UPDATE albums SET draft = $2 WHERE id = $1`, rel.libraryID, !*req.Published); err != nil {
			serverError(w, r, "save failed", err)
			return
		}
		rel.draft = !*req.Published
	}
	if err := tx.Commit(); err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	placeholder = ""
	s.syncLocalSnapshots(rel.id)
	st, err := s.releaseState(r, rel)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func saveOwnTrack(tx *sql.Tx, id, title string, explicit bool, artists []string) error {
	if _, err := tx.Exec(`UPDATE tracks SET title = $2 WHERE deezer_id = $1`, id, title); err != nil {
		return err
	}
	if artists == nil && !explicit {
		_, err := tx.Exec(`DELETE FROM track_overrides WHERE deezer_id = $1`, id)
		return err
	}
	_, err := tx.Exec(
		`INSERT INTO track_overrides (deezer_id, explicit, artist_ids) VALUES ($1, $2, $3)
		 ON CONFLICT (deezer_id) DO UPDATE SET explicit = $2, artist_ids = $3, updated_at = now()`,
		id, explicit, nullArray(artists))
	return err
}

func (s *Server) otherTracks(v viewer, releaseID string, tracks []releaseTrackEdit, have []string) (map[string]bool, error) {
	other := map[string]bool{}
	seen := map[string]bool{}
	for _, t := range tracks {
		if seen[t.ID] {
			return nil, &badRequest{"a track can be on the release only once"}
		}
		seen[t.ID] = true
		if slices.Contains(have, t.ID) {
			continue
		}
		if !validCatalogID(t.ID) {
			return nil, &badRequest{"tracks must be Deezer or kaikku track IDs"}
		}
		got, err := s.catalogTrack(v, t.ID)
		if errors.Is(err, errNotFound) || err == nil && got.AlbumID == releaseID {
			return nil, &badRequest{fmt.Sprintf("track %s not found", t.ID)}
		}
		if err != nil {
			return nil, &badRequest{fmt.Sprintf("track %s not found on Deezer", t.ID)}
		}
		other[t.ID] = true
	}
	return other, nil
}

func missingForPublish(req releaseEdit, other map[string]bool, artists []string) []string {
	missing := []string{}
	if req.Title == "" {
		missing = append(missing, "title")
	}
	if len(artists) == 0 {
		missing = append(missing, "artists")
	}
	if req.RecordType == "" {
		missing = append(missing, "record_type")
	}
	if req.ReleaseDate == "" {
		missing = append(missing, "release_date")
	}
	if len(req.Tracks) == 0 {
		missing = append(missing, "tracks")
	}
	if slices.ContainsFunc(req.Tracks, func(t releaseTrackEdit) bool { return !other[t.ID] && t.Title == "" }) {
		missing = append(missing, "track_titles")
	}
	return missing
}

func nullArray(ids []string) any {
	if len(ids) == 0 {
		return nil
	}
	return pq.Array(ids)
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func idsIn(q querier, query string, args ...any) ([]string, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Server) handleReleaseCover(w http.ResponseWriter, r *http.Request) {
	rel, ok := s.releaseFor(w, r)
	if !ok {
		return
	}
	key, ok := s.saveUploadedImage(w, r)
	if !ok {
		return
	}
	if err := s.setReleaseCover(rel, key, true); err != nil {
		s.removeStorageFiles(key)
		serverError(w, r, "update failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"cover_url": "/media/" + key})
}

func (s *Server) handleDeleteReleaseCover(w http.ResponseWriter, r *http.Request) {
	rel, ok := s.releaseFor(w, r)
	if !ok {
		return
	}
	var next any
	var placeholder string
	if !rel.draft && s.StorageDir != "" {
		var err error
		if placeholder, err = s.writePlaceholderCover(rel.id); err != nil {
			serverError(w, r, "update failed", err)
			return
		}
		next = placeholder
	}
	var old sql.NullString
	err := s.DB.QueryRow(
		`UPDATE albums a SET cover_key = $1::text, cover_custom = $1::text IS NOT NULL
		   FROM (SELECT cover_key FROM albums WHERE id = $2 FOR UPDATE) prev
		  WHERE a.id = $2 RETURNING prev.cover_key`, next, rel.libraryID,
	).Scan(&old)
	if err != nil {
		if placeholder != "" {
			s.removeStorageFiles(placeholder)
		}
		serverError(w, r, "update failed", err)
		return
	}
	if old.Valid && old.String != placeholder {
		s.removeStorageFiles(old.String)
	}
	s.syncLocalSnapshots(rel.id)
	url := ""
	if placeholder != "" {
		url = "/media/" + placeholder
	}
	writeJSON(w, http.StatusOK, map[string]string{"cover_url": url})
}

func (s *Server) setReleaseCover(rel releaseRow, key string, replace bool) error {
	var old sql.NullString
	err := s.DB.QueryRow(
		`UPDATE albums a SET cover_key = $1, cover_custom = true
		   FROM (SELECT cover_key FROM albums WHERE id = $2 FOR UPDATE) prev
		  WHERE a.id = $2 AND ($3 OR a.cover_key IS NULL) RETURNING prev.cover_key`, key, rel.libraryID, replace,
	).Scan(&old)
	if err != nil {
		return err
	}
	s.syncLocalSnapshots(rel.id)
	if old.Valid && old.String != key {
		s.removeStorageFiles(old.String)
	}
	return nil
}

func (s *Server) handleDeleteRelease(w http.ResponseWriter, r *http.Request) {
	rel, ok := s.releaseFor(w, r)
	if !ok {
		return
	}
	keys, err := s.deleteOwn(rel.id, rel.libraryID, "")
	if err != nil {
		serverError(w, r, "delete failed", err)
		return
	}
	s.removeStorageFiles(s.unreferencedAudio(keys...)...)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteOwn(albumID, libraryID, trackID string) ([]string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var cover sql.NullString
	if err := tx.QueryRow(`SELECT cover_key FROM albums WHERE id = $1 FOR UPDATE`, libraryID).Scan(&cover); err != nil {
		return nil, err
	}
	ids, err := idsIn(tx, `SELECT deezer_id FROM tracks WHERE album_id = $1 AND ($2 = '' OR deezer_id = $2)`, libraryID, trackID)
	if err != nil {
		return nil, err
	}
	refs := []string{
		`DELETE FROM liked_tracks WHERE deezer_id = ANY($1)`,
		`DELETE FROM playlist_items WHERE deezer_id = ANY($1)`,
		`DELETE FROM plays WHERE deezer_id = ANY($1)`,
		`DELETE FROM lyrics WHERE deezer_id = ANY($1)`,
		`DELETE FROM track_overrides WHERE deezer_id = ANY($1)`,
		`UPDATE albums a SET tracklist = k.ids, tracklist_discs = k.discs
		   FROM (SELECT al.id,
		                array_agg(x.id ORDER BY x.n) FILTER (WHERE x.id <> ALL($1)) AS ids,
		                CASE WHEN al.tracklist_discs IS NOT NULL
		                     THEN array_agg(al.tracklist_discs[x.n] ORDER BY x.n) FILTER (WHERE x.id <> ALL($1)) END AS discs
		           FROM albums al, unnest(al.tracklist) WITH ORDINALITY x(id, n)
		          WHERE al.tracklist && $1
		          GROUP BY al.id) k
		  WHERE a.id = k.id`,
	}
	for _, q := range refs {
		if _, err := tx.Exec(q, pq.Array(ids)); err != nil {
			return nil, fmt.Errorf("%s: %w", q, err)
		}
	}
	keys, err := idsIn(tx, `DELETE FROM tracks WHERE deezer_id = ANY($1) RETURNING storage_key`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	if trackID == "" {
		for _, q := range []string{
			`DELETE FROM saved_albums WHERE deezer_id = $1`,
			`DELETE FROM plays WHERE context_kind = 'album' AND context_id = $1`,
			`DELETE FROM albums WHERE deezer_id = $1`,
		} {
			if _, err := tx.Exec(q, albumID); err != nil {
				return nil, fmt.Errorf("%s: %w", q, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if trackID == "" && cover.Valid {
		s.removeStorageFiles(cover.String)
	} else {
		s.syncLocalSnapshots(albumID)
	}
	return keys, nil
}

func (s *Server) handleCreateArtist(w http.ResponseWriter, r *http.Request) {
	if !canUpload(r) {
		writeError(w, http.StatusForbidden, "uploads are not available")
		return
	}
	var req artistEdit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validName(req.Name) {
		writeError(w, http.StatusBadRequest, "name is required (up to 300 characters)")
		return
	}
	a := deezer.Artist{Name: strings.TrimSpace(req.Name)}
	if err := s.DB.QueryRow(
		`INSERT INTO artists (deezer_id, name, edited, owner_id) VALUES ('k' || nextval('kaikku_ids'), $1, true, $2)
		 RETURNING deezer_id, id`, a.Name, currentUser(r)).Scan(&a.ID, &a.LibraryID); err != nil {
		serverError(w, r, "could not create the artist", err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) artistEditor(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var owner string
		err := s.DB.QueryRow(`SELECT coalesce(owner_id::text, '') FROM artists WHERE id::text = $1`, r.PathValue("id")).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "artist not found")
			return
		} else if err != nil {
			serverError(w, r, "query failed", err)
			return
		}
		if !viewerOf(r).mayEdit(owner) {
			writeError(w, http.StatusForbidden, "only the artist's creator can change it")
			return
		}
		next(w, r)
	}
}

func (s *Server) syncLocalSnapshots(albumID string) {
	v := viewer{adminMode: true}
	tracks, err := s.localTracks(v, mustIDs(s.DB, `SELECT t.deezer_id FROM tracks t JOIN albums a ON a.id = t.album_id WHERE a.deezer_id = $1`, albumID))
	if err != nil {
		log.Printf("snapshots of %s: %v", albumID, err)
		return
	}
	for _, t := range tracks {
		t.Hidden = false
		meta, _ := json.Marshal(t)
		for _, q := range []string{
			`UPDATE liked_tracks SET meta = $2 WHERE deezer_id = $1`,
			`UPDATE playlist_items SET meta = $2 WHERE deezer_id = $1`,
		} {
			if _, err := s.DB.Exec(q, t.ID, meta); err != nil {
				log.Printf("snapshots of %s: %v", t.ID, err)
			}
		}
	}
	if cards, err := s.localAlbumCards([]string{albumID}); err == nil && len(cards) == 1 {
		cards[0].Hidden = false
		meta, _ := json.Marshal(cards[0])
		if _, err := s.DB.Exec(`UPDATE saved_albums SET meta = $2 WHERE deezer_id = $1`, albumID, meta); err != nil {
			log.Printf("snapshots of %s: %v", albumID, err)
		}
	}
}

func mustIDs(db *sql.DB, query string, args ...any) []string {
	ids, err := idsIn(db, query, args...)
	if err != nil {
		log.Printf("ids: %v", err)
	}
	return ids
}

type ownTrackState struct {
	uploadTrack
	AlbumID        string             `json:"album_id"`
	AlbumTitle     string             `json:"album_title"`
	ReleaseArtists []deezer.ArtistRef `json:"release_artists"`
}

func (s *Server) ownTrackState(r *http.Request, rel releaseRow, trackID string) (*ownTrackState, error) {
	st, err := s.releaseState(r, rel)
	if err != nil {
		return nil, err
	}
	for _, t := range st.Tracks {
		if t.ID == trackID && !t.Other {
			return &ownTrackState{uploadTrack: t, AlbumID: st.ID, AlbumTitle: st.Title, ReleaseArtists: st.Artists}, nil
		}
	}
	return nil, errNotFound
}

func (s *Server) handleGetOwnTrack(w http.ResponseWriter, r *http.Request) {
	rel, trackID, ok := s.ownTrackFor(w, r)
	if !ok {
		return
	}
	st, err := s.ownTrackState(r, rel, trackID)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSaveOwnTrack(w http.ResponseWriter, r *http.Request) {
	rel, trackID, ok := s.ownTrackFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Title    string   `json:"title"`
		Artists  []string `json:"artists"`
		Explicit bool     `json:"explicit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || tooLong(req.Title) {
		writeError(w, http.StatusBadRequest, "title is required (up to 300 characters)")
		return
	}
	var artists []string
	if req.Artists != nil {
		var err error
		if artists, err = s.creditArtists(req.Artists); err != nil {
			writeError(w, http.StatusBadRequest, "artists: "+err.Error())
			return
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	defer tx.Rollback()
	if err := saveOwnTrack(tx, trackID, req.Title, req.Explicit, artists); err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	if err := tx.Commit(); err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	s.syncLocalSnapshots(rel.id)
	st, err := s.ownTrackState(r, rel, trackID)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleGetOwnLyrics(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.ownTrackFor(w, r); !ok {
		return
	}
	var text string
	if s.Lyrics != nil {
		l, err := s.Lyrics.Stored(r.Context(), r.PathValue("id"))
		if err != nil {
			serverError(w, r, "query failed", err)
			return
		}
		text = lyrics.ToText(l)
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) handlePutOwnLyrics(w http.ResponseWriter, r *http.Request) {
	_, trackID, ok := s.ownTrackFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "lyrics must be up to 60 KB of text")
		return
	}
	if s.Lyrics == nil {
		writeError(w, http.StatusServiceUnavailable, "lyrics are off")
		return
	}
	if err := s.saveOwnLyrics(r.Context(), trackID, req.Text); err != nil {
		serverError(w, r, "save failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) saveOwnLyrics(ctx context.Context, trackID, text string) error {
	var ms int
	s.DB.QueryRow(`SELECT duration_ms FROM tracks WHERE deezer_id = $1`, trackID).Scan(&ms)
	return s.Lyrics.Put(ctx, trackID, lyrics.FromText(text, float64(ms)/1000))
}
