package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCollaborativePlaylist(t *testing.T) {
	_, router, owner, _ := newTestServer(t)
	friend := invitedUser(t, router, owner, "collab-friend")
	stranger := invitedUser(t, router, owner, "collab-stranger")
	friendID := userIDOf(t, router, friend)

	var pl struct{ ID string }
	json.Unmarshal(call(t, router, "POST", "/api/playlists", owner, `{"title":"Ours"}`, http.StatusCreated), &pl)
	base := "/api/playlists/" + pl.ID

	call(t, router, "POST", base+"/tracks", friend, `{"deezer_ids":["12345"]}`, http.StatusNotFound) // ещё не соавтор
	call(t, router, "PUT", base+"/collaborators/"+friendID, stranger, "", http.StatusNotFound)       // звать может только владелец

	// Соавтор — только друг: подписаны друг на друга.
	ownerID := userIDOf(t, router, owner)
	call(t, router, "PUT", base+"/collaborators/"+friendID, owner, "", http.StatusForbidden)
	call(t, router, "PUT", "/api/users/"+friendID+"/follow", owner, "", http.StatusOK)
	call(t, router, "PUT", base+"/collaborators/"+friendID, owner, "", http.StatusForbidden) // подписка только в одну сторону
	call(t, router, "PUT", "/api/users/"+ownerID+"/follow", friend, "", http.StatusOK)
	call(t, router, "PUT", base+"/collaborators/no-such-user", owner, "", http.StatusNotFound)
	call(t, router, "PUT", base+"/collaborators/"+friendID, owner, "", http.StatusNoContent)

	// Соавтор видит плейлист у себя, может добавлять и переставлять.
	var lists struct{ Mine []playlistInfo }
	json.Unmarshal(call(t, router, "GET", "/api/playlists", friend, "", http.StatusOK), &lists)
	if len(lists.Mine) != 1 || !lists.Mine[0].CanEdit || lists.Mine[0].Mine || !lists.Mine[0].Collaborative {
		t.Fatalf("friend's playlists: %+v", lists.Mine)
	}
	call(t, router, "POST", base+"/tracks", owner, `{"deezer_ids":["12345"]}`, http.StatusOK)
	call(t, router, "POST", base+"/tracks", friend, `{"deezer_ids":["888"]}`, http.StatusOK)
	var got struct {
		Playlist playlistInfo
		Tracks   []collectionTrack
	}
	json.Unmarshal(call(t, router, "GET", base, friend, "", http.StatusOK), &got)
	if len(got.Tracks) != 2 || got.Tracks[0].AddedBy == nil || got.Tracks[1].AddedBy.Username != "collab-friend" ||
		len(got.Playlist.Collaborators) != 1 {
		t.Fatalf("tracks: %+v, collaborators %+v", got.Tracks, got.Playlist.Collaborators)
	}
	call(t, router, "PUT", base+"/tracks/"+itoa(got.Tracks[1].ItemID)+"/position", friend, `{"before":`+itoa(got.Tracks[0].ItemID)+`}`, http.StatusNoContent)
	call(t, router, "DELETE", base+"/tracks/"+itoa(got.Tracks[0].ItemID), friend, "", http.StatusNoContent)

	// Название, обложку и удаление — только владелец.
	call(t, router, "PUT", base, friend, `{"title":"Mine now"}`, http.StatusForbidden)
	call(t, router, "DELETE", base, friend, "", http.StatusForbidden)
	call(t, router, "GET", base, stranger, "", http.StatusNotFound)

	// Соавтор выходит сам — и плейлиста у него больше нет.
	call(t, router, "DELETE", base+"/collaborators/"+friendID, friend, "", http.StatusNoContent)
	call(t, router, "POST", base+"/tracks", friend, `{"deezer_ids":["12345"]}`, http.StatusNotFound)
	call(t, router, "GET", base, friend, "", http.StatusNotFound)
}
