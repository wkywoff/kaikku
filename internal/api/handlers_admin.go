package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/lib/pq"

	"kaikku/internal/deezer"
	"kaikku/internal/ingest"
)

func (s *Server) creditArtists(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one artist")
	}
	var out []string
	for _, id := range ids {
		if !validCatalogID(id) {
			return nil, fmt.Errorf("artist ids must be deezer or kaikku artist ids")
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, id := range out {
		if isLocalID(id) {
			var exists bool
			if err := s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM artists WHERE deezer_id = $1)`, id).Scan(&exists); err != nil {
				return nil, err
			} else if !exists {
				return nil, fmt.Errorf("no artist %s", id)
			}
			continue
		}
		a, err := s.artistInfo(id)
		if err != nil {
			return nil, fmt.Errorf("deezer artist %s: %w", id, err)
		}
		if _, err := ingest.EnsureArtist(s.DB, id, a.Name); err != nil {
			return nil, err
		}
		if _, err := s.DB.Exec(`UPDATE artists SET picture_url = NULLIF($2, '') WHERE deezer_id = $1`, id, a.PictureURL); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Server) handleArtistInfo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}
	a, err := s.catalogArtist(id)
	if err != nil {
		writeCatalogError(w, "artist lookup failed", err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

type trackCreditsState struct {
	Explicit       bool               `json:"explicit"`
	Artists        []deezer.ArtistRef `json:"artists"`
	CreditsEdited  bool               `json:"credits_edited"`
	ExplicitEdited bool               `json:"explicit_edited"`
}

func (s *Server) trackCreditsOf(dz string) (trackCreditsState, error) {
	t, err := s.Deezer.GetTrack(dz)
	if err != nil {
		return trackCreditsState{}, err
	}
	list := []deezer.Track{*t}
	s.overlayFor(nil, nil, list)
	st := trackCreditsState{Explicit: list[0].Explicit, Artists: trackRefs(&list[0])}
	var explicit sql.NullBool
	var artists []string
	if s.DB.QueryRow(`SELECT explicit, artist_ids FROM track_overrides WHERE deezer_id = $1`, dz).
		Scan(&explicit, pq.Array(&artists)) == nil {
		st.ExplicitEdited, st.CreditsEdited = explicit.Valid, len(artists) > 0
	}
	return st, nil
}

func (s *Server) saveTrackCredits(dz string, explicit *bool, artists []string) error {
	if explicit == nil && artists == nil {
		return nil
	}
	t, err := s.Deezer.GetTrack(dz)
	if err != nil {
		return err
	}
	var keepExplicit any
	if explicit != nil && *explicit != t.Explicit {
		keepExplicit = *explicit
	}
	var keepArtists any
	if artists != nil {
		ids, err := s.creditArtists(artists)
		if err != nil {
			return &badRequest{err.Error()}
		}
		inherited := refIDs(trackRefs(t))
		var to, from []string
		if s.DB.QueryRow(`SELECT artist_ids, credits_from FROM albums WHERE deezer_id = $1 AND artist_ids IS NOT NULL`, t.AlbumID).
			Scan(pq.Array(&to), pq.Array(&from)) == nil {
			if r := replaceCredits(inherited, from, to); r != nil {
				inherited = r
			}
		}
		if !slices.Equal(ids, inherited) {
			keepArtists = pq.Array(ids)
		}
	} else {
		var prev []string
		if s.DB.QueryRow(`SELECT artist_ids FROM track_overrides WHERE deezer_id = $1`, dz).Scan(pq.Array(&prev)) == nil && prev != nil {
			keepArtists = pq.Array(prev)
		}
	}
	if keepExplicit == nil && keepArtists == nil {
		_, err = s.DB.Exec(`DELETE FROM track_overrides WHERE deezer_id = $1`, dz)
		return err
	}
	_, err = s.DB.Exec(
		`INSERT INTO track_overrides (deezer_id, explicit, artist_ids) VALUES ($1, $2, $3)
		 ON CONFLICT (deezer_id) DO UPDATE SET explicit = EXCLUDED.explicit, artist_ids = EXCLUDED.artist_ids, updated_at = now()`,
		dz, keepExplicit, keepArtists)
	return err
}

type badRequest struct{ msg string }

func (e *badRequest) Error() string { return e.msg }

func writeSaveError(w http.ResponseWriter, r *http.Request, err error) {
	var br *badRequest
	if errors.As(err, &br) {
		writeError(w, http.StatusBadRequest, br.msg)
		return
	}
	var de *deezer.APIError
	if errors.As(err, &de) {
		writeDeezerError(w, "deezer lookup failed", err)
		return
	}
	serverError(w, r, "update failed", err)
}

func (s *Server) albumAsSeen(dz string) (*deezer.Album, error) {
	if s.Deezer == nil {
		return nil, errors.New("deezer is not configured")
	}
	a, err := s.Deezer.GetAlbum(dz)
	if err != nil {
		return nil, err
	}
	s.overlayFor(nil, []*deezer.Album{a}, nil)
	return a, nil
}

func (s *Server) saveAlbumCredits(libraryID string, artists []string) error {
	var dz string
	if err := s.DB.QueryRow(`SELECT deezer_id FROM albums WHERE id = $1`, libraryID).Scan(&dz); err != nil {
		return err
	}
	ids, err := s.creditArtists(artists)
	if err != nil {
		return &badRequest{err.Error()}
	}
	a, err := s.Deezer.GetAlbum(dz)
	if err != nil {
		return err
	}
	from := refIDs(albumRefs(a))
	if slices.Equal(ids, from) {
		_, err = s.DB.Exec(`UPDATE albums SET artist_ids = NULL, credits_from = NULL WHERE id = $1`, libraryID)
		return err
	}
	_, err = s.DB.Exec(`UPDATE albums SET artist_ids = $2, credits_from = $3 WHERE id = $1`, libraryID, pq.Array(ids), pq.Array(from))
	return err
}

func (s *Server) handleSetAlbumHidden(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	var req struct {
		Hidden bool `json:"hidden"`
	}
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "hidden is required")
		return
	}
	id, err := s.claimAlbum(dz)
	if err != nil {
		writeSaveError(w, r, err)
		return
	}
	if _, err := s.DB.Exec(`UPDATE albums SET hidden = $2 WHERE id = $1`, id, req.Hidden); err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "hidden": req.Hidden})
}

func (s *Server) handleRefreshAlbum(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	s.Deezer.ForgetAlbum(dz)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRedownloadTrack(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	var explicit any
	if t, err := s.Deezer.GetTrack(dz); err == nil {
		explicit = t.Explicit
	}
	res, err := s.DB.Exec(
		`UPDATE ingest_jobs SET status = 'pending', error = NULL, video_id = NULL, hold_until = NULL,
		        manual_video_id = NULL, known_video_id = NULL, attempts = 0,
		        replace_audio = true, explicit = COALESCE($3, explicit), updated_at = now()
		  WHERE source_ref = $1 AND status IN ('done', 'failed')
		    AND EXISTS (SELECT 1 FROM tracks WHERE deezer_id = $2)`, "deezer:"+dz, dz, explicit)
	if err != nil {
		serverError(w, r, "update failed", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var status string
		if s.DB.QueryRow(`SELECT status FROM ingest_jobs WHERE source_ref = $1`, "deezer:"+dz).Scan(&status) == nil &&
			(status == "pending" || status == "processing") {
			writeError(w, http.StatusConflict, "the track is already being downloaded")
			return
		}
		writeError(w, http.StatusConflict, "the track is not in the library yet")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

const (
	maxAudioUpload     = 200 << 20
	audioUploadTimeout = 5 * time.Minute
	audioBodyTimeout   = 20 * time.Minute
)

var prepareAudio = ingest.PrepareAudio

func (s *Server) handleUploadTrackAudio(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	if s.StorageDir == "" {
		writeError(w, http.StatusServiceUnavailable, "storage is not configured")
		return
	}
	var oldKey string
	missing := false
	if err := s.DB.QueryRow(`SELECT storage_key FROM tracks WHERE deezer_id = $1`, dz).Scan(&oldKey); err == sql.ErrNoRows {
		if missing = s.trackMissing(dz); !missing {
			writeError(w, http.StatusConflict, "the track is not in the library yet")
			return
		}
	} else if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	var busy bool
	s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM ingest_jobs WHERE source_ref = $1 AND status IN ('pending', 'processing'))`,
		"deezer:"+dz).Scan(&busy)
	if busy {
		writeError(w, http.StatusConflict, "the track is being downloaded, try again in a minute")
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(audioBodyTimeout))
	_ = rc.SetWriteDeadline(time.Now().Add(audioBodyTimeout))

	src, err := os.CreateTemp(s.StorageDir, ".upload-*")
	if err != nil {
		serverError(w, r, "could not save audio", err)
		return
	}
	defer os.Remove(src.Name())
	_, err = io.Copy(src, http.MaxBytesReader(w, r.Body, maxAudioUpload))
	src.Close()
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "audio must be at most 200 MB")
		return
	} else if err != nil {
		writeError(w, http.StatusBadRequest, "upload interrupted")
		return
	}

	key := randomKey() + ".m4a"
	_ = rc.SetWriteDeadline(time.Now().Add(slotWait + audioUploadTimeout + 30*time.Second))
	if !acquireAudioSlot(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), audioUploadTimeout)
	lufs, gainDB, err := prepareAudio(ctx, src.Name(), filepath.Join(s.StorageDir, key))
	cancel()
	releaseAudioSlot()
	if err != nil {
		log.Printf("track %s: uploaded audio: %v", dz, err)
		writeError(w, http.StatusBadRequest, "no audio found in the file")
		return
	}
	if missing {
		s.saveMissingTrack(w, r, dz, key, lufs, gainDB)
		return
	}
	res, err := s.DB.Exec(`UPDATE tracks SET storage_key = $1, loudness_lufs = $2, gain_db = $3, audio_pinned = true WHERE storage_key = $4`,
		key, lufs, gainDB, oldKey)
	if err == nil {
		if n, _ := res.RowsAffected(); n == 0 {
			err = sql.ErrNoRows
		}
	}
	if err != nil {
		s.removeStorageFiles(key)
		writeError(w, http.StatusConflict, "the track changed meanwhile, try again")
		return
	}
	s.removeStorageFiles(s.unreferencedAudio(oldKey)...)
	log.Printf("track %s: audio replaced by an upload (%s -> %s)", dz, oldKey, key)
	writeJSON(w, http.StatusOK, map[string]any{"status": "replaced", "gain_db": gainDB})
}

