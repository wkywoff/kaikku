package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"kaikku/internal/auth"
	"kaikku/internal/deezer"
	"kaikku/internal/lastfm"
	"kaikku/internal/lyrics"
	"kaikku/internal/media"
)

type Server struct {
	DB          *sql.DB
	JWTSecret   []byte
	MediaSigner *media.Signer
	Deezer      *deezer.Client
	// DeezerCache — кеш Deezer в базе (миграция 0033) для страниц артистов,
	// дискографий и чарта (storedCached); nil — только память.
	DeezerCache deezer.Store
	// DeezerSite — описания артистов с сайта Deezer (bio.go); nil — без них.
	DeezerSite *deezer.Site
	// StorageDir — тот же каталог, что STORAGE_DIR воркера; нужен API, чтобы
	// удаление трека/альбома удаляло и файлы. Пусто — файлы не трогаем.
	StorageDir string
	// Streams отдаёт ещё не скачанные треки потоком (stream.go); nil — выключено.
	Streams *StreamHub
	// Background запускает фоновую работу (скачивание лайкнутого и т.п.);
	// nil — горутина. Тесты подставляют синхронный запуск.
	Background func(func())

	// Lyrics — тексты песен (handlers_lyrics.go, EnableLyrics); nil — выключено.
	Lyrics *lyrics.Service

	// Lastfm — скробблинг (lastfm.go); nil — выключено.
	Lastfm     *lastfm.Client
	lastfmWake chan struct{} // будит отправку очереди (StartLastfm)

	// FrontendIndex — index.html собранного фронта: в него вставляются
	// og-теги для превью ссылок (og.go). Пусто — превью выключены.
	FrontendIndex string
	// PublicURL — адрес сайта (https://…) для абсолютных ссылок в превью
	// и картинок в Subsonic-приложениях (MEDIA_BASE_URL).
	PublicURL  string
	frontIndex fileCache

	cache  ttlCache   // ответы Deezer для подсказок и чартов (cache.go)
	logins loginGuard // неудачные попытки входа (loginguard.go)
}

type ctxKey string

const (
	userIDCtxKey  ctxKey = "user_id"
	isAdminCtxKey ctxKey = "is_admin"
)

