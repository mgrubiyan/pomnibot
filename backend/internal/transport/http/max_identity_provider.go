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

var (
	errMissingInitDataHeader = errors.New("missing Authorization header")
	errMissingHash           = errors.New("missing hash in initData")
	errMissingAuthDate       = errors.New("missing auth_date in initData")
	errInvalidAuthDate       = errors.New("invalid auth_date in initData")
	errExpiredAuthDate       = errors.New("expired auth_date in initData")
	errHashMismatch          = errors.New("hash mismatch in initData")
	errMissingUserField      = errors.New("missing user in initData")
	errInvalidUserID         = errors.New("invalid or zero user ID")
)

const maxInitDataAge = time.Hour

type maxUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	IsBot     bool   `json:"is_bot"`
}

func resolveUserName(u maxUser) string {
	if strings.TrimSpace(u.Name) != "" {
		return strings.TrimSpace(u.Name)
	}
	fullName := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if fullName != "" {
		return fullName
	}
	if strings.TrimSpace(u.FirstName) != "" {
		return strings.TrimSpace(u.FirstName)
	}
	if strings.TrimSpace(u.Username) != "" {
		return strings.TrimSpace(u.Username)
	}
	return "MAX User"
}

func validateInitData(rawInitData, botToken string, now time.Time) (*maxUser, error) {
	raw := strings.TrimSpace(rawInitData)
	raw = strings.TrimPrefix(raw, "#")
	if strings.HasPrefix(raw, "WebAppData=") {
		raw = strings.TrimPrefix(raw, "WebAppData=")
		if unescaped, err := url.QueryUnescape(raw); err == nil {
			raw = unescaped
		}
	}

	vals, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}

	hash := vals.Get("hash")
	if hash == "" {
		return nil, errMissingHash
	}

	authDateStr := vals.Get("auth_date")
	if authDateStr == "" {
		return nil, errMissingAuthDate
	}

	authTimestamp, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return nil, errInvalidAuthDate
	}

	age := now.Unix() - authTimestamp
	if age > int64(maxInitDataAge.Seconds()) {
		return nil, errExpiredAuthDate
	}
	if age < -300 {
		return nil, errInvalidAuthDate
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+vals.Get(k))
	}
	launchParams := strings.Join(lines, "\n")

	h := hmac.New(sha256.New, []byte("WebAppData"))
	h.Write([]byte(botToken))
	secretKey := h.Sum(nil)

	h2 := hmac.New(sha256.New, secretKey)
	h2.Write([]byte(launchParams))
	calcHashBytes := h2.Sum(nil)

	givenHashBytes, err := hex.DecodeString(hash)
	if err != nil || !hmac.Equal(givenHashBytes, calcHashBytes) {
		return nil, errHashMismatch
	}

	userRaw := vals.Get("user")
	if userRaw == "" {
		return nil, errMissingUserField
	}

	var u maxUser
	if unmarshalErr := json.Unmarshal([]byte(userRaw), &u); unmarshalErr != nil {
		if unescaped, unescapeErr := url.QueryUnescape(userRaw); unescapeErr == nil {
			if unmarshalErr = json.Unmarshal([]byte(unescaped), &u); unmarshalErr != nil {
				return nil, unmarshalErr
			}
		} else {
			return nil, unmarshalErr
		}
	}

	if u.ID <= 0 {
		return nil, errInvalidUserID
	}

	return &u, nil
}

// IdentityProvider verifies raw Mini App data without accessing persistence.
type IdentityProvider interface {
	Verify(context.Context, string) (usecase.Identity, error)
}

type maxIdentityProvider struct{ token string }

// NewMAXIdentityProvider uses the existing MAX signature and time rules.
func NewMAXIdentityProvider(token string) IdentityProvider { return maxIdentityProvider{token: token} }

func (p maxIdentityProvider) Verify(_ context.Context, raw string) (usecase.Identity, error) {
	if p.token == "" {
		return usecase.Identity{}, errMissingInitDataHeader
	}
	u, err := validateInitData(raw, p.token, time.Now())
	if err != nil {
		return usecase.Identity{}, err
	}
	first := strings.TrimSpace(u.FirstName)
	if first == "" {
		first = resolveUserName(*u)
	}
	return usecase.Identity{Platform: "max", ExternalID: u.ID, FirstName: first, LastName: strings.TrimSpace(u.LastName), Username: strings.TrimSpace(u.Username), IsBot: u.IsBot}, nil
}
