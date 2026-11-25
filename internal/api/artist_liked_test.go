package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

type artistLiked struct {
	Artist struct{ ID, Name string }
	Tracks []pageTrack
	Albums []pageAlbum
}

func pageTrackIDs(tracks []pageTrack) []string {
	var ids []string
	for _, t := range tracks {
		ids = append(ids, t.ID)
	}
	return ids
}

func likedOfArtist(t *testing.T, router http.Handler, token, artistID string) (tracks, albums []string) {
	t.Helper()
	var got artistLiked
	getJSON(t, router, "/api/me/artists/"+artistID+"/liked", token, false, &got)
	if got.Artist.ID != artistID || got.Tracks == nil || got.Albums == nil {
		t.Errorf("liked of artist %s: %+v", artistID, got)
	}
	tracks, albums = pageTrackIDs(got.Tracks), albumIDs(got.Albums)

	var page struct {
		Liked struct{ Tracks, Albums []string }
	}
	getJSON(t, router, "/api/discover/artists/"+artistID, token, false, &page)
	if page.Liked.Tracks == nil || page.Liked.Albums == nil {
		t.Errorf("artist %s page must list likes even when there are none: %+v", artistID, page.Liked)
	}
	if !slices.Equal(page.Liked.Tracks, append([]string{}, tracks...)) || !slices.Equal(page.Liked.Albums, append([]string{}, albums...)) {
		t.Errorf("artist %s page says %+v, the liked page %v %v", artistID, page.Liked, tracks, albums)
	}
	return tracks, albums
}

func expectLiked(t *testing.T, router http.Handler, token, artistID string, tracks, albums []string) {
	t.Helper()
	gotTracks, gotAlbums := likedOfArtist(t, router, token, artistID)
	if !slices.Equal(gotTracks, tracks) || !slices.Equal(gotAlbums, albums) {
		t.Errorf("liked of artist %s = %v %v, want %v %v", artistID, gotTracks, gotAlbums, tracks, albums)
	}
}

func claimedAlbum(t *testing.T, router http.Handler, token, deezerID string) string {
	t.Helper()
	var claimed struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/albums/deezer/"+deezerID+"/claim", token, "", http.StatusOK), &claimed)
	if claimed.ID == "" {
		t.Fatalf("album %s was not claimed", deezerID)
	}
	return claimed.ID
}

func TestArtistLiked_SongsAndReleasesOfTheArtist(t *testing.T) {
	_, router, token := newTestRouter(t)
	for _, id := range []string{"12345", "888", "501"} {
		call(t, router, "PUT", "/api/me/liked/"+id, token, "", http.StatusNoContent)
	}
	for _, id := range []string{"77", "11", "9"} {
		call(t, router, "PUT", "/api/me/albums/"+id, token, "", http.StatusNoContent)
	}

	expectLiked(t, router, token, "1", []string{"501", "888", "12345"}, []string{"9", "11"})
	expectLiked(t, router, token, "2", nil, []string{"11"})
	expectLiked(t, router, token, "3", []string{"888"}, nil)

	other := invitedUser(t, router, token, "liked-other")
	expectLiked(t, router, other, "1", nil, nil)

	call(t, router, "DELETE", "/api/me/liked/888", token, "", http.StatusNoContent)
	call(t, router, "DELETE", "/api/me/albums/9", token, "", http.StatusNoContent)
	expectLiked(t, router, token, "1", []string{"501", "12345"}, []string{"11"})

	call(t, router, "GET", "/api/me/artists/nope/liked", token, "", http.StatusBadRequest)
	call(t, router, "GET", "/api/me/artists/k999999/liked", token, "", http.StatusNotFound)
	call(t, router, "GET", "/api/me/artists/1/liked", "", "", http.StatusUnauthorized)
}

func TestArtistLiked_TracksCarryTheDateAdded(t *testing.T) {
	_, router, token := newTestRouter(t)
	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)

	var got struct {
		Tracks []struct {
			Title   string `json:"title"`
			AddedAt string `json:"added_at"`
		}
	}
	getJSON(t, router, "/api/me/artists/1/liked", token, false, &got)
	if len(got.Tracks) != 1 || got.Tracks[0].Title != "Test Song" || got.Tracks[0].AddedAt == "" {
		t.Errorf("tracks: %+v", got.Tracks)
	}
}

func TestArtistLiked_FollowsReleaseCreditEdits(t *testing.T) {
	_, router, token := newTestRouter(t)
	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/albums/9", token, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/albums/11", token, "", http.StatusNoContent)

	put(t, router, "/api/albums/"+claimedAlbum(t, router, token, "9"), token, `{"title":"Test Album","artists":["2"]}`, http.StatusOK)
	expectLiked(t, router, token, "1", nil, []string{"11"})
	expectLiked(t, router, token, "2", []string{"12345"}, []string{"11", "9"})

	put(t, router, "/api/albums/"+claimedAlbum(t, router, token, "11"), token, `{"title":"Late Album","artists":["1"]}`, http.StatusOK)
	expectLiked(t, router, token, "2", []string{"12345"}, []string{"9"})
}

func TestArtistLiked_FollowsTrackCreditEdits(t *testing.T) {
	_, router, token := newTestRouter(t)
	call(t, router, "PUT", "/api/me/liked/12345", token, "", http.StatusNoContent)
	put(t, router, "/api/tracks/deezer/12345", token, `{"title":"Test Song","artists":["2"]}`, http.StatusOK)

	expectLiked(t, router, token, "1", nil, nil)
	expectLiked(t, router, token, "2", []string{"12345"}, nil)
}

func TestArtistLiked_OwnReleases(t *testing.T) {
	db, router, admin := newTestRouter(t)
	user := invitedUser(t, router, admin, "own-fan")
	rel := seedOwnRelease(t, db, userIDOf(t, router, admin), false)
	call(t, router, "PUT", "/api/me/liked/"+rel.tracks[1], user, "", http.StatusNoContent)
	call(t, router, "PUT", "/api/me/albums/"+rel.album, user, "", http.StatusNoContent)

	expectLiked(t, router, user, rel.artist, []string{rel.tracks[1]}, []string{rel.album})
	expectLiked(t, router, user, "1", nil, []string{rel.album})
	expectLiked(t, router, user, "2", []string{rel.tracks[1]}, nil)
}
