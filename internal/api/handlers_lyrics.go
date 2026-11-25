package api

import (
	"net/http"

	"kaikku/internal/lyrics"
)

func (s *Server) EnableLyrics(lrcredContact string) {
	providers := []lyrics.Provider{&lyrics.Deezer{}, lyrics.LRCLIB{}}
	if lrcredContact != "" {
		providers = append([]lyrics.Provider{lyrics.LRCRed{Contact: lrcredContact}}, providers...)
	}
	s.Lyrics = &lyrics.Service{DB: s.DB, Providers: providers}
}

func (s *Server) handleLyrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "bad track id")
		return
	}
	if s.Lyrics == nil {
		writeJSON(w, http.StatusOK, map[string]any{"lyrics": nil})
		return
	}
	if isLocalID(id) {
		s.ownLyrics(w, r, id)
		return
	}
	tr, err := s.Deezer.GetTrack(id)
	if err != nil {
		writeDeezerError(w, "track", err)
		return
	}
	l, err := s.Lyrics.Get(r.Context(), lyrics.Track{
		DeezerID: id,
		ISRC:     tr.ISRC,
		Artist:   tr.Artist,
		Title:    tr.Title,
		Album:    tr.Album,
		Duration: float64(tr.DurationMs) / 1000,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "lyrics sources unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"lyrics": l})
}

func (s *Server) ownLyrics(w http.ResponseWriter, r *http.Request, id string) {
	if tracks, err := s.localTracks(viewerOf(r), []string{id}); err != nil {
		serverError(w, r, "query failed", err)
		return
	} else if len(tracks) == 0 {
		writeError(w, http.StatusNotFound, "track not found")
		return
	}
	l, err := s.Lyrics.Stored(r.Context(), id)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"lyrics": l})
}
