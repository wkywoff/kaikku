package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
)

// enqueueTrack resolves a client-supplied Deezer track ID (the client never
// sends metadata, only the ID) plus its album, and enqueues it. The dedup
// check runs first, before any Deezer request. sourceRef is built once from
// the requested ID and used for both the check and the insert.
func (s *Server) enqueueTrack(userID, deezerTrackID string) (map[string]any, error) {
	if !validDeezerID(deezerTrackID) {
		return nil, errLocalID
	}
	sourceRef := "deezer:" + deezerTrackID
	if existing, err := s.existingJob(sourceRef); err != nil || existing != nil {
		return existing, err
	}

	track, err := s.Deezer.GetTrack(deezerTrackID)
	if err != nil {
		return nil, err
	}
	album, err := s.Deezer.GetAlbum(track.AlbumID)
	if err != nil {
		return nil, err
	}
	return s.insertIngestJob(userID, sourceRef, *track, album)
}

// enqueueResolved enqueues a track whose metadata already came from Deezer
// (an album tracklist), so track.ID is authoritative for sourceRef.
func (s *Server) enqueueResolved(userID string, track deezer.Track, album *deezer.Album) (map[string]any, error) {
	if !validDeezerID(track.ID) {
		return nil, errLocalID
	}
	sourceRef := "deezer:" + track.ID
	if existing, err := s.existingJob(sourceRef); err != nil || existing != nil {
		return existing, err
	}
	return s.insertIngestJob(userID, sourceRef, track, album)
}

func (s *Server) insertIngestJob(userID, sourceRef string, track deezer.Track, album *deezer.Album) (map[string]any, error) {
	var jobID int64
	err := s.DB.QueryRow(
		`INSERT INTO ingest_jobs
		   (requested_by, source_ref, title, artist_name, artist_deezer_id,
		    album_artist_name, album_artist_deezer_id, album_title, album_deezer_id,
		    track_no, disc_no, duration_ms, cover_url, release_date, album_record_type, other_artists, isrc)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		 ON CONFLICT (source_ref) DO NOTHING
		 RETURNING id`,
		nullableString(userID), sourceRef, track.Title, track.Artist, nullableString(track.ArtistID),
		album.ArtistName, nullableString(album.ArtistID), album.Title, nullableString(album.ID),
		nullablePositive(track.TrackNo), nullablePositive(track.DiscNo), track.DurationMs,
		nullableString(album.CoverURL), nullableDate(album.ReleaseDate), nullableString(album.RecordType),
		pq.Array(otherArtists(track)), nullableString(track.ISRC),
	).Scan(&jobID)

	switch {
	case err == sql.ErrNoRows:
		// Гонка: кто-то поставил в очередь этот же трек между dedup-проверкой и INSERT.
		existing, lookupErr := s.existingJob(sourceRef)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("race on ingest, retry")
	case err != nil:
		return nil, err
	}

	return map[string]any{"job_id": jobID, "status": "queued"}, nil
}

// otherArtists returns the track's artists other than the main one.
func otherArtists(track deezer.Track) []string {
	out := []string{} // не nil: pq.Array(nil) — это NULL, а колонка NOT NULL
	for _, c := range track.Contributors {
		if c.Name != "" && c.Name != track.Artist && !slices.Contains(out, c.Name) {
			out = append(out, c.Name)
		}
	}
	return out
}

// currentOtherArtists re-reads the featured artists of a job's track from
// Deezer before a retry: jobs queued before migration 0008 don't have them,
// and without them YouTube may never find the track. nil (keep what the
// job has) if Deezer is unavailable.
func (s *Server) currentOtherArtists(jobID int64) []string {
	var sourceRef string
	if err := s.DB.QueryRow(`SELECT source_ref FROM ingest_jobs WHERE id = $1`, jobID).Scan(&sourceRef); err != nil {
		return nil
	}
	deezerID, ok := strings.CutPrefix(sourceRef, "deezer:")
	if !ok {
		return nil
	}
	track, err := s.Deezer.GetTrack(deezerID)
	if err != nil {
		return nil
	}
	return otherArtists(*track)
}

func statusFor(result map[string]any) int {
	if s, ok := result["status"].(string); ok && s == "duplicate" {
		return http.StatusOK
	}
	return http.StatusAccepted
}

