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

// Правки админа сверх названий и картинок (handlers_edit.go, handlers_claim.go):
//   - исполнители трека и релиза — по ссылке на артиста Deezer: Deezer залил
//     релиз дубликату артиста, и у одного артиста две карточки;
//   - метка explicit у трека;
//   - скрыть релиз (overlay.go — где он пропадает);
//   - перекачать аудио трека, загрузить своё и заново найти текст.

const maxCredits = 10

// creditArtists validates a credit list (artist IDs, the main one first:
// Deezer or own, k…) and makes sure every Deezer artist has a library row
// with its name and Deezer photo — the overlay names them from there. Own
// artists must already exist.
func (s *Server) creditArtists(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > maxCredits {
		return nil, fmt.Errorf("from 1 to %d artists", maxCredits)
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

// handleArtistInfo — артист по ID (имя, фото) для правки исполнителей и
// выбора артиста при загрузке: вставили ссылку — окно показывает, кто это.
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

// --- трек: исполнители и explicit ---

// trackCreditsState — то, что окно правки трека показывает и присылает.
type trackCreditsState struct {
	Explicit       bool               `json:"explicit"`
	Artists        []deezer.ArtistRef `json:"artists"`
	CreditsEdited  bool               `json:"credits_edited"`
	ExplicitEdited bool               `json:"explicit_edited"`
}

// trackCreditsOf is the track as everyone sees it (Deezer plus all edits).
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

// saveTrackCredits stores what differs from what the track would have
// without its own edit (Deezer, plus a credit edit of its release); the
// same as that — no edit.
func (s *Server) saveTrackCredits(dz string, explicit *bool, artists []string) error {
	if explicit == nil && artists == nil {
		return nil // окно старой версии: только название и номер
	}
	t, err := s.Deezer.GetTrack(dz)
	if err != nil {
		return err
	}
	var keepExplicit any // NULL — как у Deezer
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
		// Исполнителей не присылали — оставить прежнюю правку.
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

// badRequest — ошибка в присланных данных (400, а не 500).
type badRequest struct{ msg string }

func (e *badRequest) Error() string { return e.msg }

func writeSaveError(w http.ResponseWriter, err error) {
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
	writeError(w, http.StatusInternalServerError, "update failed")
}

// --- релиз: исполнители и скрытие ---

// albumAsSeen is the Deezer release as everyone sees it (with the edits).
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

// saveAlbumCredits stores the release's artists, or drops the edit when
// they are Deezer's own. credits_from — Deezer's artists, whom the release's
// tracks swap for the new ones.
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

// handleSetAlbumHidden — скрыть/показать релиз по Deezer ID (меню «…» на
// странице альбома); строка альбома заводится, если её не было.
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
		writeSaveError(w, err)
		return
	}
	if _, err := s.DB.Exec(`UPDATE albums SET hidden = $2 WHERE id = $1`, id, req.Hidden); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "hidden": req.Hidden})
}

// handleRefreshAlbum забывает альбом в кеше Deezer: релиз вышел или на
// Deezer поменялся треклист, а кеш держит старое.
func (s *Server) handleRefreshAlbum(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	s.Deezer.ForgetAlbum(dz)
	w.WriteHeader(http.StatusNoContent)
}

// --- перекачать аудио, заново найти текст ---