func NewRouter(s *Server) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	// Превью ссылок (og.go) — без входа; nginx шлёт сюда только эти адреса.
	if s.FrontendIndex != "" {
		mux.HandleFunc("GET /album/{id}", s.handleOG("album"))
		mux.HandleFunc("GET /artist/{id}", s.handleOG("artist"))
		mux.HandleFunc("GET /playlist/{id}", s.handleOG("playlist"))
	}
	// Subsonic API для приложений (subsonic.go) — свой вход, паролем для приложений.
	mux.HandleFunc("/rest/{method}", s.handleSubsonic)
	mux.Handle("GET /api/me/subsonic", s.authed(s.handleSubsonicAccess))
	mux.Handle("POST /api/me/subsonic", s.authed(s.handleNewSubsonicPassword))
	mux.Handle("DELETE /api/me/subsonic", s.authed(s.handleDisableSubsonic))
	mux.HandleFunc("GET /api/register", s.handleRegisterInfo)
	mux.HandleFunc("POST /api/register", s.handleRegister)
	mux.HandleFunc("POST /api/login", s.handleLogin)

	mux.Handle("GET /api/me", s.authed(s.handleMe))
	mux.Handle("POST /api/invites", s.adminOnly(s.handleCreateInvite))
	mux.Handle("GET /api/admin/users", s.adminOnly(s.handleAdminUsers))
	mux.Handle("GET /api/lyrics/{id}", s.authed(s.handleLyrics))
	mux.Handle("POST /api/lyrics/{id}/refresh", s.adminOnly(s.handleRefreshLyrics))
	mux.Handle("POST /api/admin/users/{id}/password-reset", s.adminOnly(s.handleCreatePasswordReset))
	mux.HandleFunc("POST /api/password-reset", s.handlePasswordReset)
	mux.Handle("PUT /api/me/password", s.authed(s.handleChangePassword))

	// Каталог (наши строки артистов, альбомов, треков — по Deezer ID)
	// наполняет воркер; страницы строятся из ответов Deezer, а эти
	// эндпоинты — для правки админом и удаления.
	mux.Handle("GET /api/artists/{id}", s.authed(s.handleGetArtist))
	mux.Handle("PUT /api/artists/{id}", s.authed(s.artistEditor(s.handleUpdateArtist)))
	mux.Handle("POST /api/artists/{id}/reset", s.adminOnly(s.deezerRowOnly("artists", s.handleResetArtist)))
	mux.Handle("PUT /api/artists/{id}/picture", s.authed(s.artistEditor(s.handleUploadArtistPicture)))
	// Правка того, что ещё не скачано: завести строку по Deezer ID (handlers_claim.go).
	mux.Handle("POST /api/artists/deezer/{id}/claim", s.adminOnly(s.handleClaimArtist))
	mux.Handle("POST /api/albums/deezer/{id}/claim", s.adminOnly(s.handleClaimAlbum))
	mux.Handle("GET /api/tracks/deezer/{id}/edit", s.adminOnly(s.handleGetDeezerTrackEdit))
	mux.Handle("PUT /api/tracks/deezer/{id}", s.adminOnly(s.handleUpdateDeezerTrack))
	mux.Handle("POST /api/tracks/deezer/{id}/reset", s.adminOnly(s.handleResetDeezerTrack))
	mux.Handle("POST /api/tracks/deezer/{id}/redownload", s.adminOnly(s.handleRedownloadTrack))
	mux.Handle("PUT /api/tracks/deezer/{id}/audio", s.adminOnly(s.handleUploadTrackAudio))
	mux.Handle("PUT /api/tracks/deezer/{id}/video", s.adminOnly(s.handleSetTrackVideo))
	mux.Handle("GET /api/admin/missing", s.adminOnly(s.handleMissing))
	mux.Handle("POST /api/admin/missing/{job}/retry", s.adminOnly(s.handleRetryMissing))
	mux.Handle("PUT /api/albums/deezer/{id}/hidden", s.adminOnly(s.handleSetAlbumHidden))
	mux.Handle("POST /api/albums/deezer/{id}/tracklist/replace", s.adminOnly(s.handleReplaceAlbumTrack))
	mux.Handle("POST /api/albums/deezer/{id}/refresh", s.adminOnly(s.handleRefreshAlbum))

	mux.Handle("GET /api/albums/{id}", s.authed(s.deezerRowOnly("albums", s.handleGetAlbum)))
	mux.Handle("PUT /api/albums/{id}", s.adminOnly(s.deezerRowOnly("albums", s.handleUpdateAlbum)))
	mux.Handle("POST /api/albums/{id}/reset", s.adminOnly(s.deezerRowOnly("albums", s.handleResetAlbum)))
	mux.Handle("PUT /api/albums/{id}/cover", s.adminOnly(s.deezerRowOnly("albums", s.handleUploadAlbumCover)))
	mux.Handle("DELETE /api/albums/{id}", s.adminOnly(s.deezerRowOnly("albums", s.handleDeleteAlbum)))

	mux.Handle("DELETE /api/tracks/{id}", s.adminOnly(s.deezerRowOnly("tracks", s.handleDeleteTrack)))
	mux.Handle("GET /api/tracks/{id}/stream-url", s.authed(s.handleStreamURL))

	mux.Handle("GET /api/search", s.authed(s.handleSearch))
	mux.Handle("GET /api/search/artists", s.authed(s.handleSearchArtists))
	// Что из треков Deezer уже скачано. Ставят на скачивание лайк, сохранение
	// альбома, плейлист и воспроизведение (handlers_collection.go, stream.go).
	mux.Handle("GET /api/ingest/lookup", s.authed(s.handleIngestLookup))

	// Трек не из медиатеки — играет, пока скачивается (stream.go). Сам поток
	// без Bearer: <audio> его не шлёт, авторизация — подписью в ссылке.
	mux.Handle("GET /api/stream/deezer/{id}/url", s.authed(s.handleDeezerStreamURL))
	mux.HandleFunc("GET /api/stream/deezer/{id}", s.handleDeezerStream)

	// Личная медиатека (handlers_collection.go).
	mux.Handle("GET /api/me/collection", s.authed(s.handleCollectionIDs))
	mux.Handle("GET /api/me/liked", s.authed(s.handleLikedTracks))
	mux.Handle("PUT /api/me/liked/{id}", s.authed(s.handleLike))
	mux.Handle("DELETE /api/me/liked/{id}", s.authed(s.handleUnlike))
	mux.Handle("GET /api/me/albums", s.authed(s.handleSavedAlbums))
	mux.Handle("PUT /api/me/albums/{id}", s.authed(s.handleSaveAlbum))
	mux.Handle("DELETE /api/me/albums/{id}", s.authed(s.handleUnsaveAlbum))
	mux.Handle("GET /api/me/artists", s.authed(s.handleFollowedArtists))
	mux.Handle("PUT /api/me/artists/{id}", s.authed(s.handleFollow))
	mux.Handle("DELETE /api/me/artists/{id}", s.authed(s.handleUnfollow))
	mux.Handle("GET /api/me/releases", s.authed(s.handleNewReleases))
	mux.Handle("POST /api/me/plays", s.authed(s.handleRecordPlay))
	mux.Handle("GET /api/me/lastfm", s.authed(s.handleLastfmStatus))
	mux.Handle("POST /api/me/lastfm", s.authed(s.handleLastfmLink))
	mux.Handle("PATCH /api/me/lastfm", s.authed(s.handleLastfmSettings))
	mux.Handle("DELETE /api/me/lastfm", s.authed(s.handleLastfmUnlink))
	mux.Handle("POST /api/me/now-playing", s.authed(s.handleNowPlaying))
	mux.Handle("POST /api/me/scrobbles", s.authed(s.handleScrobble))
	mux.Handle("GET /api/me/recent", s.authed(s.handleRecent))
	mux.Handle("GET /api/me/library/times", s.authed(s.handleLibraryTimes))
	mux.Handle("GET /api/me/mixes", s.authed(s.handleMixes))
	mux.Handle("GET /api/genres", s.authed(s.handleGenres))
	mux.Handle("GET /api/genres/{slug}", s.authed(s.handleGenre))
	mux.Handle("GET /api/me/mixes/{id}", s.authed(s.handleMix))
	mux.Handle("GET /api/me/suggestions", s.authed(s.handleSuggestions))
	mux.Handle("GET /api/me/wrapped", s.authed(s.handleWrapped))
	mux.Handle("GET /api/playlists", s.authed(s.handlePlaylists))
	mux.Handle("POST /api/playlists", s.authed(s.handleCreatePlaylist))
	mux.Handle("GET /api/playlists/{id}", s.authed(s.handleGetPlaylist))
	mux.Handle("PUT /api/playlists/{id}", s.authed(s.handleUpdatePlaylist))
	mux.Handle("DELETE /api/playlists/{id}", s.authed(s.handleDeletePlaylist))
	mux.Handle("POST /api/playlists/{id}/tracks", s.authed(s.handleAddToPlaylist))
	mux.Handle("DELETE /api/playlists/{id}/tracks/{item}", s.authed(s.handleRemoveFromPlaylist))
	mux.Handle("PUT /api/playlists/{id}/tracks/{item}/position", s.authed(s.handleMovePlaylistItem))
	mux.Handle("PUT /api/playlists/{id}/cover", s.authed(s.handleUploadPlaylistCover))
	mux.Handle("DELETE /api/playlists/{id}/cover", s.authed(s.handleDeletePlaylistCover))
	mux.Handle("PUT /api/playlists/{id}/collaborators/{user}", s.authed(s.handleAddCollaborator))
	mux.Handle("DELETE /api/playlists/{id}/collaborators/{user}", s.authed(s.handleRemoveCollaborator))
	mux.Handle("GET /api/users/{name}", s.authed(s.handleProfile))
	mux.Handle("PUT /api/users/{id}/follow", s.authed(s.handleFollowUser))
	mux.Handle("DELETE /api/users/{id}/follow", s.authed(s.handleUnfollowUser))
	mux.Handle("GET /api/me/people", s.authed(s.handlePeople))

	mux.Handle("GET /api/import/spotify", s.authed(s.handleImportSpotify))
	mux.Handle("POST /api/import/match", s.authed(s.handleImportMatch))

	mux.Handle("GET /api/discover/artists/{id}", s.authed(s.handleDiscoverArtist))
	mux.Handle("GET /api/discover/artists/{id}/albums", s.authed(s.handleDiscoverArtistAlbums))
	mux.Handle("GET /api/discover/artists/{id}/related", s.authed(s.handleDiscoverRelated))
	mux.Handle("GET /api/discover/artists/{id}/radio", s.authed(s.handleDiscoverRadio))
	mux.Handle("GET /api/discover/artists/{id}/info", s.authed(s.handleArtistInfo))
	mux.Handle("GET /api/discover/artists/{id}/bio", s.authed(s.handleArtistBio))
	mux.Handle("PUT /api/discover/artists/{id}/bio", s.authed(s.handlePutArtistBio))
	mux.Handle("DELETE /api/discover/artists/{id}/bio", s.authed(s.handleDeleteArtistBio))
	mux.Handle("PUT /api/discover/artists/{id}/tags", s.authed(s.handlePutArtistTags))
	mux.Handle("DELETE /api/discover/artists/{id}/tags", s.authed(s.handleDeleteArtistTags))
	mux.Handle("GET /api/discover/albums/{id}", s.authed(s.handleDiscoverAlbum))
	mux.Handle("GET /api/discover/tracks/{id}", s.authed(s.handleDiscoverTrack))

	// Свои релизы (uploads.go, uploads_audio.go): черновик → треки и поля →
	// публикация. Правит автор релиза и админ.
	mux.Handle("GET /api/uploads", s.authed(s.handleUploads))
	mux.Handle("POST /api/uploads/releases", s.authed(s.handleCreateRelease))
	mux.Handle("GET /api/uploads/releases/{id}", s.authed(s.handleGetRelease))
	mux.Handle("PUT /api/uploads/releases/{id}", s.authed(s.handleSaveRelease))
	mux.Handle("DELETE /api/uploads/releases/{id}", s.authed(s.handleDeleteRelease))
	mux.Handle("PUT /api/uploads/releases/{id}/cover", s.authed(s.handleReleaseCover))
	mux.Handle("POST /api/uploads/releases/{id}/audio", s.authed(s.handleAddReleaseAudio))
	mux.Handle("PUT /api/uploads/tracks/{id}/audio", s.authed(s.handleReplaceTrackAudio))
	mux.Handle("DELETE /api/uploads/tracks/{id}", s.authed(s.handleDeleteOwnTrack))
	mux.Handle("GET /api/uploads/tracks/{id}/lyrics", s.authed(s.handleGetOwnLyrics))
	mux.Handle("PUT /api/uploads/tracks/{id}/lyrics", s.authed(s.handlePutOwnLyrics))
	mux.Handle("POST /api/uploads/artists", s.authed(s.handleCreateArtist))

	// Картинки Deezer через наш домен (images.go); без Bearer, как и /media.
	mux.HandleFunc("GET /api/img/{kind}/{hash}/{file}", s.handleDeezerImage)
	// Скачанные обложки нужного размера (covers.go); без Bearer, как /media/covers/.
	mux.HandleFunc("GET /api/cover/{px}/{key...}", s.handleSizedCover)

	return limitBodies(mux)
}

