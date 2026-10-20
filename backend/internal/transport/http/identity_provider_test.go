package http

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestIdentityProviders(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, platform := range []string{"max", "telegram"} {
		t.Run(platform, func(t *testing.T) {
			p := NewMAXIdentityProvider("secret")
			if platform == "telegram" {
				p = NewTelegramIdentityProvider("secret")
			}
			for _, tc := range []struct {
				age   time.Duration
				user  string
				valid bool
			}{
				{0, `{"id":42,"first_name":"Remote"}`, true},
				{time.Hour + time.Second, `{"id":42}`, false},
				{-6 * time.Minute, `{"id":42}`, false},
				{-4 * time.Minute, `{"id":42}`, true},
				{0, `{"id":0}`, false}, {0, `{"id":-1}`, false}, {0, `{"id":"42"}`, false},
			} {
				raw := generateValidInitData(map[string]string{"auth_date": strconv.FormatInt(now.Add(-tc.age).Unix(), 10), "user": tc.user, "query_id": "a + b"}, "secret")
				got, err := p.Verify(context.Background(), raw)
				if (err == nil) != tc.valid {
					t.Fatalf("%v %s: %v", tc.age, tc.user, err)
				}
				if err == nil && (got.ExternalID != 42 || got.Platform != platform) {
					t.Fatalf("identity: %+v", got)
				}
				if _, err = p.Verify(context.Background(), raw+"tamper"); err == nil {
					t.Fatal("tampered signature accepted")
				}
			}
		})
	}
}
