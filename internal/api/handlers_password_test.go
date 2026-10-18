package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func loginAs(router http.Handler, username, password string) int {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return postJSON(router, "/api/login", string(body), "").Code
}

func TestChangePassword(t *testing.T) {
	_, router := newAuthTestServer(t)
	owner := tokenOf(t, register(router, "owner", ""))
	device2 := func() string { // второе устройство: токен, выданный до смены пароля
		body, _ := json.Marshal(map[string]string{"username": "owner", "password": "testpass123"})
		var r struct{ Token string }
		json.Unmarshal(postJSON(router, "/api/login", string(body), "").Body.Bytes(), &r)
		return r.Token
	}()

	put := func(token, body string) (int, string) {
		rec := adminRequest(router, "PUT", "/api/me/password", token, []byte(body), "application/json")
		var r struct{ Token string }
		json.Unmarshal(rec.Body.Bytes(), &r)
		return rec.Code, r.Token
	}
	if code, _ := put(owner, `{"old_password":"wrong-pass","new_password":"brandnew123"}`); code != http.StatusForbidden {
		t.Fatalf("wrong current password: %d", code)
	}
	if code, _ := put(owner, `{"old_password":"testpass123","new_password":"short"}`); code != http.StatusBadRequest {
		t.Fatalf("short new password: %d", code)
	}
	code, fresh := put(owner, `{"old_password":"testpass123","new_password":"brandnew123"}`)
	if code != http.StatusOK || fresh == "" {
		t.Fatalf("change: %d", code)
	}

	if loginAs(router, "owner", "testpass123") != http.StatusUnauthorized || loginAs(router, "owner", "brandnew123") != http.StatusOK {
		t.Error("the new password must work and the old one must not")
	}
	if rec := request(router, "GET", "/api/me", device2); rec.Code != http.StatusUnauthorized {
		t.Errorf("a session from before the change must end: %d", rec.Code)
	}
	if rec := request(router, "GET", "/api/me", fresh); rec.Code != http.StatusOK {
		t.Errorf("the token from the change must work: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPasswordResetLink(t *testing.T) {
	_, router := newAuthTestServer(t)
	admin := tokenOf(t, register(router, "owner", ""))
	friendRec := register(router, "friend", newInvite(t, router, admin))
	friend := tokenOf(t, friendRec)

	var list struct {
		Users []struct{ ID, Username string }
	}
	json.Unmarshal(request(router, "GET", "/api/admin/users", admin).Body.Bytes(), &list)
	friendID := ""
	for _, u := range list.Users {
		if u.Username == "friend" {
			friendID = u.ID
		}
	}
	if friendID == "" {
		t.Fatalf("users: %+v", list)
	}
	if rec := request(router, "GET", "/api/admin/users", friend); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin user list: %d", rec.Code)
	}
	if rec := postJSON(router, "/api/admin/users/"+friendID+"/password-reset", "", friend); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin reset: %d", rec.Code)
	}

	newLink := func() string {
		rec := postJSON(router, "/api/admin/users/"+friendID+"/password-reset", "", admin)
		var r struct{ Code string }
		json.Unmarshal(rec.Body.Bytes(), &r)
		if rec.Code != http.StatusCreated || r.Code == "" {
			t.Fatalf("create link: %d %s", rec.Code, rec.Body.String())
		}
		return r.Code
	}
	stale := newLink()
	code := newLink() // новая ссылка отменяет старую
	reset := func(code, password string) int {
		body, _ := json.Marshal(map[string]string{"code": code, "password": password})
		return postJSON(router, "/api/password-reset", string(body), "").Code
	}
	if c := reset(stale, "resetpass123"); c != http.StatusForbidden {
		t.Errorf("an older link must stop working: %d", c)
	}
	if c := reset("not-a-code", "resetpass123"); c != http.StatusForbidden {
		t.Errorf("a made-up link: %d", c)
	}
	if c := reset(code, "short"); c != http.StatusBadRequest {
		t.Errorf("short password: %d", c)
	}
	if c := reset(code, "resetpass123"); c != http.StatusOK {
		t.Fatalf("reset: %d", c)
	}
	if c := reset(code, "another123"); c != http.StatusForbidden {
		t.Errorf("a link works once: %d", c)
	}
	if loginAs(router, "friend", "resetpass123") != http.StatusOK || loginAs(router, "friend", "testpass123") != http.StatusUnauthorized {
		t.Error("the new password must work and the old one must not")
	}
	if rec := request(router, "GET", "/api/me", friend); rec.Code != http.StatusUnauthorized {
		t.Errorf("old sessions must end after a reset: %d", rec.Code)
	}
	if rec := request(router, "GET", "/api/me", admin); rec.Code != http.StatusOK {
		t.Errorf("the admin's own session is not affected: %d", rec.Code)
	}
}
