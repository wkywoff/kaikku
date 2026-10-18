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

// Загрузка своих релизов — того, чего нет в Deezer. Релиз заводится
// черновиком (его видят автор и админ), в него загружаются треки
// (uploads_audio.go), заполняются название, исполнители, тип, дата и
// обложка, и он публикуется — тогда его видят все, как релиз Deezer
// (local.go). ID у своих артистов, релизов и треков — «k…» (helpers.go).
//
// Загружать может любой пользователь (canUpload), править релиз — его автор
// и админ, своего артиста (имя, фото) — тот, кто его завёл, и админ.

// canUpload — кто может загружать: с 16.10.2026 — все пользователи
// (сначала был только админ). Закрыть загрузку — поменять здесь.
func canUpload(r *http.Request) bool {
	return currentUser(r) != ""
}

// releaseRow — свой релиз, который зритель вправе править.
type releaseRow struct {
	id        string // k-ID
	libraryID string
	owner     string
	draft     bool
}

// releaseFor loads an own release the viewer may edit, answering 404 (no
// such release, or a stranger's draft) or 403 itself when not.
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
		writeError(w, http.StatusInternalServerError, "query failed")
		return rel, false
	case !v.mayEdit(rel.owner):
		writeError(w, http.StatusForbidden, "only the release's author can change it")
		return rel, false
	}
	return rel, true
}

// editorViewer — автор или админ видит черновик, который правит.
func editorViewer(r *http.Request) viewer {
	v := viewerOf(r)
	v.adminMode = true
	return v
}

// --- список и черновик ---

type uploadCard struct {
	deezer.Album
	Draft bool   `json:"draft"`
	Owner string `json:"owner,omitempty"` // кто загрузил — админу у чужих релизов
}

