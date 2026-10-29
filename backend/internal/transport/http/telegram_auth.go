package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// validateTelegramInitData verifies Telegram HMAC initData with a one-hour TTL.
func validateTelegramInitData(raw, token string, now time.Time) (*initDataUser, error) {
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	for _, values := range vals {
		if len(values) != 1 {
			return nil, errors.New("duplicate initData parameter")
		}
	}
	date, err := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if err != nil {
		return nil, errInvalidAuthDate
	}
	age := now.Unix() - date
	if age > 3600 || age < -300 {
		return nil, errExpiredAuthDate
	}
	keys := make([]string, 0, len(vals))
	for key := range vals {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+vals.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	hash, err := hex.DecodeString(vals.Get("hash"))
	if err != nil || !hmac.Equal(hash, mac.Sum(nil)) {
		return nil, errHashMismatch
	}
	var user initDataUser
	if err = json.Unmarshal([]byte(vals.Get("user")), &user); err != nil {
		return nil, err
	}
	if user.ID <= 0 {
		return nil, errInvalidUserID
	}
	return &user, nil
}
