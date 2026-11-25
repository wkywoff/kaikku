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
	Secret string
	TTL    time.Duration
}

func (s *Signer) URL(storageKey string) (signedURL string, expires int64) {
	expires = time.Now().Add(s.TTL).Unix()
	uri := "/media/" + storageKey
	sig := sign(expires, uri, s.Secret)
	return fmt.Sprintf("%s?st=%s&e=%d", uri, sig, expires), expires
}

func (s *Signer) PathURL(uri string) (signedURL string, expires int64) {
	expires = time.Now().Add(s.TTL).Unix()
	return fmt.Sprintf("%s?st=%s&e=%d", uri, sign(expires, uri, s.Secret), expires), expires
}

var (
	ErrBadSignature = errors.New("bad signature")
	ErrExpired      = errors.New("link expired")
)

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