// handleUploads — мои релизы (с черновиками), новые сверху; админу — все
// загруженные, и без режима админа: это его рабочий список.
func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	v := viewerOf(r)
	rows, err := s.DB.Query(
		`SELECT a.deezer_id, a.draft, CASE WHEN a.owner_id::text = $1 THEN '' ELSE coalesce(u.username, '?') END
		   FROM albums a LEFT JOIN users u ON u.id = a.owner_id
		  WHERE a.deezer_id LIKE 'k%' AND (a.owner_id::text = $1 OR $2)
		  ORDER BY a.created_at DESC`, v.userID, v.admin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
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
	cards, err := s.localAlbumCards(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	out := make([]uploadCard, len(cards))
	for i, a := range cards {
		a.Hidden = false
		out[i] = uploadCard{a, drafts[a.ID], owners[a.ID]}
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": out})
}

// handleCreateRelease заводит пустой черновик: сегодняшняя дата, тип
// «сингл» (фронт поправит по числу треков). Треки и всё остальное — потом.
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
		writeError(w, http.StatusInternalServerError, "could not create the release")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// --- состояние окна загрузки ---

type uploadTrack struct {
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	DurationMs int                `json:"duration_ms"`
	Explicit   bool               `json:"explicit"`
	Artists    []deezer.ArtistRef `json:"artists"` // null — как у релиза
}

type releaseState struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Artists     []deezer.ArtistRef `json:"artists"`
	RecordType  string             `json:"record_type"`
	ReleaseDate string             `json:"release_date"`
	CoverURL    string             `json:"cover_url"`
	Draft       bool               `json:"draft"`
	Owner       string             `json:"owner,omitempty"` // кто загрузил, если правит не он (админ)
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
	own := map[string]bool{} // у каких треков свои исполнители (track_overrides)
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
	for _, t := range tracks {
		ut := uploadTrack{ID: t.ID, Title: t.Title, DurationMs: t.DurationMs, Explicit: t.Explicit}
		if own[t.ID] {
			ut.Artists = t.Contributors
		}
		st.Tracks = append(st.Tracks, ut)
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
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// --- сохранение и публикация ---

type releaseEdit struct {
	Title       string             `json:"title"`
	Artists     []string           `json:"artists"` // ID по порядку, основной первый: свои (k…) и Deezer
	RecordType  string             `json:"record_type"`
	ReleaseDate string             `json:"release_date"` // YYYY-MM-DD или ""
	Tracks      []releaseTrackEdit `json:"tracks"`       // все треки релиза, по порядку
	Published   *bool              `json:"published"`    // true — опубликовать, false — снять; нет — не менять
}

type releaseTrackEdit struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Artists  []string `json:"artists"` // null — как у релиза
	Explicit bool     `json:"explicit"`
}

const maxTitle = 300

func tooLong(s string) bool { return len([]rune(s)) > maxTitle }

// handleSaveRelease сохраняет всё окно разом. Черновик можно сохранить
// недозаполненным; публикация проверяет, что есть всё нужное, и иначе
// отвечает 400 со списком missing — фронт подсвечивает эти поля.
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
	trackArtists := make([][]string, len(req.Tracks))
	for i, t := range req.Tracks {
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

	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	defer tx.Rollback()
	var cover sql.NullString
	if err := tx.QueryRow(`SELECT cover_key FROM albums WHERE id = $1 FOR UPDATE`, rel.libraryID).Scan(&cover); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	have, err := idsIn(tx, `SELECT deezer_id FROM tracks WHERE album_id = $1`, rel.libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	sent := make([]string, len(req.Tracks))
	for i, t := range req.Tracks {
		sent[i] = t.ID
	}
	if !sameSet(have, sent) {
		writeError(w, http.StatusConflict, "the release's tracks changed meanwhile, reload the page")
		return
	}
	if _, err := tx.Exec(
		`UPDATE albums SET title = $2, artist_ids = $3, record_type = NULLIF($4, ''), released_at = $5 WHERE id = $1`,
		rel.libraryID, req.Title, nullArray(artists), req.RecordType, released); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	for i, t := range req.Tracks {
		if _, err := tx.Exec(`UPDATE tracks SET title = $2, track_no = $3, disc_no = 1 WHERE deezer_id = $1`,
			t.ID, t.Title, i+1); err != nil {
			writeError(w, http.StatusInternalServerError, "save failed")
			return
		}
		if trackArtists[i] == nil && !t.Explicit {
			_, err = tx.Exec(`DELETE FROM track_overrides WHERE deezer_id = $1`, t.ID)
		} else {
			_, err = tx.Exec(
				`INSERT INTO track_overrides (deezer_id, explicit, artist_ids) VALUES ($1, $2, $3)
				 ON CONFLICT (deezer_id) DO UPDATE SET explicit = $2, artist_ids = $3, updated_at = now()`,
				t.ID, t.Explicit, nullArray(trackArtists[i]))
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "save failed")
			return
		}
	}
	if req.Published != nil {
		if *req.Published {
			if missing := missingForPublish(req, artists, cover.Valid); len(missing) > 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the release is not complete", "missing": missing})
				return
			}
		}
		if _, err := tx.Exec(`UPDATE albums SET draft = $2 WHERE id = $1`, rel.libraryID, !*req.Published); err != nil {
			writeError(w, http.StatusInternalServerError, "save failed")
			return
		}
		rel.draft = !*req.Published
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	s.syncLocalSnapshots(rel.id)
	st, err := s.releaseState(r, rel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// missingForPublish — чего не хватает для публикации (те же коды проверяет
// фронт, lib/uploads.js).
func missingForPublish(req releaseEdit, artists []string, hasCover bool) []string {
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
	if !hasCover {
		missing = append(missing, "cover")
	}
	if len(req.Tracks) == 0 {
		missing = append(missing, "tracks")
	}
	if slices.ContainsFunc(req.Tracks, func(t releaseTrackEdit) bool { return t.Title == "" }) {
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

// --- обложка ---

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
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"cover_url": "/media/" + key})
}

// setReleaseCover ставит обложку релиза; replace=false — только если её ещё
// нет (обложка из тегов файла не заменяет выбранную). Прежний файл и снимки
// в медиатеке слушателей обновляются.
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

// --- удаление ---

// handleDeleteRelease удаляет релиз отовсюду: треки, их файлы и обложку,
// лайки, места в плейлистах, сохранения, историю, тексты. Мягкий вариант —
// снять с публикации (handleSaveRelease, published:false).
func (s *Server) handleDeleteRelease(w http.ResponseWriter, r *http.Request) {
	rel, ok := s.releaseFor(w, r)
	if !ok {
		return
	}
	keys, err := s.deleteOwn(rel.id, rel.libraryID, "")
	if err != nil {
		log.Printf("delete release %s: %v", rel.id, err)
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.removeStorageFiles(s.unreferencedAudio(keys...)...)
	w.WriteHeader(http.StatusNoContent)
}

// deleteOwn удаляет свой релиз (trackID == "") или один его трек вместе со
// всем, что на них ссылается, и возвращает ключи файлов — их удаляют после
// коммита. Обложка релиза удаляется сразу.
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

// --- свои артисты ---

// handleCreateArtist заводит своего артиста (только имя; фото — как у
// любого артиста, PUT /api/artists/{id}/picture).
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
		writeError(w, http.StatusInternalServerError, "could not create the artist")
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

// artistEditor пускает к правке артиста (имя, фото) админа и того, кто
// завёл своего артиста.
func (s *Server) artistEditor(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var owner string
		err := s.DB.QueryRow(`SELECT coalesce(owner_id::text, '') FROM artists WHERE id::text = $1`, r.PathValue("id")).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "artist not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if !viewerOf(r).mayEdit(owner) {
			writeError(w, http.StatusForbidden, "only the artist's creator can change it")
			return
		}
		next(w, r)
	}
}

// --- снимки в медиатеке ---

// syncLocalSnapshots переписывает снимки своего релиза и его треков в
// лайках, плейлистах и сохранённых альбомах: названия и исполнители и так
// накладываются при чтении, а обложку и длительность плейлист берёт прямо
// из снимка (мозаика, общее время).
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

// --- тексты своих треков ---

// handleGetOwnLyrics — текст своего трека для окна загрузки: как его
// вводили (LRC, если есть время).
func (s *Server) handleGetOwnLyrics(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.ownTrackFor(w, r); !ok {
		return
	}
	var text string
	if s.Lyrics != nil {
		l, err := s.Lyrics.Stored(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		text = lyrics.ToText(l)
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

// handlePutOwnLyrics сохраняет текст своего трека: {"text": "..."} — LRC
// или просто строки; пустой — убрать.
func (s *Server) handlePutOwnLyrics(w http.ResponseWriter, r *http.Request) {
	rel, trackID, ok := s.ownTrackFor(w, r)
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
		log.Printf("lyrics of %s (%s): %v", trackID, rel.id, err)
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) saveOwnLyrics(ctx context.Context, trackID, text string) error {
	var ms int
	s.DB.QueryRow(`SELECT duration_ms FROM tracks WHERE deezer_id = $1`, trackID).Scan(&ms)
	return s.Lyrics.Put(ctx, trackID, lyrics.FromText(text, float64(ms)/1000))
}
