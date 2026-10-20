package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type profileUsers struct {
	usecase.UserService
	account   usecase.Account
	saveError error
}

func (u *profileUsers) UpsertTelegramUser(context.Context, usecase.TelegramUserParams) (usecase.Account, error) {
	return u.account, nil
}
func (u *profileUsers) UpsertMAXUser(context.Context, usecase.MAXUserParams) (usecase.Account, error) {
	return u.account, nil
}
func (u *profileUsers) SaveProfile(_ context.Context, id int64, first, last string) (usecase.Account, error) {
	if id != u.account.User.ID {
		return usecase.Account{}, errors.New("wrong internal ID")
	}
	if u.saveError != nil {
		return usecase.Account{}, u.saveError
	}
	var err error
	first, err = usecase.CleanProfileName(first)
	if err != nil {
		return usecase.Account{}, err
	}
	last, err = usecase.CleanProfileName(last)
	if err != nil {
		return usecase.Account{}, err
	}
	u.account.User.FirstName = first
	u.account.User.LastName.SetTo(last)
	u.account.ProfileComplete = true
	u.account.ProfileStep = ""
	return u.account, nil
}

func TestProfileHTTP(t *testing.T) {
	users := &profileUsers{account: usecase.Account{User: contracts.User{ID: 900}, TgID: 42, ProfileStep: "last_name"}}
	raw := generateValidInitData(map[string]string{"auth_date": fmt.Sprint(time.Now().Unix()), "user": `{"id":42}`}, "secret")

	handler := NewAPIHandler(nil, nil, nil, users)
	router, err := NewRouter(handler, fstest.MapFS{}, "", "secret", users)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, auth string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", auth)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/profile", "", "tma "+raw); w.Code != 200 || !strings.Contains(w.Body.String(), `"profileComplete":false`) || !strings.Contains(w.Body.String(), `"id":900`) {
		t.Fatalf("read incomplete: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/", "", "tma "+raw); w.Code != 403 || !strings.Contains(w.Body.String(), "profile_required") {
		t.Fatalf("gate: %d %s", w.Code, w.Body.String())
	}
	if w := request("PUT", "/api/profile", `{"firstName":"  ","lastName":"Last"}`, "tma "+raw); w.Code != 400 {
		t.Fatalf("validation: %d %s", w.Code, w.Body.String())
	}
	users.saveError = errors.New("database unavailable")
	if w := request("PUT", "/api/profile", `{"firstName":"First","lastName":"Last"}`, "tma "+raw); w.Code != 500 {
		t.Fatalf("database: %d %s", w.Code, w.Body.String())
	}
	users.saveError = nil
	if w := request("PUT", "/api/profile", `{"firstName":" First ","lastName":" Last "}`, "tma "+raw); w.Code != 200 || !strings.Contains(w.Body.String(), `"firstName":"First"`) {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	// The learning gate is removed; the unconfigured learning handler returns 501.
	if w := request("GET", "/api/", "", "tma "+raw); w.Code == 403 {
		t.Fatalf("gate remains: %s", w.Body.String())
	}
	if w := request("PUT", "/api/profile", `{"firstName":"First","lastName":"Last"}`, ""); w.Code != 403 {
		t.Fatalf("browser can save: %d", w.Code)
	}
	if w := request("GET", "/api/profile", "", ""); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"platform"`) {
		t.Fatalf("browser profile: %d %s", w.Code, w.Body.String())
	}
	users.account.TgID = 0
	users.account.MaxID = 42
	maxRouter, err := NewRouter(handler, fstest.MapFS{}, "secret", "", users)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", "/api/profile", strings.NewReader(`{"firstName":"First","lastName":"Last"}`))
	r.Header.Set("Authorization", "max "+raw)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	maxRouter.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("MAX can save Telegram profile: %d", w.Code)
	}
}
