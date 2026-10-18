package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims is intentionally minimal — just enough for auth.
type Claims struct {
	UserID string `json:"sub"`
	Exp    int64  `json:"exp"`
	Iat    int64  `json:"iat"` // когда выдан: токены до смены пароля не действуют
}

var ErrInvalidToken = errors.New("invalid or expired token")

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// IssueToken creates a signed HS256 JWT. Hand-rolled on stdlib to keep
// lib/pq the only dependency; the header is never parsed on verify, so
// alg-confusion attacks don't apply.
func IssueToken(userID string, ttl time.Duration, secret []byte) (string, error) {
	return IssueTokenAt(userID, time.Now(), ttl, secret)
}

// IssueTokenAt — то же, выданный в момент now (тесты продления входа).
func IssueTokenAt(userID string, now time.Time, ttl time.Duration, secret []byte) (string, error) {
	header, err := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(Claims{UserID: userID, Exp: now.Add(ttl).Unix(), Iat: now.Unix()})
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(claims)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

// VerifyToken checks the signature and expiry and returns the claims.
func VerifyToken(token string, secret []byte) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	signingInput := parts[0] + "." + parts[1]

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(expectedSig), []byte(parts[2])) != 1 {
		return nil, ErrInvalidToken
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().Unix() > claims.Exp {
		return nil, ErrInvalidToken
	}
	return &claims, nil
}
