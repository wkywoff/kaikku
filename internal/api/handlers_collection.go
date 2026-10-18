package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
	"kaikku/internal/safe"
)

// Личная медиатека (миграция 0009): лайкнутые треки, сохранённые альбомы и
// плейлисты. Клиент присылает только Deezer ID — метаданные сервер берёт
// у Deezer сам и хранит снимком (meta), так что лайкнуть и добавить в
// плейлист можно и нескачанное. Всё добавленное заодно ставится на
// скачивание в фоне: когда человек это включит, оно уже будет на сервере.
//
// Плейлист видит и слушает владелец, а публичный — все пользователи
// сервиса; менять может только владелец. Чужой приватный плейлист — 404,
// как будто его нет.

const maxPlaylistAdd = 200

// --- фоновое скачивание ---

// inBackground runs f off the request; tests replace it to run f inline.
func (s *Server) inBackground(f func()) {
	if s.Background != nil {
		s.Background(f)
		return
	}
	safe.Go("background", f)
}

func (s *Server) downloadTracks(userID string, deezerIDs ...string) {
	s.inBackground(func() {
		for _, id := range deezerIDs {
			if isLocalID(id) {
				continue // свой трек: файл уже есть
			}
			if _, err := s.enqueueTrack(userID, id); err != nil {
				log.Printf("background download deezer:%s: %v", id, err)
			}
		}
	})
}

func (s *Server) downloadAlbum(userID string, album *deezer.Album) {
	s.inBackground(func() {
		tracks, _, err := s.albumTracks(album)
		if err != nil {
			log.Printf("background download album %s: %v", album.ID, err)
			return
		}
		s.Deezer.EnrichContributors(tracks)
		for _, t := range tracks {
			var err error
			if t.AlbumID == album.ID {
				_, err = s.enqueueResolved(userID, t, album)
			} else {
				// Подставлен в треклист с другого релиза (tracklist.go) —
				// качается как трек своего релиза.
				_, err = s.enqueueTrack(userID, t.ID)
			}
			if err != nil {
				log.Printf("background download deezer:%s: %v", t.ID, err)
			}
		}
	})
}

// --- что у меня есть (для кнопок «лайк» и «сохранить» во всех списках) ---

