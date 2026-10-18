package media

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type Signer struct {
	Secret string // must match the literal string in nginx's secure_link_md5 directive
	TTL    time.Duration
}

// URL returns a time-limited link for storageKey and the Unix expiry it
// encodes. Relative, like PathURL: the site and apps resolve it against the
// address they came from, so a track plays whether the site was opened at
// its domain or at a LAN address. The signature format matches nginx's ngx_http_secure_link_module
// exactly (see docker/nginx.conf.template):
//
//	secure_link_md5 "$secure_link_expires$uri secret";
//
// i.e. md5("<expires><uri> <secret>"), base64 with the URL-safe alphabet and
// no padding — precisely what nginx's docs describe as
// `openssl md5 -binary | openssl base64 | tr +/ -_ | tr -d =`.
func (s *Signer) URL(storageKey string) (signedURL string, expires int64) {
	expires = time.Now().Add(s.TTL).Unix()
	uri := "/media/" + storageKey
	sig := sign(expires, uri, s.Secret)
	return fmt.Sprintf("%s?st=%s&e=%d", uri, sig, expires), expires
}

// PathURL signs a path the API serves itself (not nginx), e.g. the
// on-the-fly stream of a track that isn't in the library yet: <audio> can't
// send the Bearer token, so it gets the same kind of signed link as /media.
func (s *Signer) PathURL(uri string) (signedURL string, expires int64) {
	expires = time.Now().Add(s.TTL).Unix()
	return fmt.Sprintf("%s?st=%s&e=%d", uri, sign(expires, uri, s.Secret), expires), expires
}

var (
	ErrBadSignature = errors.New("bad signature")
	ErrExpired      = errors.New("link expired")
)

// Verify checks st/e of a link made by PathURL — the same checks as nginx's
// secure_link: ErrBadSignature (nginx: 403) or ErrExpired (410).
func (s *Signer) Verify(uri, st, e string) error {
	expires, err := strconv.ParseInt(e, 10, 64)
	if err != nil || st == "" {
		return ErrBadSignature
	}
	if subtle.ConstantTimeCompare([]byte(st), []byte(sign(expires, uri, s.Secret))) != 1 {
		return ErrBadSignature
	}
	if time.Now().Unix() > expires {
		return ErrExpired
	}
	return nil
}

func sign(expires int64, uri, secret string) string {
	raw := fmt.Sprintf("%d%s %s", expires, uri, secret)
	sum := md5.Sum([]byte(raw))
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(sum[:])
}