// handleRedownloadTrack ставит скачанный трек на перекачку: воркер заново
// ищет видео (поиск мог стать точнее, артист — перезалить трек) и заменяет
// файл в строке трека; лайки, плейлисты и история остаются при нём.
func (s *Server) handleRedownloadTrack(w http.ResponseWriter, r *http.Request) {
	dz := r.PathValue("id")
	if !validDeezerID(dz) {
		writeError(w, http.StatusBadRequest, "numeric deezer id is required")
		return
	}
	// Задачи до 0037 не знают explicit, а без него YouTube Music может
	// снова дать clean-версию. Deezer недоступен — оставить как есть.
	var explicit any
	if t, err := s.Deezer.GetTrack(dz); err == nil {
		explicit = t.Explicit
	}
	res, err := s.DB.Exec(
		`UPDATE ingest_jobs SET status = 'pending', error = NULL, video_id = NULL, hold_until = NULL,
		        manual_video_id = NULL, known_video_id = NULL, attempts = 0, -- заново искать
		        replace_audio = true, explicit = COALESCE($3, explicit), updated_at = now()
		  WHERE source_ref = $1 AND status IN ('done', 'failed')
		    AND EXISTS (SELECT 1 FROM tracks WHERE deezer_id = $2)`, "deezer:"+dz, dz, explicit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
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

// Свой файл: лучше исходник (FLAC, WAV — до ~100 МБ на 10 минут), а не то,
// что нашлось на YouTube. Предел совпадает с client_max_body_size в nginx
// для этого адреса.
const (
	maxAudioUpload     = 200 << 20
	audioUploadTimeout = 5 * time.Minute
)

// prepareAudio — ingest.PrepareAudio; тесты подставляют медленный.
var prepareAudio = ingest.PrepareAudio

// handleUploadTrackAudio заменяет аудио трека из медиатеки файлом админа:
// любой формат, который читает ffmpeg, перепаковывается/кодируется в .m4a с
// замером громкости, как скачанное (ingest.PrepareAudio). Файл меняется у
// всех треков, которые его делили (та же запись на другом релизе), — это
// одна запись. Лайки, плейлисты и история остаются при треке.
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
	// missing — трека нет в медиатеке, но его задача упала (на YouTube не
	// нашлось, handlers_missing.go) или файл удалили как неслушаемый: файл
	// админа и станет треком.
	missing := false
	if err := s.DB.QueryRow(`SELECT storage_key FROM tracks WHERE deezer_id = $1`, dz).Scan(&oldKey); err == sql.ErrNoRows {
		if missing = s.trackMissing(dz); !missing {
			writeError(w, http.StatusConflict, "the track is not in the library yet")
			return
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	// Перекачка в работе заменила бы загруженный файл своим.
	var busy bool
	s.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM ingest_jobs WHERE source_ref = $1 AND status IN ('pending', 'processing'))`,
		"deezer:"+dz).Scan(&busy)
	if busy {
		writeError(w, http.StatusConflict, "the track is being downloaded, try again in a minute")
		return
	}

	// Файл в сотни МБ и перекодирование до audioUploadTimeout — дольше общих
	// таймаутов сервера (cmd/api): без своих сроков ответ обрывался, и админ
	// видел ошибку, хотя аудио уже заменилось. nginx ждёт этот адрес 360 с.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(audioUploadTimeout))
	_ = rc.SetWriteDeadline(time.Now().Add(audioUploadTimeout))

	// Тело — во временный файл рядом с медиатекой (не в память), оттуда — в .m4a.
	src, err := os.CreateTemp(s.StorageDir, ".upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save audio")
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
	if !acquireAudioSlot(w, r) { // одна обработка аудио за раз (uploads_audio.go)
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
		s.saveMissingTrack(w, dz, key, lufs, gainDB)
		return
	}
	// audio_pinned: файл админа не скачать заново — неслушаемым его не удалять (0029).
	res, err := s.DB.Exec(`UPDATE tracks SET storage_key = $1, loudness_lufs = $2, gain_db = $3, audio_pinned = true WHERE storage_key = $4`,
		key, lufs, gainDB, oldKey)
	if err == nil {
		if n, _ := res.RowsAffected(); n == 0 {
			err = sql.ErrNoRows // трек удалили или перекачали, пока шла загрузка
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

// saveMissingTrack делает загруженный админом файл треком, которого не было
// в медиатеке: каталог — по данным его задачи, как у воркера.
func (s *Server) saveMissingTrack(w http.ResponseWriter, dz, key string, lufs, gainDB float64) {
	job, err := ingest.LoadJob(s.DB, "deezer:"+dz)
	var trackID string
	if err == nil {
		trackID, _, err = ingest.SaveTrack(s.DB, job, key, lufs, gainDB)
	}
	if err == nil {
		_, err = s.DB.Exec(`UPDATE tracks SET audio_pinned = true WHERE id = $1 AND storage_key = $2`, trackID, key)
	}
	if err != nil {
		log.Printf("track %s: saving uploaded audio: %v", dz, err)
		s.removeStorageFiles(key)
		writeError(w, http.StatusInternalServerError, "could not save the track")
		return
	}
	ingest.MarkDone(s.DB, job.ID, trackID)
	s.Streams.clearNotFound(dz)
	log.Printf("track %s: added from an upload (%s)", dz, key)
	writeJSON(w, http.StatusOK, map[string]any{"status": "added", "gain_db": gainDB})
}

// handleRefreshLyrics забывает найденный текст и ищет заново (источники
// могли добавить текст или тайминг по словам). Ответ — как у GET.
func (s *Server) handleRefreshLyrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeezerID(id) {
		writeError(w, http.StatusBadRequest, "bad track id")
		return
	}
	// Забыть на всех релизах этой записи (общий ISRC), иначе тут же нашёлся
	// бы прежний текст с сингла или альбома.
	isrc := ""
	if tr, err := s.Deezer.GetTrack(id); err == nil {
		isrc = tr.ISRC
	}
	if s.Lyrics != nil {
		if err := s.Lyrics.Forget(r.Context(), id, isrc); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
	}
	s.handleLyrics(w, r)
}