func (s *Server) handleCollectionIDs(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	liked, err := s.idList(`SELECT deezer_id FROM liked_tracks WHERE user_id = $1`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	albums, err := s.idList(`SELECT deezer_id FROM saved_albums WHERE user_id = $1`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	artists, err := s.idList(`SELECT deezer_id FROM followed_artists WHERE user_id = $1`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"liked": liked, "albums": albums, "artists": artists})
}

func (s *Server) idList(query string, args ...any) ([]string, error) {
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// --- лайки ---

func (s *Server) handleLike(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid track id")
		return
	}
	var track *deezer.Track
	var err error
	if isLocalID(id) {
		track, err = s.catalogTrack(viewerOf(r), id)
	} else {
		track, err = s.Deezer.GetTrack(id) // снимок — как у Deezer, правки накладываются при чтении
	}
	if err != nil {
		writeCatalogError(w, "track lookup failed", err)
		return
	}
	meta, _ := json.Marshal(track)
	if _, err := s.DB.Exec(
		`INSERT INTO liked_tracks (user_id, deezer_id, meta) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		userID, id, meta,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not like the song")
		return
	}
	s.downloadTracks(userID, id)
	s.lastfmLove(userID, track, true)
	s.forgetMixes(userID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnlike(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	var meta []byte
	err := s.DB.QueryRow(`DELETE FROM liked_tracks WHERE user_id = $1 AND deezer_id = $2 RETURNING meta`, userID, r.PathValue("id")).Scan(&meta)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not remove the like")
		return
	}
	var track deezer.Track
	if meta != nil && json.Unmarshal(meta, &track) == nil {
		s.lastfmLove(userID, &track, false)
	}
	s.forgetMixes(userID)
	w.WriteHeader(http.StatusNoContent)
}

type collectionTrack struct {
	deezer.Track
	AddedAt time.Time      `json:"added_at"`
	ItemID  int64          `json:"item_id,omitempty"`  // позиция в плейлисте — для удаления
	AddedBy *playlistOwner `json:"added_by,omitempty"` // в плейлисте — кто добавил (совместные)
}

func (s *Server) handleLikedTracks(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	tracks, err := s.collectionTracks(
		`SELECT 0, meta, added_at FROM liked_tracks WHERE user_id = $1 ORDER BY added_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}

// collectionTracks reads (item id, meta, added_at) rows and applies the
// admin's edits to the stored snapshots.
func (s *Server) collectionTracks(query string, args ...any) ([]collectionTrack, error) {
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []collectionTrack{}
	for rows.Next() {
		var ct collectionTrack
		var meta []byte
		if err := rows.Scan(&ct.ItemID, &meta, &ct.AddedAt); err != nil {
			return nil, err
		}
		if json.Unmarshal(meta, &ct.Track) != nil {
			continue
		}
		out = append(out, ct)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.overlayCollection(out)
	return out, nil
}

// overlayCollection applies the admin's edits to the stored snapshots.
func (s *Server) overlayCollection(out []collectionTrack) {
	tracks := make([]deezer.Track, len(out))
	for i := range out {
		tracks[i] = out[i].Track
	}
	s.overlayFor(nil, nil, tracks)
	for i := range out {
		out[i].Track = tracks[i]
	}
}

// --- сохранённые альбомы ---

func (s *Server) handleSaveAlbum(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid album id")
		return
	}
	var album *deezer.Album
	var err error
	if isLocalID(id) {
		album, err = s.catalogAlbum(viewerOf(r), id)
	} else {
		album, err = s.Deezer.GetAlbum(id)
	}
	if err != nil {
		writeCatalogError(w, "album lookup failed", err)
		return
	}
	meta, _ := json.Marshal(album)
	if _, err := s.DB.Exec(
		`INSERT INTO saved_albums (user_id, deezer_id, meta) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		userID, id, meta,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the album")
		return
	}
	if !isLocalID(id) {
		s.downloadAlbum(userID, album)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnsaveAlbum(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	if _, err := s.DB.Exec(`DELETE FROM saved_albums WHERE user_id = $1 AND deezer_id = $2`, userID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove the album")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSavedAlbums(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	rows, err := s.DB.Query(`SELECT meta FROM saved_albums WHERE user_id = $1 ORDER BY added_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	albums := []deezer.Album{}
	for rows.Next() {
		var meta []byte
		var a deezer.Album
		if rows.Scan(&meta) == nil && json.Unmarshal(meta, &a) == nil {
			albums = append(albums, a)
		}
	}
	s.overlayFor(nil, albumPtrs(albums), nil)
	writeJSON(w, http.StatusOK, map[string]any{"albums": albums})
}

// --- плейлисты ---

type playlistOwner struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type playlistInfo struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	IsPublic    bool          `json:"is_public"`
	Owner       playlistOwner `json:"owner"`
	Mine        bool          `json:"mine"`
	TrackCount  int           `json:"track_count"`
	DurationMs  int64         `json:"duration_ms"`
	// CanEdit — может добавлять, убирать и переставлять треки: владелец или
	// соавтор (playlist_collab.go). Collaborative — у плейлиста есть соавторы.
	CanEdit       bool            `json:"can_edit"`
	Collaborative bool            `json:"collaborative"`
	Collaborators []playlistOwner `json:"collaborators,omitempty"` // только у одного плейлиста (handleGetPlaylist)
	CreatedAt     time.Time       `json:"created_at"`
	Covers        []string        `json:"covers"`       // своя обложка или мозаика (Mosaic)
	Mosaic        []string        `json:"mosaic"`       // до 4 разных обложек треков
	CustomCover   bool            `json:"custom_cover"` // владелец загрузил свою
	UpdatedAt     time.Time       `json:"updated_at"`
}

// playlistSelect — общая часть списка и одного плейлиста ($1 — текущий пользователь).
const playlistSelect = `
	SELECT p.id, p.title, p.description, p.is_public, u.id, u.username, p.user_id = $1,
	       (SELECT count(*) FROM playlist_items i WHERE i.playlist_id = p.id),
	       (SELECT COALESCE(sum((i.meta->>'duration_ms')::bigint), 0) FROM playlist_items i WHERE i.playlist_id = p.id),
	       ARRAY(SELECT c FROM (
	             SELECT DISTINCT ON (i.meta->>'cover_url') i.meta->>'cover_url' AS c, i.position
	               FROM playlist_items i
	              WHERE i.playlist_id = p.id AND COALESCE(i.meta->>'cover_url', '') <> ''
	              ORDER BY i.meta->>'cover_url', i.position) x
	             ORDER BY x.position LIMIT 4),
	       p.cover_key, p.updated_at, p.created_at,
	       p.user_id = $1 OR EXISTS (SELECT 1 FROM playlist_collaborators c WHERE c.playlist_id = p.id AND c.user_id = $1),
	       EXISTS (SELECT 1 FROM playlist_collaborators c WHERE c.playlist_id = p.id)
	  FROM playlists p JOIN users u ON u.id = p.user_id`

// playlistVisibleTo — условие «плейлист виден $1»: свой, публичный или он в соавторах.
const playlistVisibleTo = `(p.user_id = $1 OR p.is_public OR EXISTS (SELECT 1 FROM playlist_collaborators c WHERE c.playlist_id = p.id AND c.user_id = $1))`

func scanPlaylist(row interface{ Scan(...any) error }) (playlistInfo, error) {
	var p playlistInfo
	var coverKey sql.NullString
	err := row.Scan(&p.ID, &p.Title, &p.Description, &p.IsPublic, &p.Owner.ID, &p.Owner.Username, &p.Mine,
		&p.TrackCount, &p.DurationMs, pq.Array(&p.Mosaic), &coverKey, &p.UpdatedAt, &p.CreatedAt, &p.CanEdit, &p.Collaborative)
	if p.Mosaic == nil {
		p.Mosaic = []string{}
	}
	p.Covers = p.Mosaic
	if coverKey.Valid {
		p.CustomCover = true
		p.Covers = []string{"/media/" + coverKey.String} // как скачанные обложки: nginx отдаёт /media/covers/ без подписи
	}
	return p, err
}

// playlistListedFor — плейлисты в списке человека: свои, соавторские и
// публичные плейлисты друзей (подписаны друг на друга). Публичный плейлист
// не-друга по-прежнему открывается по ссылке и виден в профиле
// (playlistVisibleTo), но в списке — на главной и в приложениях — его нет.
const playlistListedFor = `(p.user_id = $1
	OR EXISTS (SELECT 1 FROM playlist_collaborators c WHERE c.playlist_id = p.id AND c.user_id = $1)
	OR (p.is_public
	    AND EXISTS (SELECT 1 FROM user_follows f WHERE f.follower_id = $1 AND f.followee_id = p.user_id)
	    AND EXISTS (SELECT 1 FROM user_follows f WHERE f.follower_id = p.user_id AND f.followee_id = $1)))`

// handlePlaylists lists my playlists and my friends' public ones.
func (s *Server) handlePlaylists(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	rows, err := s.DB.Query(playlistSelect+` WHERE `+playlistListedFor+` ORDER BY p.updated_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	mine, others := []playlistInfo{}, []playlistInfo{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if p.Mine || p.CanEdit { // совместный — тоже «мой»: в него добавляют из меню трека
			mine = append(mine, p)
		} else {
			others = append(others, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mine": mine, "others": others})
}

// visiblePlaylist loads a playlist the user may see (own, public or one
// they collaborate on).
func (s *Server) visiblePlaylist(userID, id string) (playlistInfo, error) {
	return scanPlaylist(s.DB.QueryRow(playlistSelect+` WHERE p.id::text = $2 AND `+playlistVisibleTo, userID, id))
}

func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	p, err := s.visiblePlaylist(userID, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "playlist not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	tracks, err := s.playlistTracks(p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if p.Collaborators, err = s.playlistCollaborators(p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlist": p, "tracks": tracks})
}

type playlistFields struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	IsPublic    *bool   `json:"is_public"`
}

func (f playlistFields) valid() bool {
	if f.Title != nil && !validName(*f.Title) {
		return false
	}
	return f.Description == nil || len(*f.Description) <= 1000
}

func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	var f playlistFields
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil || f.Title == nil || !f.valid() {
		writeError(w, http.StatusBadRequest, "a title of 1–300 characters is required")
		return
	}
	desc, public := "", false
	if f.Description != nil {
		desc = strings.TrimSpace(*f.Description)
	}
	if f.IsPublic != nil {
		public = *f.IsPublic
	}
	var id string
	if err := s.DB.QueryRow(
		`INSERT INTO playlists (user_id, title, description, is_public) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, strings.TrimSpace(*f.Title), desc, public,
	).Scan(&id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the playlist")
		return
	}
	p, _ := s.visiblePlaylist(userID, id)
	writeJSON(w, http.StatusCreated, p)
}

// ownPlaylist answers false (and writes an error) unless the playlist is the
// user's: renaming, the cover and deleting are the owner's alone. A
// collaborator gets 403, anyone else 404 — as if there were no playlist.
func (s *Server) ownPlaylist(w http.ResponseWriter, userID, id string) bool {
	return s.playlistAccess(w, userID, id, false)
}

// editablePlaylist — the user may add, remove and reorder songs: the owner
// or a collaborator (playlist_collab.go).
func (s *Server) editablePlaylist(w http.ResponseWriter, userID, id string) bool {
	return s.playlistAccess(w, userID, id, true)
}

func (s *Server) playlistAccess(w http.ResponseWriter, userID, id string, collaboratorOK bool) bool {
	var owner, collaborator bool
	err := s.DB.QueryRow(
		`SELECT p.user_id = $2, EXISTS (SELECT 1 FROM playlist_collaborators c WHERE c.playlist_id = p.id AND c.user_id = $2)
		   FROM playlists p WHERE p.id::text = $1`, id, userID).Scan(&owner, &collaborator)
	switch {
	case errors.Is(err, sql.ErrNoRows) || err == nil && !owner && !collaborator:
		writeError(w, http.StatusNotFound, "playlist not found")
		return false
	case err != nil:
		writeError(w, http.StatusInternalServerError, "query failed")
		return false
	case !owner && !collaboratorOK:
		writeError(w, http.StatusForbidden, "only the owner can change this")
		return false
	}
	return true
}

func (s *Server) handleUpdatePlaylist(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	var f playlistFields
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil || !f.valid() {
		writeError(w, http.StatusBadRequest, "invalid playlist fields")
		return
	}
	if !s.ownPlaylist(w, userID, id) {
		return
	}
	var title, desc any
	if f.Title != nil {
		title = strings.TrimSpace(*f.Title)
	}
	if f.Description != nil {
		desc = strings.TrimSpace(*f.Description)
	}
	if _, err := s.DB.Exec(
		`UPDATE playlists SET title = COALESCE($2, title), description = COALESCE($3, description),
		        is_public = COALESCE($4, is_public), updated_at = now()
		  WHERE id = $1`, id, title, desc, f.IsPublic,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update the playlist")
		return
	}
	p, _ := s.visiblePlaylist(userID, id)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	if !s.ownPlaylist(w, userID, id) {
		return
	}
	var cover sql.NullString
	if err := s.DB.QueryRow(`DELETE FROM playlists WHERE id = $1 RETURNING cover_key`, id).Scan(&cover); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete the playlist")
		return
	}
	if cover.Valid {
		s.removeStorageFiles(cover.String)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- своя обложка ---

// handleUploadPlaylistCover: JPEG, как у картинок админа (saveUploadedImage).
func (s *Server) handleUploadPlaylistCover(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	if !s.ownPlaylist(w, userID, id) {
		return
	}
	key, ok := s.saveUploadedImage(w, r)
	if !ok {
		return
	}
	s.setPlaylistCover(w, userID, id, &key)
}

func (s *Server) handleDeletePlaylistCover(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	if !s.ownPlaylist(w, userID, id) {
		return
	}
	s.setPlaylistCover(w, userID, id, nil)
}

// setPlaylistCover stores the new cover (nil — back to the mosaic), deletes
// the previous file and answers with the playlist.
func (s *Server) setPlaylistCover(w http.ResponseWriter, userID, id string, key *string) {
	var old sql.NullString
	err := s.DB.QueryRow(
		`UPDATE playlists p SET cover_key = $2, updated_at = now()
		   FROM (SELECT cover_key FROM playlists WHERE id = $1) prev
		  WHERE p.id = $1 RETURNING prev.cover_key`, id, key,
	).Scan(&old)
	if err != nil {
		if key != nil {
			s.removeStorageFiles(*key)
		}
		writeError(w, http.StatusInternalServerError, "could not update the cover")
		return
	}
	if old.Valid && (key == nil || old.String != *key) {
		s.removeStorageFiles(old.String)
	}
	p, err := s.visiblePlaylist(userID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleAddToPlaylist appends songs ({"deezer_ids": [...]}) in the given
// order and queues them for download, unless "download": false (an import
// of a whole playlist — those download when played).
func (s *Server) handleAddToPlaylist(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	var req struct {
		DeezerIDs []string `json:"deezer_ids"`
		Download  *bool    `json:"download"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.DeezerIDs) == 0 || len(req.DeezerIDs) > maxPlaylistAdd {
		writeError(w, http.StatusBadRequest, "deezer_ids: 1–200 song IDs are required")
		return
	}
	for _, dz := range req.DeezerIDs {
		if !validCatalogID(dz) {
			writeError(w, http.StatusBadRequest, "deezer_ids must be song IDs")
			return
		}
	}
	if !s.editablePlaylist(w, userID, id) {
		return
	}
	// Deezer — параллельно, до 200 треков за раз; свои — из базы.
	tracks, err := s.catalogTracks(viewerOf(r), req.DeezerIDs)
	if err == nil && len(tracks) != len(req.DeezerIDs) {
		err = errNotFound
	}
	if err != nil {
		writeCatalogError(w, "track lookup failed", err)
		return
	}
	metas := make([][]byte, len(tracks))
	for i, track := range tracks {
		metas[i], _ = json.Marshal(track)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add the songs")
		return
	}
	defer tx.Rollback()
	var last float64
	// FOR UPDATE: два одновременных добавления не получат одни и те же позиции.
	if err := tx.QueryRow(`SELECT 1 FROM playlists WHERE id = $1 FOR UPDATE`, id).Scan(new(int)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not add the songs")
		return
	}
	tx.QueryRow(`SELECT COALESCE(max(position), 0) FROM playlist_items WHERE playlist_id = $1`, id).Scan(&last)
	for i, dz := range req.DeezerIDs {
		if _, err := tx.Exec(`INSERT INTO playlist_items (playlist_id, position, deezer_id, meta, added_by) VALUES ($1, $2, $3, $4, $5)`,
			id, last+float64(i+1), dz, metas[i], userID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not add the songs")
			return
		}
	}
	tx.Exec(`UPDATE playlists SET updated_at = now() WHERE id = $1`, id)
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not add the songs")
		return
	}
	if req.Download == nil || *req.Download {
		s.downloadTracks(userID, req.DeezerIDs...)
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": len(req.DeezerIDs)})
}

// --- порядок треков ---

var errPositionsTooClose = errors.New("positions too close")

// handleMovePlaylistItem puts a song before another one ({"before": item id})
// or at the end ({"before": null}). Positions are floats: the song gets the
// midpoint between its new neighbours, so one move is one UPDATE; when the
// neighbours are too close to split, the playlist is renumbered 1…n first.
func (s *Server) handleMovePlaylistItem(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	itemID, err := strconv.ParseInt(r.PathValue("item"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	var req struct {
		Before *int64 `json:"before"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Before != nil && *req.Before == itemID) {
		writeError(w, http.StatusBadRequest, `"before" must be another song of the playlist or null`)
		return
	}
	if !s.editablePlaylist(w, userID, id) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not move the song")
		return
	}
	defer tx.Rollback()
	// Как при добавлении: перестановки одного плейлиста идут по очереди.
	if err := tx.QueryRow(`SELECT 1 FROM playlists WHERE id = $1 FOR UPDATE`, id).Scan(new(int)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not move the song")
		return
	}
	var found bool
	err = tx.QueryRow(`SELECT true FROM playlist_items WHERE id = $1 AND playlist_id = $2`, itemID, id).Scan(&found)
	if err == nil {
		var pos float64
		pos, err = newItemPosition(tx, id, itemID, req.Before)
		if errors.Is(err, errPositionsTooClose) {
			if _, err = tx.Exec(
				`UPDATE playlist_items i SET position = n.rn
				   FROM (SELECT id, row_number() OVER (ORDER BY position, id) AS rn
				           FROM playlist_items WHERE playlist_id = $1) n
				  WHERE i.id = n.id`, id); err == nil {
				pos, err = newItemPosition(tx, id, itemID, req.Before)
			}
		}
		if err == nil {
			_, err = tx.Exec(`UPDATE playlist_items SET position = $2 WHERE id = $1`, itemID, pos)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "song not in this playlist")
		return
	}
	if err == nil {
		_, err = tx.Exec(`UPDATE playlists SET updated_at = now() WHERE id = $1`, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not move the song")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newItemPosition is the position for an item placed before `before` (nil —
// at the end), not counting the item itself. The list is ordered by
// (position, id). sql.ErrNoRows — `before` is not in this playlist.
func newItemPosition(tx *sql.Tx, playlistID string, itemID int64, before *int64) (float64, error) {
	if before == nil {
		var last float64
		err := tx.QueryRow(`SELECT COALESCE(max(position), 0) FROM playlist_items WHERE playlist_id = $1 AND id <> $2`,
			playlistID, itemID).Scan(&last)
		return last + 1, err
	}
	var next float64
	if err := tx.QueryRow(`SELECT position FROM playlist_items WHERE id = $1 AND playlist_id = $2`,
		*before, playlistID).Scan(&next); err != nil {
		return 0, err
	}
	var prev float64
	err := tx.QueryRow(
		`SELECT position FROM playlist_items
		  WHERE playlist_id = $1 AND id <> $2 AND (position, id) < ($3, $4)
		  ORDER BY position DESC, id DESC LIMIT 1`, playlistID, itemID, next, *before,
	).Scan(&prev)
	if errors.Is(err, sql.ErrNoRows) {
		return next - 1, nil // в самое начало
	} else if err != nil {
		return 0, err
	}
	if next-prev < 1e-9 {
		return 0, errPositionsTooClose
	}
	return (prev + next) / 2, nil
}

func (s *Server) handleRemoveFromPlaylist(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r)
	id := r.PathValue("id")
	itemID, err := strconv.ParseInt(r.PathValue("item"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	if !s.editablePlaylist(w, userID, id) {
		return
	}
	res, err := s.DB.Exec(`DELETE FROM playlist_items WHERE id = $1 AND playlist_id = $2`, itemID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove the song")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "song not in this playlist")
		return
	}
	s.DB.Exec(`UPDATE playlists SET updated_at = now() WHERE id = $1`, id)
	w.WriteHeader(http.StatusNoContent)
}
