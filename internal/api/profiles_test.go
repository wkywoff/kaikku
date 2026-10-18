package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

type profileReply struct {
	User      person
	Me        bool
	Playlists []playlistInfo
	People    []person
}

func getProfile(t *testing.T, router http.Handler, token, name string) profileReply {
	t.Helper()
	var p profileReply
	json.Unmarshal(call(t, router, "GET", "/api/users/"+name, token, "", http.StatusOK), &p)
	return p
}

func TestProfilesAndFollows(t *testing.T) {
	_, router, me, _ := newTestServer(t)
	ann := invitedUser(t, router, me, "prof-ann")
	bob := invitedUser(t, router, me, "prof-bob")
	annID, bobID := userIDOf(t, router, ann), userIDOf(t, router, bob)

	// Профиль — по нику без учёта регистра; в нём только публичные плейлисты.
	call(t, router, "POST", "/api/playlists", ann, `{"title":"Open","is_public":true}`, http.StatusCreated)
	call(t, router, "POST", "/api/playlists", ann, `{"title":"Hidden"}`, http.StatusCreated)
	p := getProfile(t, router, me, "PROF-ANN")
	if p.User.ID != annID || p.Me || len(p.Playlists) != 1 || p.Playlists[0].Title != "Open" || p.People != nil {
		t.Fatalf("ann's profile: %+v", p)
	}
	call(t, router, "GET", "/api/users/nobody-here", me, "", http.StatusNotFound)

	// Подписки: в одну сторону — подписчик, в обе — друзья.
	var st person
	json.Unmarshal(call(t, router, "PUT", "/api/users/"+annID+"/follow", bob, "", http.StatusOK), &st)
	if !st.IFollow || st.FollowsMe {
		t.Fatalf("bob follows ann: %+v", st)
	}
	call(t, router, "PUT", "/api/users/"+annID+"/follow", bob, "", http.StatusOK) // повтор — не ошибка
	if a := getProfile(t, router, ann, "prof-ann"); !a.Me || len(a.People) != 1 || !a.People[0].FollowsMe || a.People[0].IFollow {
		t.Fatalf("ann's own profile: %+v", a)
	}
	call(t, router, "PUT", "/api/users/"+bobID+"/follow", ann, "", http.StatusOK)
	var people struct{ People []person }
	json.Unmarshal(call(t, router, "GET", "/api/me/people", ann, "", http.StatusOK), &people)
	if len(people.People) != 1 || !people.People[0].IFollow || !people.People[0].FollowsMe {
		t.Fatalf("friends: %+v", people.People)
	}
	json.Unmarshal(call(t, router, "DELETE", "/api/users/"+annID+"/follow", bob, "", http.StatusOK), &st)
	if st.IFollow || !st.FollowsMe {
		t.Fatalf("after unfollow: %+v", st)
	}
	call(t, router, "PUT", "/api/users/"+userIDOf(t, router, me)+"/follow", me, "", http.StatusBadRequest)
	call(t, router, "PUT", "/api/users/not-a-user/follow", me, "", http.StatusNotFound)

	// Поиск находит человека только по нику целиком.
	if got := (&Server{DB: openTestDB(t)}).searchPeople("00000000-0000-0000-0000-000000000000", "@prof-bob"); len(got) != 1 || got[0].ID != bobID {
		t.Errorf("exact search: %+v", got)
	}
	if got := (&Server{DB: openTestDB(t)}).searchPeople("00000000-0000-0000-0000-000000000000", "prof"); len(got) != 0 {
		t.Errorf("prefix search found people: %+v", got)
	}
}
