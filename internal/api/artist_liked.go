package api

import (
	"log"
	"net/http"
	"slices"

	"kaikku/internal/deezer"
)

func (s *Server) likedTracksOf(userID, artistID string) ([]collectionTrack, error) {
	tracks, err := s.collectionTracks(`
		SELECT 0, l.meta, l.added_at
		  FROM liked_tracks l
		 WHERE l.user_id = $1
		   AND (l.meta->>'artist_id' = $2
		        OR l.meta->'contributors' @> jsonb_build_array(jsonb_build_object('id', $2::text))
		        OR EXISTS (SELECT 1 FROM track_overrides o WHERE o.deezer_id = l.deezer_id AND o.artist_ids @> ARRAY[$2::text])
		        OR EXISTS (SELECT 1 FROM albums a WHERE a.deezer_id = l.meta->>'album_id' AND a.artist_ids @> ARRAY[$2::text]))
		 ORDER BY l.added_at DESC`, userID, artistID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(tracks, func(t collectionTrack) bool { return !isCredited(artistID, t.Track) }), nil
}

const savedAlbumsByArtist = `
	  FROM saved_albums s LEFT JOIN albums a ON a.deezer_id = s.deezer_id
	 WHERE s.user_id = $1
	   AND CASE WHEN a.artist_ids IS NOT NULL THEN a.artist_ids @> ARRAY[$2::text]
	            ELSE s.meta->>'artist_id' = $2 OR s.meta->'main_artist_ids' @> to_jsonb($2::text) END
	 ORDER BY s.added_at DESC`

func (s *Server) savedAlbumsOf(userID, artistID string) ([]deezer.Album, error) {
	return s.savedAlbums(`SELECT s.meta`+savedAlbumsByArtist, userID, artistID)
}

type likedIDs struct {
	Tracks []string `json:"tracks"`
	Albums []string `json:"albums"`
}

func (s *Server) likedOf(userID, artistID string) likedIDs {
	liked := likedIDs{Tracks: []string{}, Albums: []string{}}
	tracks, err := s.likedTracksOf(userID, artistID)
	if err != nil {
		log.Printf("artist %s: liked songs: %v", artistID, err)
	}
	for _, t := range tracks {
		liked.Tracks = append(liked.Tracks, t.ID)
	}
	albums, err := s.idList(`SELECT s.deezer_id`+savedAlbumsByArtist, userID, artistID)
	if err != nil {
		log.Printf("artist %s: saved albums: %v", artistID, err)
		return liked
	}
	liked.Albums = albums
	return liked
}

func (s *Server) handleArtistLiked(w http.ResponseWriter, r *http.Request) {
	userID, id := currentUser(r), r.PathValue("id")
	if !validCatalogID(id) {
		writeError(w, http.StatusBadRequest, "invalid artist id")
		return
	}
	artist, err := s.catalogArtist(id)
	if err != nil {
		writeCatalogError(w, "artist lookup failed", err)
		return
	}
	tracks, err := s.likedTracksOf(userID, id)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	albums, err := s.savedAlbumsOf(userID, id)
	if err != nil {
		serverError(w, r, "query failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artist": artist, "tracks": tracks, "albums": albums})
}