func (s *Server) saveMissingTrack(w http.ResponseWriter, r *http.Request, dz, key string, lufs, gainDB float64) {
	job, err := ingest.LoadJob(s.DB, "deezer:"+dz)
	var trackID string
	if err == nil {
		trackID, _, err = ingest.SaveTrack(s.DB, job, key, lufs, gainDB)
	}
	if err == nil {
		_, err = s.DB.Exec(`UPDATE tracks SET audio_pinned = true WHERE id = $1 AND storage_key = $2`, trackID, key)
	}
	if err != nil {
		s.removeStorageFiles(key)
		serverError(w, r, "could not save the track", err)
		return
	}
	ingest.MarkDone(s.DB, job.ID, trackID)
	s.Streams.clearNotFound(dz)
	log.Printf("track %s: added from an upload (%s)", dz, key)
	writeJSON(w, http.StatusOK, map[string]any{"status": "added", "gain_db": gainDB})
}

func (s *Server) handleRefreshLyrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeezerID(id) {
		writeError(w, http.StatusBadRequest, "bad track id")
		return
	}
	isrc := ""
	if tr, err := s.Deezer.GetTrack(id); err == nil {
		isrc = tr.ISRC
	}
	if s.Lyrics != nil {
		if err := s.Lyrics.Forget(r.Context(), id, isrc); err != nil {
			serverError(w, r, "update failed", err)
			return
		}
	}
	s.handleLyrics(w, r)
}
