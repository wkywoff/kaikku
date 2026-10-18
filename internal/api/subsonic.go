package api

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"kaikku/internal/deezer"
)

// Subsonic API (subsonic.org/pages/api.jsp) с расширениями OpenSubsonic —
// для приложений: Feishin, Amperfy, Symfonium и других. Адрес —
// /rest/<метод> (с .view или без), GET или POST-форма; ответ — XML, с f=json
// — JSON. Данные отдают те же обработчики, что и сайту (call): правки
// админа, скрытые релизы, черновики и права на плейлисты — как в вебе.
//
// Вход — паролем для приложений (users.subsonic_secret), не основным:
// клиенты шлют md5(пароль + соль), а основной хранится только как PBKDF2.

const subsonicVersion = "1.16.1"

// Коды ошибок Subsonic.
const (
	subErrGeneric      = 0
	subErrMissing      = 10
	subErrWrongCreds   = 40
	subErrAuthNotAllow = 42
	subErrForbidden    = 50
	subErrNotFound     = 70
)

type subsonicError struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

func (e *subsonicError) Error() string { return e.Message }

func subFail(code int, msg string) error { return &subsonicError{Code: code, Message: msg} }

func subMissing(name string) error {
	return subFail(subErrMissing, "required parameter is missing: "+name)
}

// subCtx — один запрос к /rest/.
type subCtx struct {
	s      *Server
	w      http.ResponseWriter
	r      *http.Request // после входа — с пользователем в контексте
	json   bool
	userID string
	user   string
	admin  bool
}

func (c *subCtx) param(name string) string { return c.r.Form.Get(name) }

func (c *subCtx) params(name string) []string { return c.r.Form[name] }

type subsonicMethod struct {
	run    func(*subCtx) error
	public bool // без входа (getOpenSubsonicExtensions)
}

// subsonicMethods заполняют файлы subsonic_*.go.
var subsonicMethods = map[string]subsonicMethod{}

func subsonicRoute(name string, run func(*subCtx) error) {
	subsonicMethods[name] = subsonicMethod{run: run}
}

func (s *Server) handleSubsonic(w http.ResponseWriter, r *http.Request) {
	// Веб-клиенты (Feishin в браузере) ходят с другого адреса; пароль — в
	// параметрах, куки не нужны.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	c := &subCtx{s: s, w: w, r: r}
	if err := r.ParseForm(); err != nil {
		c.fail(subFail(subErrGeneric, "could not read the parameters"))
		return
	}
	c.json = c.param("f") == "json"
	m, ok := subsonicMethods[strings.TrimSuffix(r.PathValue("method"), ".view")]
	if !ok {
		c.fail(subFail(subErrGeneric, "not implemented"))
		return
	}
	if !m.public {
		if err := c.login(); err != nil {
			c.fail(err)
			return
		}
	}
	if err := m.run(c); err != nil {
		c.fail(err)
	}
}

// --- вход ---

func (c *subCtx) login() error {
	if c.param("apiKey") != "" {
		return subFail(subErrAuthNotAllow, "use the username and the app password")
	}
	name := c.param("u")
	if name == "" {
		return subMissing("u")
	}
	// Подбор — как у входа на сайт (loginguard.go), но счёт свой.
	key := "subsonic:" + name
	now := time.Now()
	if c.s.logins.wait(key, now) > 0 {
		return subFail(subErrWrongCreds, "too many failed attempts, try again later")
	}
	var id string
	var admin bool
	var sealed []byte
	err := c.s.DB.QueryRowContext(c.r.Context(),
		`SELECT id, is_admin, subsonic_secret FROM users WHERE username = $1`, name).Scan(&id, &admin, &sealed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var password string
	if sealed != nil {
		if password, err = c.s.openSubsonicSecret(sealed); err != nil {
			log.Printf("subsonic: app password of %s: %v", name, err) // сменился JWT_SECRET
		}
	}
	token, salt, plain := c.param("t"), c.param("s"), c.param("p")
	var ok bool
	switch {
	case token != "":
		if salt == "" {
			return subMissing("s")
		}
		sum := md5.Sum([]byte(password + salt))
		ok = subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(strings.ToLower(token))) == 1
	case plain != "":
		if h, found := strings.CutPrefix(plain, "enc:"); found {
			b, _ := hex.DecodeString(h)
			plain = string(b)
		}
		ok = subtle.ConstantTimeCompare([]byte(plain), []byte(password)) == 1
	default:
		return subMissing("p")
	}
	if !ok || password == "" {
		c.s.logins.failed(key, now)
		return subFail(subErrWrongCreds, "wrong username or app password")
	}
	c.s.logins.succeeded(key)
	c.userID, c.user, c.admin = id, name, admin
	ctx := context.WithValue(c.r.Context(), userIDCtxKey, id)
	c.r = c.r.WithContext(context.WithValue(ctx, isAdminCtxKey, admin))
	return nil
}

