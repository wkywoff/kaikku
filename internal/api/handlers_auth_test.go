package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kaikku/internal/auth"
)

func newAuthTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	srv := &Server{
		DB:         openTestDB(t),
		JWTSecret:  []byte("test-secret-at-least-32-bytes-long!!"),
		StorageDir: t.TempDir(),
	}
	return srv, NewRouter(srv)
}

func register(router http.Handler, username, invite string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"username": username, "password": "testpass123", "invite": invite})
	return postJSON(router, "/api/register", string(body), "")
}

func tokenOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct{ Token string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Token
}

func request(router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func newInvite(t *testing.T, router http.Handler, adminToken string) string {
	t.Helper()
	rec := postJSON(router, "/api/invites", "", adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create invite: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct{ Code string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Code
}

// Форма входа спрашивает, нужен ли код: на пустой базе — нет.
func TestRegisterInfo_FirstUser(t *testing.T) {
	_, router := newAuthTestServer(t)
	first := func() bool {
		rec := request(router, "GET", "/api/register", "")
		var resp struct {
			FirstUser *bool `json:"first_user"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &resp); rec.Code != http.StatusOK || resp.FirstUser == nil {
			t.Fatalf("register info: %d %s", rec.Code, rec.Body.String())
		}
		return *resp.FirstUser
	}
	if !first() {
		t.Error("empty database: want first_user")
	}
	tokenOf(t, register(router, "admin", ""))
	if first() {
		t.Error("after the first sign-up: want an invite")
	}
}

func TestRegister_FirstUserIsAdminThenInviteOnly(t *testing.T) {
	_, router := newAuthTestServer(t)

	admin := tokenOf(t, register(router, "owner", ""))
	var me struct {
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
	}
	json.Unmarshal(request(router, "GET", "/api/me", admin).Body.Bytes(), &me)
	if me.Username != "owner" || !me.IsAdmin {
		t.Fatalf("first user: %+v, want admin", me)
	}

	if rec := register(router, "stranger", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("register without invite: status %d, want 403", rec.Code)
	}
	if rec := register(router, "stranger", "made-up-code"); rec.Code != http.StatusForbidden {
		t.Fatalf("register with unknown invite: status %d, want 403", rec.Code)
	}

	code := newInvite(t, router, admin)
	friend := tokenOf(t, register(router, "friend", code))
	json.Unmarshal(request(router, "GET", "/api/me", friend).Body.Bytes(), &me)
	if me.IsAdmin {
		t.Errorf("invited user must not be admin")
	}

	if rec := register(router, "friend2", code); rec.Code != http.StatusForbidden {
		t.Errorf("reusing an invite: status %d, want 403", rec.Code)
	}
}

func TestRegister_ExpiredInvite(t *testing.T) {
	srv, router := newAuthTestServer(t)
	admin := tokenOf(t, register(router, "owner", ""))
	code := newInvite(t, router, admin)
	srv.DB.Exec(`UPDATE invites SET expires_at = now() - interval '1 minute' WHERE code = $1`, code)

	if rec := register(router, "late", code); rec.Code != http.StatusForbidden {
		t.Errorf("expired invite: status %d, want 403", rec.Code)
	}
}

func TestAdminOnlyRoutes(t *testing.T) {
	srv, router := newAuthTestServer(t)
	admin := tokenOf(t, register(router, "owner", ""))
	user := tokenOf(t, register(router, "friend", newInvite(t, router, admin)))

	var artistID, albumID, trackID string
	srv.DB.QueryRow(`INSERT INTO artists (name, deezer_id) VALUES ('A', '1') RETURNING id`).Scan(&artistID)
	srv.DB.QueryRow(`INSERT INTO albums (artist_id, title, cover_key, deezer_id) VALUES ($1, 'B', 'covers/c.jpg', '2') RETURNING id`, artistID).Scan(&albumID)
	srv.DB.QueryRow(`INSERT INTO tracks (album_id, title, duration_ms, storage_key, deezer_id) VALUES ($1, 'T', 1000, 't.m4a', '3') RETURNING id`, albumID).Scan(&trackID)
	os.MkdirAll(filepath.Join(srv.StorageDir, "covers"), 0o755)
	for _, f := range []string{"t.m4a", "covers/c.jpg"} {
		os.WriteFile(filepath.Join(srv.StorageDir, f), []byte("x"), 0o644)
	}

	if rec := postJSON(router, "/api/invites", "", user); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin invite: status %d, want 403", rec.Code)
	}
	if rec := request(router, "DELETE", "/api/tracks/"+trackID, user); rec.Code != http.StatusForbidden {
		t.Errorf("non-admin delete: status %d, want 403", rec.Code)
	}
	if rec := postJSON(router, "/api/tracks", `{}`, admin); rec.Code == http.StatusCreated {
		t.Errorf("manual track creation must be gone")
	}

	if rec := request(router, "DELETE", "/api/albums/"+albumID, admin); rec.Code != http.StatusNoContent {
		t.Fatalf("admin album delete: status %d, body %s", rec.Code, rec.Body.String())
	}
	for _, f := range []string{"t.m4a", "covers/c.jpg"} {
		if _, err := os.Stat(filepath.Join(srv.StorageDir, f)); !os.IsNotExist(err) {
			t.Errorf("%s should be deleted with the album", f)
		}
	}
	var n int
	srv.DB.QueryRow(`SELECT count(*) FROM tracks WHERE id = $1`, trackID).Scan(&n)
	if n != 0 {
		t.Errorf("album's tracks should be deleted")
	}
}

func TestRegister_RejectsBadInputBeforeHashing(t *testing.T) {
	_, router := newAuthTestServer(t)
	admin := tokenOf(t, register(router, "owner", ""))
	invite := newInvite(t, router, admin)

	reg := func(username, password, invite string) int {
		body, _ := json.Marshal(map[string]string{"username": username, "password": password, "invite": invite})
		return postJSON(router, "/api/register", string(body), "").Code
	}
	cases := []struct {
		name, username, password, invite string
		want                             int
	}{
		{"password over 256 bytes", "newbie", strings.Repeat("p", 300), invite, http.StatusBadRequest},
		{"space in the name", "new bie", "testpass123", invite, http.StatusBadRequest},
		{"name over 32 characters", strings.Repeat("n", 33), "testpass123", invite, http.StatusBadRequest},
		{"invisible character", "new​bie", "testpass123", invite, http.StatusBadRequest},
		{"wrong invite", "newbie", "testpass123", "not-a-code", http.StatusForbidden},
	}
	for _, c := range cases {
		if got := reg(c.username, c.password, c.invite); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
	if got := reg("new_bie.2", "testpass123", invite); got != http.StatusCreated {
		t.Errorf("a valid registration: status %d", got)
	}
}

// Тело запроса больше 64 КБ не дочитывается — даже без авторизации.
func TestRequestBodyIsCapped(t *testing.T) {
	_, router := newAuthTestServer(t)
	body := `{"username":"owner","password":"` + strings.Repeat("p", 100<<10) + `"}`
	if rec := postJSON(router, "/api/login", body, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("oversized body: status %d, want 400", rec.Code)
	}
}

func TestLogin_PausesAfterRepeatedFailures(t *testing.T) {
	_, router := newAuthTestServer(t)
	tokenOf(t, register(router, "owner", ""))

	login := func(password string) *httptest.ResponseRecorder {
		return postJSON(router, "/api/login", `{"username":"owner","password":"`+password+`"}`, "")
	}
	for i := 0; i < loginFreeAttempts; i++ {
		if rec := login("wrongpass"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, rec.Code)
		}
	}
	// Дальше — пауза, даже с правильным паролем.
	rec := login("testpass123")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("after %d failures: status %d, Retry-After %q; want 429 with Retry-After",
			loginFreeAttempts, rec.Code, rec.Header().Get("Retry-After"))
	}
	// Неизвестные имена считаются так же (и проверяются столько же времени).
	for i := 0; i < loginFreeAttempts; i++ {
		postJSON(router, "/api/login", `{"username":"ghost","password":"whatever1"}`, "")
	}
	if rec := postJSON(router, "/api/login", `{"username":"ghost","password":"whatever1"}`, ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("unknown name after failures: status %d, want 429", rec.Code)
	}
}

func TestLoginGuard(t *testing.T) {
	var g loginGuard
	t0 := time.Now()
	for i := 0; i < loginFreeAttempts-1; i++ {
		g.failed("a", t0)
	}
	if w := g.wait("a", t0); w != 0 {
		t.Fatalf("before the limit: wait %v", w)
	}
	g.failed("a", t0)
	if w := g.wait("a", t0.Add(10*time.Second)); w != 20*time.Second {
		t.Errorf("10 s after the 5th failure: wait %v, want 20s", w)
	}
	if w := g.wait("a", t0.Add(loginPauseFirst)); w != 0 {
		t.Errorf("after the pause: wait %v, want 0", w)
	}
	if w := g.wait("b", t0); w != 0 {
		t.Errorf("another name must not wait: %v", w)
	}
	if w := g.wait("a", t0.Add(loginWindow+time.Second)); w != 0 {
		t.Errorf("old failures must be forgotten: %v", w)
	}
	g.succeeded("a")
	if w := g.wait("a", t0); w != 0 {
		t.Errorf("a successful login clears the failures: %v", w)
	}
}

// Каждая следующая неудача удваивает паузу, но не дальше 15 минут: подбор
// упирается в сотню попыток в сутки.
func TestLoginGuard_PauseGrows(t *testing.T) {
	var g loginGuard
	t0 := time.Now()
	for i := 0; i < loginFreeAttempts; i++ {
		g.failed("a", t0)
	}
	now := t0
	for _, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		if w := g.wait("a", now); w != want {
			t.Fatalf("pause %v, want %v", w, want)
		}
		now = now.Add(want)
		g.failed("a", now)
	}
	attempts, now := 0, t0
	g = loginGuard{}
	for now.Before(t0.Add(24 * time.Hour)) {
		now = now.Add(g.wait("a", now))
		g.failed("a", now)
		attempts++
	}
	if attempts > 110 {
		t.Errorf("%d attempts in a day", attempts)
	}
}

// Пользователя, который что-то скачивал, можно удалить (его задания
// остаются без автора), и его токен сразу перестаёт работать.
func TestDeletedUserLosesAccess(t *testing.T) {
	db, router, admin := newTestRouter(t)
	friend := invitedUser(t, router, admin, "leaving-friend")
	call(t, router, "PUT", "/api/me/liked/12345", friend, "", http.StatusNoContent) // ставит задание на скачивание

	if _, err := db.Exec(`DELETE FROM users WHERE username = 'leaving-friend'`); err != nil {
		t.Fatalf("deleting a user with download jobs: %v", err)
	}
	var orphaned int
	db.QueryRow(`SELECT count(*) FROM ingest_jobs WHERE requested_by IS NULL`).Scan(&orphaned)
	if orphaned != 1 {
		t.Errorf("the job should stay, without its author: %d", orphaned)
	}
	call(t, router, "GET", "/api/me/collection", friend, "", http.StatusUnauthorized)
	call(t, router, "GET", "/api/me/collection", admin, "", http.StatusOK)
}

func TestHealthVersion(t *testing.T) {
	_, router, _ := newTestRouter(t)
	defer func(v string) { Version = v }(Version)
	Version = "1.2.0"
	rec := request(router, "GET", "/api/health", "")
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || resp["status"] != "ok" || resp["version"] != "1.2.0" {
		t.Errorf("health: %d %v", rec.Code, resp)
	}
}

func TestTokenRenewal(t *testing.T) {
	_, router, token, srv := newTestServer(t)
	claims, err := auth.VerifyToken(token, srv.JWTSecret)
	if err != nil {
		t.Fatal(err)
	}

	// Свежий токен не продлевается.
	rec := request(router, "GET", "/api/me", token)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Auth-Token") != "" {
		t.Fatalf("fresh token: %d, renewed %q", rec.Code, rec.Header().Get("X-Auth-Token"))
	}

	// Двухдневный — ответ несёт новый, выданный сейчас, на тот же срок.
	old, err := auth.IssueTokenAt(claims.UserID, time.Now().Add(-48*time.Hour), tokenTTL, srv.JWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	rec = request(router, "GET", "/api/me", old)
	fresh := rec.Header().Get("X-Auth-Token")
	if rec.Code != http.StatusOK || fresh == "" {
		t.Fatalf("old token: %d, renewed %q", rec.Code, fresh)
	}
	got, err := auth.VerifyToken(fresh, srv.JWTSecret)
	if err != nil || got.UserID != claims.UserID || time.Since(time.Unix(got.Iat, 0)) > time.Minute ||
		time.Until(time.Unix(got.Exp, 0)) < tokenTTL-time.Minute {
		t.Fatalf("renewed token: %+v, %v", got, err)
	}

	// После смены пароля старый токен не продлевается, а не действует.
	if _, err := srv.DB.Exec(`UPDATE users SET password_changed_at = now() WHERE id = $1`, claims.UserID); err != nil {
		t.Fatal(err)
	}
	rec = request(router, "GET", "/api/me", old)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-Auth-Token") != "" {
		t.Fatalf("after password change: %d, renewed %q", rec.Code, rec.Header().Get("X-Auth-Token"))
	}
}
