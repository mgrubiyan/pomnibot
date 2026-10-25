package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	pngenc "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type profileUsers struct {
	usecase.UserService
	account   usecase.Account
	saveError error
	photo     []byte
}

func (u *profileUsers) UpsertTelegramUser(context.Context, usecase.TelegramUserParams) (usecase.Account, error) {
	return u.account, nil
}
func (u *profileUsers) UpsertMAXUser(context.Context, usecase.MAXUserParams) (usecase.Account, error) {
	return u.account, nil
}
func (u *profileUsers) SaveProfile(_ context.Context, id uuid.UUID, first, last string) (usecase.Account, error) {
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
	testUserUUID := uuid.MustParse("00000000-0000-0000-0000-000000000900")
	users := &profileUsers{account: usecase.Account{User: contracts.User{ID: testUserUUID}, TgID: 42, ProfileStep: "last_name"}}
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
	if w := request("GET", "/api/profile", "", "tma "+raw); w.Code != 200 || !strings.Contains(w.Body.String(), `"profileComplete":false`) || !strings.Contains(w.Body.String(), fmt.Sprintf(`"id":"%s"`, testUserUUID)) {
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

func (u *profileUsers) SetDataConsent(_ context.Context, id uuid.UUID, given bool) (usecase.Account, error) {
	if id != u.account.User.ID {
		return usecase.Account{}, errors.New("wrong internal ID")
	}
	if u.saveError != nil {
		return usecase.Account{}, u.saveError
	}
	u.account.DataConsent = given
	u.account.DataConsentAnswered = true
	return u.account, nil
}

func (u *profileUsers) EnsureUser(context.Context, uuid.UUID) error { return nil }

func (u *profileUsers) GetAccount(_ context.Context, id uuid.UUID) (usecase.Account, error) {
	if id != u.account.User.ID {
		return usecase.Account{}, errors.New("wrong internal ID")
	}
	return u.account, nil
}

func TestProfileConsentHTTP(t *testing.T) {
	member := uuid.MustParse("00000000-0000-0000-0000-000000000902")
	users := &profileUsers{account: usecase.Account{User: contracts.User{ID: member, FirstName: "Ann"}, MaxID: 42, ProfileComplete: true}}
	raw := generateValidInitData(map[string]string{"auth_date": fmt.Sprint(time.Now().Unix()), "user": `{"id":42}`}, "secret")
	request := func(handler *APIHandler, method, path, body, auth string) *httptest.ResponseRecorder {
		t.Helper()
		router, err := NewRouter(handler, fstest.MapFS{}, "secret", "", users)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", auth)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	handler := NewAPIHandler(nil, nil, nil, users)
	if w := request(handler, "GET", "/api/profile", "", "max "+raw); w.Code != 200 || !strings.Contains(w.Body.String(), `"dataConsent":false`) || !strings.Contains(w.Body.String(), `"dataConsentAnswered":false`) {
		t.Fatalf("read before consent: %d %s", w.Code, w.Body.String())
	}
	if w := request(handler, "PUT", "/api/profile/consent", `{"given":false}`, "max "+raw); w.Code != 200 || !strings.Contains(w.Body.String(), `"dataConsent":false`) || !strings.Contains(w.Body.String(), `"dataConsentAnswered":true`) {
		t.Fatalf("decline: %d %s", w.Code, w.Body.String())
	}
	if w := request(handler, "PUT", "/api/profile/consent", `{"given":true}`, "max "+raw); w.Code != 200 || !strings.Contains(w.Body.String(), `"dataConsent":true`) || !users.account.DataConsent {
		t.Fatalf("give: %d %s", w.Code, w.Body.String())
	}
	if w := request(handler, "PUT", "/api/profile/consent", `{"given":false}`, "max "+raw); w.Code != 200 || !strings.Contains(w.Body.String(), `"dataConsent":false`) || users.account.DataConsent {
		t.Fatalf("withdraw: %d %s", w.Code, w.Body.String())
	}
	users.saveError = errors.New("database unavailable")
	if w := request(handler, "PUT", "/api/profile/consent", `{"given":true}`, "max "+raw); w.Code != 500 {
		t.Fatalf("database: %d %s", w.Code, w.Body.String())
	}
	users.saveError = nil

	// Without a signature the request is the shared browser account, which
	// anyone on the internet may use in production.
	if w := request(handler, "PUT", "/api/profile/consent", `{"given":true}`, ""); w.Code != 403 || users.account.DataConsent {
		t.Fatalf("browser can consent in production: %d %s", w.Code, w.Body.String())
	}
	// Nobody can answer for it, so it is never asked.
	if w := request(handler, "GET", "/api/profile", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"dataConsentAnswered":true`) {
		t.Fatalf("browser is asked in production: %d %s", w.Code, w.Body.String())
	}

	// Locally the browser account stands for the developer.
	users.account = usecase.Account{User: contracts.User{ID: uuid.Nil, FirstName: "Browser Test User"}, ProfileComplete: true}
	local := NewAPIHandler(nil, nil, nil, users).WithBrowserProfile()
	if w := request(local, "PUT", "/api/profile/consent", `{"given":true}`, ""); w.Code != 200 || !users.account.DataConsent {
		t.Fatalf("local browser consent: %d %s", w.Code, w.Body.String())
	}
	if w := request(local, "GET", "/api/profile", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"dataConsent":true`) {
		t.Fatalf("local browser profile: %d %s", w.Code, w.Body.String())
	}
}

func (u *profileUsers) SaveAvatar(_ context.Context, id uuid.UUID, data []byte) (usecase.Account, error) {
	if id != u.account.User.ID {
		return usecase.Account{}, errors.New("wrong internal ID")
	}
	photo, err := usecase.CleanAvatar(data)
	if err != nil {
		return usecase.Account{}, err
	}
	u.photo = photo
	u.account.User.AvatarUrl.SetTo("/api/avatars/" + testPhotoID.String())
	return u.account, nil
}

func (u *profileUsers) DeleteAvatar(_ context.Context, id uuid.UUID) (usecase.Account, error) {
	if id != u.account.User.ID {
		return usecase.Account{}, errors.New("wrong internal ID")
	}
	u.photo = nil
	u.account.User.AvatarUrl.Reset()
	return u.account, nil
}

func (u *profileUsers) AvatarImage(_ context.Context, id uuid.UUID) ([]byte, error) {
	if id != testPhotoID || u.photo == nil {
		return nil, usecase.ErrNotFound
	}
	return u.photo, nil
}

var testPhotoID = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")

func TestProfileAvatarHTTP(t *testing.T) {
	member := uuid.MustParse("00000000-0000-0000-0000-000000000903")
	users := &profileUsers{account: usecase.Account{User: contracts.User{ID: member, FirstName: "Ann"}, MaxID: 42, ProfileComplete: true}}
	raw := generateValidInitData(map[string]string{"auth_date": fmt.Sprint(time.Now().Unix()), "user": `{"id":42}`}, "secret")
	router, err := NewRouter(NewAPIHandler(nil, nil, nil, users), fstest.MapFS{}, "secret", "", users)
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
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var png bytes.Buffer
	if err := pngenc.Encode(&png, img); err != nil {
		t.Fatal(err)
	}
	body := `{"image":"` + base64.StdEncoding.EncodeToString(png.Bytes()) + `"}`

	if w := request("PUT", "/api/profile/avatar", body, ""); w.Code != 403 || users.photo != nil {
		t.Fatalf("browser can upload in production: %d %s", w.Code, w.Body.String())
	}
	if w := request("PUT", "/api/profile/avatar", `{"image":"aGVsbG8="}`, "max "+raw); w.Code != 400 {
		t.Fatalf("not an image: %d %s", w.Code, w.Body.String())
	}
	// A body far over the limit is refused; TestLimitAvatarBody shows it is not read in full.
	huge := `{"image":"` + strings.Repeat("A", 2<<20) + `"}`
	if w := request("PUT", "/api/profile/avatar", huge, "max "+raw); w.Code < 400 || w.Code >= 500 {
		t.Fatalf("huge body: %d %s", w.Code, w.Body.String())
	}
	w := request("PUT", "/api/profile/avatar", body, "max "+raw)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"avatarUrl":"/api/avatars/`+testPhotoID.String()+`"`) {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}

	// An image tag sends no authorization.
	w = request("GET", "/api/avatars/"+testPhotoID.String(), "", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("photo: %d %v", w.Code, w.Header())
	}
	if _, format, err := image.DecodeConfig(w.Body); err != nil || format != "jpeg" {
		t.Fatalf("served photo: %s %v", format, err)
	}
	if w := request("GET", "/api/avatars/"+uuid.NewString(), "", ""); w.Code != 404 {
		t.Fatalf("unknown photo: %d", w.Code)
	}

	if w := request("DELETE", "/api/profile/avatar", "", ""); w.Code != 403 || users.photo == nil {
		t.Fatalf("browser can delete in production: %d", w.Code)
	}
	if w := request("DELETE", "/api/profile/avatar", "", "max "+raw); w.Code != 200 || strings.Contains(w.Body.String(), "avatarUrl") {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/avatars/"+testPhotoID.String(), "", ""); w.Code != 404 {
		t.Fatalf("deleted photo: %d", w.Code)
	}
}

func TestLimitAvatarBody(t *testing.T) {
	var read int64
	var readErr error
	handler := limitAvatarBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		read, readErr = io.Copy(io.Discard, r.Body)
	}))
	body := strings.Repeat("A", 2*maxAvatarRequest)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PUT", "/api/profile/avatar", strings.NewReader(body)))
	if readErr == nil || read > maxAvatarRequest {
		t.Fatalf("photo upload read %d bytes, error %v", read, readErr)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/sets", strings.NewReader(body)))
	if readErr != nil || read != int64(len(body)) {
		t.Fatalf("other request read %d bytes, error %v", read, readErr)
	}
}

func TestProfileWritesWithoutUserService(t *testing.T) {
	account := usecase.Account{User: contracts.User{ID: uuid.New()}, MaxID: 42, ProfileComplete: true}
	ctx := context.WithValue(context.Background(), accountContextKey, account)
	handler := NewAPIHandler(nil, nil, nil, nil)
	if res, _ := handler.PutProfileConsent(ctx, &contracts.ConsentRequest{Given: true}); res == nil {
		t.Fatal("consent: no response")
	} else if _, ok := res.(*contracts.PutProfileConsentInternalServerError); !ok {
		t.Fatalf("consent: %T", res)
	}
	if res, _ := handler.PutProfileAvatar(ctx, &contracts.AvatarRequest{}); res == nil {
		t.Fatal("avatar: no response")
	} else if _, ok := res.(*contracts.PutProfileAvatarInternalServerError); !ok {
		t.Fatalf("avatar: %T", res)
	}
	if res, _ := handler.DeleteProfileAvatar(ctx); res == nil {
		t.Fatal("delete: no response")
	} else if _, ok := res.(*contracts.DeleteProfileAvatarInternalServerError); !ok {
		t.Fatalf("delete: %T", res)
	}
}