// Пароль для приложений шифруется ключом из JWT_SECRET: дамп базы без
// .env его не выдаёт. Сменился JWT_SECRET — пароль надо создать заново.
func (s *Server) subsonicAEAD() (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, s.JWTSecret, nil, "kaikku subsonic app password", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Server) sealSubsonicSecret(password string) ([]byte, error) {
	aead, err := s.subsonicAEAD()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	return aead.Seal(nonce, nonce, []byte(password), nil), nil
}

func (s *Server) openSubsonicSecret(sealed []byte) (string, error) {
	aead, err := s.subsonicAEAD()
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", errors.New("too short")
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
	return string(plain), err
}

// newAppPassword — 16 знаков без похожих (0/o, 1/l/i): его набирают руками
// на телефоне.
func newAppPassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 16)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

// --- пароль для приложений на сайте (Настройки) ---

type subsonicAccess struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

func (s *Server) subsonicAccessOf(userID string) (subsonicAccess, error) {
	var a subsonicAccess
	var sealed []byte
	if err := s.DB.QueryRow(`SELECT username, subsonic_secret FROM users WHERE id = $1`, userID).Scan(&a.Username, &sealed); err != nil {
		return a, err
	}
	if sealed != nil {
		if p, err := s.openSubsonicSecret(sealed); err == nil {
			a.Enabled, a.Password = true, p
		}
	}
	return a, nil
}

