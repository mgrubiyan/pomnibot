package http

import (
	"context"
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

	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type telegramIdentityProvider struct{ token string }

// NewTelegramIdentityProvider verifies Telegram HMAC initData with a one-hour TTL.
// See https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app.
func NewTelegramIdentityProvider(token string) IdentityProvider {
	return telegramIdentityProvider{token: token}
}

func (p telegramIdentityProvider) Verify(_ context.Context, raw string) (usecase.Identity, error) {
	if p.token == "" {
		return usecase.Identity{}, errMissingInitDataHeader
	}
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return usecase.Identity{}, err
	}
	for _, values := range vals {
		if len(values) != 1 {
			return usecase.Identity{}, errors.New("duplicate initData parameter")
		}
	}
	date, err := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if err != nil {
		return usecase.Identity{}, errInvalidAuthDate
	}
	age := time.Now().Unix() - date
	if age > 3600 || age < -300 {
		return usecase.Identity{}, errExpiredAuthDate
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
	secret.Write([]byte(p.token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	hash, err := hex.DecodeString(vals.Get("hash"))
	if err != nil || !hmac.Equal(hash, mac.Sum(nil)) {
		return usecase.Identity{}, errHashMismatch
	}
	var user struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
		IsBot     bool   `json:"is_bot"`
	}
	if err = json.Unmarshal([]byte(vals.Get("user")), &user); err != nil {
		return usecase.Identity{}, err
	}
	if user.ID <= 0 {
		return usecase.Identity{}, errInvalidUserID
	}
	return usecase.Identity{Platform: "telegram", ExternalID: user.ID, FirstName: user.FirstName, LastName: user.LastName, Username: user.Username, IsBot: user.IsBot}, nil
}