// Тела запросов к API — небольшой JSON; без предела nginx пропускал бы до
// 1 МБ на любой эндпоинт, в том числе без авторизации. Загрузки картинок и
// аудио ограничивают себя сами (handlers_edit.go, 1 МБ; handlers_admin.go,
// uploads_audio.go).
const maxJSONBody = 64 << 10

func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upload := r.Method == http.MethodPut &&
			(strings.HasSuffix(r.URL.Path, "/picture") || strings.HasSuffix(r.URL.Path, "/cover")) ||
			(r.Method == http.MethodPut || r.Method == http.MethodPost) && strings.HasSuffix(r.URL.Path, "/audio")
		if r.Body != nil && !upload {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		}
		next.ServeHTTP(w, r)
	})
}

// Version — версия сборки: тег релиза или коммит. Подставляется при сборке
// (-ldflags "-X kaikku/internal/api.Version=…", Dockerfile), иначе dev.
var Version = "dev"

// handleHealth is unauthenticated: Docker's healthcheck, and the version for
// the settings page.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
}

// authed wraps a handler, requiring a valid "Authorization: Bearer <jwt>"
// header of a user that still exists — the token alone would keep a deleted
// user in for up to 30 days — and issued after their last password change
// (handlers_password.go). Whether the user is an admin comes from the same
// query, so revoking admin takes effect immediately too.
func (s *Server) authed(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := auth.VerifyToken(token, s.JWTSecret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		var isAdmin bool
		var changed sql.NullTime
		err = s.DB.QueryRowContext(r.Context(), `SELECT is_admin, password_changed_at FROM users WHERE id = $1`, claims.UserID).Scan(&isAdmin, &changed)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "user no longer exists")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		// Токены — с точностью до секунды: выданный в ту же секунду, что и
		// смена, тоже старый (новый выдаётся со следующей, writeFreshToken).
		if changed.Valid && claims.Iat <= changed.Time.Unix() {
			writeError(w, http.StatusUnauthorized, "password was changed, log in again")
			return
		}
		// Скользящий вход: токену больше суток — ответ несёт новый, клиент
		// его сохраняет (api.js). Не вышло выдать — не страшно, старый ещё
		// действует.
		if time.Since(time.Unix(claims.Iat, 0)) > tokenRenewAfter {
			if fresh, err := auth.IssueToken(claims.UserID, tokenTTL, s.JWTSecret); err == nil {
				w.Header().Set("X-Auth-Token", fresh)
			}
		}
		ctx := context.WithValue(r.Context(), userIDCtxKey, claims.UserID)
		ctx = context.WithValue(ctx, isAdminCtxKey, isAdmin)
		next(w, r.WithContext(ctx))
	})
}

// currentUser is the id of the user that authed let through.
func currentUser(r *http.Request) string {
	id, _ := r.Context().Value(userIDCtxKey).(string)
	return id
}

// adminOnly is authed plus users.is_admin.
func (s *Server) adminOnly(next http.HandlerFunc) http.Handler {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		if isAdmin, _ := r.Context().Value(isAdminCtxKey).(bool); !isAdmin {
			writeError(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r)
	})
}