// GET /api/me/subsonic — включён ли вход из приложений, логин и пароль.
func (s *Server) handleSubsonicAccess(w http.ResponseWriter, r *http.Request) {
	a, err := s.subsonicAccessOf(currentUser(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// POST /api/me/subsonic — новый пароль для приложений (старый перестаёт работать).
func (s *Server) handleNewSubsonicPassword(w http.ResponseWriter, r *http.Request) {
	sealed, err := s.sealSubsonicSecret(newAppPassword())
	if err == nil {
		_, err = s.DB.Exec(`UPDATE users SET subsonic_secret = $2 WHERE id = $1`, currentUser(r), sealed)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the password")
		return
	}
	s.handleSubsonicAccess(w, r)
}

// DELETE /api/me/subsonic — выключить вход из приложений.
func (s *Server) handleDisableSubsonic(w http.ResponseWriter, r *http.Request) {
	if _, err := s.DB.Exec(`UPDATE users SET subsonic_secret = NULL WHERE id = $1`, currentUser(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not turn it off")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- ответ ---

// ok отвечает payload под именем key ("" — пустой ответ, как у ping).
func (c *subCtx) ok(key string, payload any) error {
	c.write("ok", key, payload)
	return nil
}

func (c *subCtx) fail(err error) {
	if errors.Is(err, context.Canceled) && c.r.Context().Err() != nil {
		return // приложение уже не ждёт ответа
	}
	var se *subsonicError
	if !errors.As(err, &se) {
		log.Printf("subsonic %s: %v", c.r.PathValue("method"), err)
		se = &subsonicError{Code: subErrGeneric, Message: "internal error"}
	}
	c.write("failed", "error", se)
}

func (c *subCtx) write(status, key string, payload any) {
	if c.json {
		body := map[string]any{
			"status": status, "version": subsonicVersion, "type": "kaikku",
			"serverVersion": Version, "openSubsonic": true,
		}
		if key != "" {
			body[key] = payload
		}
		writeJSON(c.w, http.StatusOK, map[string]any{"subsonic-response": body})
		return
	}
	c.w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	c.w.WriteHeader(http.StatusOK)
	io.WriteString(c.w, xml.Header)
	enc := xml.NewEncoder(c.w)
	attr := func(name, value string) xml.Attr { return xml.Attr{Name: xml.Name{Local: name}, Value: value} }
	start := xml.StartElement{Name: xml.Name{Local: "subsonic-response"}, Attr: []xml.Attr{
		attr("xmlns", "http://subsonic.org/restapi"), attr("status", status), attr("version", subsonicVersion),
		attr("type", "kaikku"), attr("serverVersion", Version), attr("openSubsonic", "true"),
	}}
	enc.EncodeToken(start)
	if key != "" {
		if err := enc.EncodeElement(payload, xml.StartElement{Name: xml.Name{Local: key}}); err != nil {
			log.Printf("subsonic %s: xml: %v", c.r.PathValue("method"), err)
		}
	}
	enc.EncodeToken(start.End())
	enc.Flush()
}

// --- доля лимита Deezer ---

// Приложения открывают альбомы сотнями (Feishin — всю дискографию артиста
// разом). Чтобы сайт не ждал за ними, их запросы к Deezer идут своей
// долей лимита (5/8 того, что пропускают выходы к Deezer: 25 из 40 за
// 5 с, со вторым выходом — 50 из 80; остальное — сайту) и не больше
// appSlots альбомов сразу; брошенный приложением запрос уходит из очереди,
// не потратив лимит.
var (
	appShares sync.Map // запросов за 5 с → *deezer.Limiter
	appSlots  = make(chan struct{}, 4)
)

// appDeezerShare — доля приложений при нынешней ёмкости: упал прокси —
// доля меньше.
func (s *Server) appDeezerShare() *deezer.Limiter {
	n := s.Deezer.Capacity() * 5 / 8
	if n <= 0 {
		n = 25 // без ограничения (тесты)
	}
	l, _ := appShares.LoadOrStore(n, deezer.NewLimiter(n, 5*time.Second))
	return l.(*deezer.Limiter)
}

// deezerShare ждёт места в очереди и cost запросов из доли приложений;
// release — отпустить место.
func (c *subCtx) deezerShare(cost int) (release func(), err error) {
	if cost == 0 {
		return func() {}, nil
	}
	ctx := c.r.Context()
	select {
	case appSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release = func() { <-appSlots }
	share := c.s.appDeezerShare()
	for range cost {
		if err := share.WaitContext(ctx); err != nil {
			release()
			return nil, err
		}
	}
	return release, nil
}

// --- обработчики сайта изнутри ---

// fromAppCtxKey — запрос пришёл от приложения (call): обработчики сайта
// не тратят на него лимит Deezer там, где можно обойтись (фиты в альбоме).
const fromAppCtxKey ctxKey = "subsonic"

func fromApp(r *http.Request) bool {
	v, _ := r.Context().Value(fromAppCtxKey).(bool)
	return v
}

// recorder — ответ обработчика сайта в память.
type recorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (rec *recorder) Header() http.Header         { return rec.header }
func (rec *recorder) Write(b []byte) (int, error) { return rec.body.Write(b) }
func (rec *recorder) WriteHeader(code int)        { rec.code = code }

// call runs one of the site's JSON handlers as the logged-in user and
// decodes its answer into out (nil — not needed). target carries the query
// (?q=…), vals — the {…} of the handler's route, body is sent as JSON.
func (c *subCtx) call(h http.HandlerFunc, method, target string, vals map[string]string, body, out any) error {
	var rd io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.WithValue(c.r.Context(), fromAppCtxKey, true), method, target, rd)
	if err != nil {
		return err
	}
	for k, v := range vals {
		req.SetPathValue(k, v)
	}
	rec := &recorder{header: http.Header{}, code: http.StatusOK}
	h(rec, req)
	if rec.code >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(rec.body.Bytes(), &e)
		switch rec.code {
		case http.StatusNotFound:
			return subFail(subErrNotFound, e.Error)
		case http.StatusForbidden:
			return subFail(subErrForbidden, e.Error)
		default:
			return subFail(subErrGeneric, e.Error)
		}
	}
	if out != nil && rec.body.Len() > 0 {
		return json.Unmarshal(rec.body.Bytes(), out)
	}
	return nil
}

// --- служебное ---

func init() {
	subsonicRoute("ping", func(c *subCtx) error { return c.ok("", nil) })
	subsonicRoute("getLicense", func(c *subCtx) error {
		return c.ok("license", struct {
			Valid bool `xml:"valid,attr" json:"valid"`
		}{true})
	})
	subsonicMethods["getOpenSubsonicExtensions"] = subsonicMethod{public: true, run: func(c *subCtx) error {
		type ext struct {
			Name     string `xml:"name,attr" json:"name"`
			Versions []int  `xml:"versions" json:"versions"`
		}
		return c.ok("openSubsonicExtensions", []ext{{"formPost", []int{1}}, {"songLyrics", []int{1}}})
	}}
	subsonicRoute("getMusicFolders", func(c *subCtx) error {
		type folder struct {
			ID   int    `xml:"id,attr" json:"id"`
			Name string `xml:"name,attr" json:"name"`
		}
		return c.ok("musicFolders", struct {
			Folders []folder `xml:"musicFolder" json:"musicFolder"`
		}{[]folder{{1, "kaikku"}}})
	})
	subsonicRoute("getUser", subsonicUser)
	subsonicRoute("getScanStatus", subsonicScanStatus)
	subsonicRoute("startScan", subsonicScanStatus)

	// Чего у нас нет — пустые ответы: клиенты спрашивают это при старте.
	empty := func(key string) func(*subCtx) error {
		return func(c *subCtx) error { return c.ok(key, struct{}{}) }
	}
	subsonicRoute("getNowPlaying", empty("nowPlaying"))
	subsonicRoute("getBookmarks", empty("bookmarks"))
	subsonicRoute("getInternetRadioStations", empty("internetRadioStations"))
	subsonicRoute("getPodcasts", empty("podcasts"))
	subsonicRoute("getNewestPodcasts", empty("newestPodcasts"))
	subsonicRoute("getShares", empty("shares"))
	subsonicRoute("getPlayQueue", func(c *subCtx) error { return c.ok("", nil) })
	subsonicRoute("savePlayQueue", func(c *subCtx) error { return c.ok("", nil) })
}

func subsonicScanStatus(c *subCtx) error {
	return c.ok("scanStatus", struct {
		Scanning bool `xml:"scanning,attr" json:"scanning"`
		Count    int  `xml:"count,attr" json:"count"`
	}{false, 0})
}

type subsonicUserInfo struct {
	Username          string `xml:"username,attr" json:"username"`
	ScrobblingEnabled bool   `xml:"scrobblingEnabled,attr" json:"scrobblingEnabled"`
	AdminRole         bool   `xml:"adminRole,attr" json:"adminRole"`
	SettingsRole      bool   `xml:"settingsRole,attr" json:"settingsRole"`
	DownloadRole      bool   `xml:"downloadRole,attr" json:"downloadRole"`
	UploadRole        bool   `xml:"uploadRole,attr" json:"uploadRole"`
	PlaylistRole      bool   `xml:"playlistRole,attr" json:"playlistRole"`
	CoverArtRole      bool   `xml:"coverArtRole,attr" json:"coverArtRole"`
	CommentRole       bool   `xml:"commentRole,attr" json:"commentRole"`
	PodcastRole       bool   `xml:"podcastRole,attr" json:"podcastRole"`
	StreamRole        bool   `xml:"streamRole,attr" json:"streamRole"`
	JukeboxRole       bool   `xml:"jukeboxRole,attr" json:"jukeboxRole"`
	ShareRole         bool   `xml:"shareRole,attr" json:"shareRole"`
	Folders           []int  `xml:"folder" json:"folder"`
}

// getUser — только о себе: другие пользователи приложениям не нужны.
func subsonicUser(c *subCtx) error {
	if name := c.param("username"); name != "" && name != c.user {
		return subFail(subErrForbidden, "only your own user")
	}
	return c.ok("user", subsonicUserInfo{
		Username: c.user, ScrobblingEnabled: true, DownloadRole: true, PlaylistRole: true, StreamRole: true,
		Folders: []int{1},
	})
}