// existingJob returns nil if sourceRef was never queued. A job that failed,
// or finished but whose track was since deleted, is put back into the queue
// (re-adding is the natural way to retry); anything else is a duplicate.
func (s *Server) existingJob(sourceRef string) (map[string]any, error) {
	var jobID int64
	var status string
	var resultTrackID sql.NullString
	err := s.DB.QueryRow(
		`SELECT id, status, result_track_id FROM ingest_jobs WHERE source_ref = $1`, sourceRef,
	).Scan(&jobID, &status, &resultTrackID)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if status == "failed" || (status == "done" && !resultTrackID.Valid) {
		requeued, err := s.requeueJob(jobID, status)
		if err != nil {
			return nil, err
		}
		if requeued {
			return map[string]any{"job_id": jobID, "status": "queued"}, nil
		}
		// Статус успел смениться параллельно — отдаём актуальный.
		return s.existingJob(sourceRef)
	}

	resp := map[string]any{"status": "duplicate", "job_id": jobID, "job_status": status}
	if resultTrackID.Valid {
		resp["track_id"] = resultTrackID.String
	}
	return resp, nil
}

// requeueJob moves a job from fromStatus back to pending; false if the job
// was no longer in fromStatus.
func (s *Server) requeueJob(jobID int64, fromStatus string) (bool, error) {
	var others any // NULL — оставить как есть
	if o := s.currentOtherArtists(jobID); o != nil {
		others = pq.Array(o)
	}
	res, err := s.DB.Exec(
		`UPDATE ingest_jobs
		    SET status = 'pending', error = NULL, result_track_id = NULL, video_id = NULL, updated_at = now(),
		        other_artists = COALESCE($3, other_artists)
		  WHERE id = $1 AND status = $2`,
		jobID, fromStatus, others,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// handleIngestLookup reports what the library already has for a list of
// Deezer track IDs (?ids=1,2,3), so Search/Discover can show "в
// библиотеке" / "в очереди" instead of an "Добавить" button. IDs that were
// never queued are absent from the response.
//
//	{"<deezer id>": {"job_id": N, "job_status": "pending|processing|done|failed", "track_id": "<uuid>"?}}
func (s *Server) handleIngestLookup(w http.ResponseWriter, r *http.Request) {
	raw := strings.Split(r.URL.Query().Get("ids"), ",")
	if len(raw) > 500 {
		writeError(w, http.StatusBadRequest, "too many ids")
		return
	}
	refs := make([]string, 0, len(raw))
	var own []string
	for _, id := range raw {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if isLocalID(id) {
			own = append(own, id)
			continue
		}
		if !validDeezerID(id) {
			writeError(w, http.StatusBadRequest, "ids must be song ids")
			return
		}
		refs = append(refs, "deezer:"+id)
	}
	result := map[string]any{}
	if len(own) > 0 {
		// Свои треки (загрузка) всегда в медиатеке — job'а у них нет.
		visible, err := s.localTracks(viewerOf(r), own)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		ids := make([]string, len(visible))
		for i, t := range visible {
			ids[i] = t.ID
		}
		rows, err := s.DB.Query(`SELECT deezer_id, id FROM tracks WHERE deezer_id = ANY($1)`, pq.Array(ids))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		for rows.Next() {
			var dz, trackID string
			if rows.Scan(&dz, &trackID) == nil {
				result[dz] = map[string]any{"job_status": "done", "track_id": trackID}
			}
		}
		rows.Close()
	}

	rows, err := s.DB.Query(
		`SELECT source_ref, id, status, result_track_id FROM ingest_jobs WHERE source_ref = ANY($1)`,
		pq.Array(refs),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var ref, status string
		var jobID int64
		var trackID sql.NullString
		if err := rows.Scan(&ref, &jobID, &status, &trackID); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		entry := map[string]any{"job_id": jobID, "job_status": status}
		if trackID.Valid {
			entry["track_id"] = trackID.String
		}
		result[strings.TrimPrefix(ref, "deezer:")] = entry
	}
	writeJSON(w, http.StatusOK, result)
}

func nullablePositive(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}

// nullableDate passes through a valid YYYY-MM-DD; Deezer uses "0000-00-00"
// for unknown dates.
func nullableDate(s string) any {
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return nil
	}
	return s
}
