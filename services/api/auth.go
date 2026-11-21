package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const cookieName = "aegis_session"
const sessionTTL = 8 * time.Hour

// Session is what we remember about a logged-in user. It is signed, so it cannot be edited.
type Session struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Role     string `json:"role"`
	Subject  string `json:"sub"`
	Expires  int64  `json:"exp"`
}

func signSession(secret []byte, s Session) (string, error) {
	body, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func verifySession(secret []byte, token string, now time.Time) (Session, error) {
	var s Session
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return s, errors.New("bad token")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0]))
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return s, errors.New("bad signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return s, errors.New("bad payload")
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return s, errors.New("bad payload")
	}
	if now.Unix() >= s.Expires {
		return s, errors.New("expired")
	}
	return s, nil
}
