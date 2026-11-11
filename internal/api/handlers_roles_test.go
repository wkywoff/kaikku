package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

type listedUser struct {
	ID      string `json:"id"`
	IsAdmin bool   `json:"is_admin"`
}

// usersByName — список пользователей для админа, по имени.
func usersByName(t *testing.T, router http.Handler, adminToken string) map[string]listedUser {
	t.Helper()
	var list struct {
		Users []struct {
			listedUser
			Username string `json:"username"`
		} `json:"users"`
	}
	if err := json.Unmarshal(call(t, router, "GET", "/api/admin/users", adminToken, "", http.StatusOK), &list); err != nil {
		t.Fatalf("user list: %v", err)
	}
	users := map[string]listedUser{}
	for _, u := range list.Users {
		users[u.Username] = u.listedUser
	}
	return users
}

func adminCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE is_admin`).Scan(&n); err != nil {
		t.Fatalf("counting admins: %v", err)
	}
	return n
}

// Админ выдаёт и снимает права; и то и другое действует на следующем же
// запросе с тем же токеном.
func TestSetUserAdmin(t *testing.T) {
	_, router, owner, _ := newTestServer(t) // первый пользователь — админ
	friend := invitedUser(t, router, owner, "friend")
	users := usersByName(t, router, owner)
	friendID := users["friend"].ID
	if friendID == "" || users["friend"].IsAdmin {
		t.Fatalf("an invited user must be listed and not be an admin: %+v", users)
	}
	path := "/api/admin/users/" + friendID + "/admin"

	// Не админ прав не выдаёт — ни себе, ни другим.
	call(t, router, "PUT", path, friend, `{"admin":true}`, http.StatusForbidden)
	call(t, router, "PUT", path, "", `{"admin":true}`, http.StatusUnauthorized)
	call(t, router, "PUT", path, owner, `{}`, http.StatusBadRequest)
	call(t, router, "PUT", "/api/admin/users/00000000-0000-0000-0000-000000000000/admin", owner, `{"admin":true}`, http.StatusNotFound)
	call(t, router, "PUT", "/api/admin/users/not-an-id/admin", owner, `{"admin":true}`, http.StatusNotFound)
	if usersByName(t, router, owner)["friend"].IsAdmin {
		t.Fatal("refused requests must not make anyone an admin")
	}

	var resp struct {
		ID, Username string
		IsAdmin      bool `json:"is_admin"`
	}
	json.Unmarshal(call(t, router, "PUT", path, owner, `{"admin":true}`, http.StatusOK), &resp)
	if resp.ID != friendID || resp.Username != "friend" || !resp.IsAdmin {
		t.Errorf("grant response: %+v", resp)
	}
	// Повторно — не ошибка.
	call(t, router, "PUT", path, owner, `{"admin":true}`, http.StatusOK)
	// Список теперь открыт и самому friend.
	if !usersByName(t, router, friend)["friend"].IsAdmin {
		t.Error("the list must show the new admin")
	}
	var me struct {
		IsAdmin bool `json:"is_admin"`
	}
	json.Unmarshal(call(t, router, "GET", "/api/me", friend, "", http.StatusOK), &me)
	if !me.IsAdmin {
		t.Error("/api/me must say the user is an admin now")
	}

	call(t, router, "PUT", path, owner, `{"admin":false}`, http.StatusOK)
	call(t, router, "GET", "/api/admin/users", friend, "", http.StatusForbidden)
	call(t, router, "POST", "/api/invites", friend, "", http.StatusForbidden)
	call(t, router, "PUT", path, friend, `{"admin":true}`, http.StatusForbidden) // сам себе права не вернёт
	json.Unmarshal(call(t, router, "GET", "/api/me", friend, "", http.StatusOK), &me)
	if me.IsAdmin || usersByName(t, router, owner)["friend"].IsAdmin {
		t.Error("revoked rights must be gone")
	}
}

// Последнего админа снять нельзя, себя — тоже; пока есть другой админ, с
// себя права снять можно.
func TestSetUserAdmin_LastAdminStays(t *testing.T) {
	db, router, owner, _ := newTestServer(t)
	friend := invitedUser(t, router, owner, "friend")
	users := usersByName(t, router, owner)
	ownerPath, friendPath := "", "/api/admin/users/"+users["friend"].ID+"/admin"
	for name, u := range users {
		if name != "friend" {
			ownerPath = "/api/admin/users/" + u.ID + "/admin"
		}
	}

	call(t, router, "PUT", ownerPath, owner, `{"admin":false}`, http.StatusConflict)
	call(t, router, "GET", "/api/admin/users", owner, "", http.StatusOK) // всё ещё админ

	call(t, router, "PUT", friendPath, owner, `{"admin":true}`, http.StatusOK)
	call(t, router, "PUT", ownerPath, owner, `{"admin":false}`, http.StatusOK) // есть другой админ — можно и с себя
	call(t, router, "GET", "/api/admin/users", owner, "", http.StatusForbidden)

	// Теперь последний — friend: ни сам с себя, ни повторным запросом.
	call(t, router, "PUT", friendPath, friend, `{"admin":false}`, http.StatusConflict)
	call(t, router, "PUT", ownerPath, friend, `{"admin":false}`, http.StatusOK) // уже не админ — не ошибка
	if n := adminCount(t, db); n != 1 {
		t.Errorf("%d admins, want 1", n)
	}
	call(t, router, "GET", "/api/admin/users", friend, "", http.StatusOK)
}

// Два админа снимают права друг с друга одновременно: проходит один запрос,
// и админ остаётся.
func TestSetUserAdmin_ConcurrentRevokesKeepOneAdmin(t *testing.T) {
	db, router, owner, _ := newTestServer(t)
	friend := invitedUser(t, router, owner, "friend")
	users := usersByName(t, router, owner)
	ownerPath, friendPath := "", "/api/admin/users/"+users["friend"].ID+"/admin"
	for name, u := range users {
		if name != "friend" {
			ownerPath = "/api/admin/users/" + u.ID + "/admin"
		}
	}

	for round := 0; round < 20; round++ {
		if _, err := db.Exec(`UPDATE users SET is_admin = true`); err != nil {
			t.Fatalf("making both admins: %v", err)
		}
		codes := make([]int, 2)
		var wg sync.WaitGroup
		for i, c := range []struct{ path, token string }{{friendPath, owner}, {ownerPath, friend}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = adminRequest(router, "PUT", c.path, c.token, []byte(`{"admin":false}`), "application/json").Code
			}()
		}
		wg.Wait()
		// Опоздавший уже не админ: ему 403.
		if !(codes[0] == http.StatusOK && codes[1] == http.StatusForbidden) && !(codes[0] == http.StatusForbidden && codes[1] == http.StatusOK) {
			t.Fatalf("round %d: statuses %v, want one 200 and one 403", round, codes)
		}
		if n := adminCount(t, db); n != 1 {
			t.Fatalf("round %d: %d admins left, want 1", round, n)
		}
	}
}
