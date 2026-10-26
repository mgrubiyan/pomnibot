package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type recordingImporter struct {
	mu        sync.Mutex
	imported  []string
	data      [][]byte
	forgotten int
}

func (r *recordingImporter) ImportAvatar(_ context.Context, _ uuid.UUID, source string, data []byte) (usecase.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.imported = append(r.imported, source)
	r.data = append(r.data, data)
	return usecase.Account{}, nil
}

func (r *recordingImporter) ForgetImportedAvatar(context.Context, uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forgotten++
	return nil
}

func photoServer(t *testing.T, fetches *int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*fetches++
		mu.Unlock()
		switch r.URL.Path {
		case "/photo.jpg":
			_, _ = w.Write([]byte("photo bytes"))
		case "/away":
			http.Redirect(w, r, "http://example.com/photo.jpg", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPhotoImporter(t *testing.T) {
	account := usecase.Account{User: contracts.User{ID: uuid.New()}}

	t.Run("fetches a new photo once and keeps it", func(t *testing.T) {
		fetches := 0
		server := photoServer(t, &fetches)
		users := &recordingImporter{}
		photos := NewPhotoImporter(users, server.Client())
		for range 3 {
			photos.Sync(context.Background(), account, server.URL+"/photo.jpg")
		}
		photos.Wait()
		if fetches != 1 || len(users.imported) != 1 || users.imported[0] != server.URL+"/photo.jpg" || string(users.data[0]) != "photo bytes" {
			t.Fatalf("fetches %d, imported %v", fetches, users.imported)
		}
	})

	t.Run("does nothing for the photo the avatar came from", func(t *testing.T) {
		fetches := 0
		server := photoServer(t, &fetches)
		users := &recordingImporter{}
		photos := NewPhotoImporter(users, server.Client())
		saved := account
		saved.AvatarSource = server.URL + "/photo.jpg"
		photos.Sync(context.Background(), saved, saved.AvatarSource)
		photos.Wait()
		if fetches != 0 || len(users.imported) != 0 {
			t.Fatalf("fetches %d, imported %v", fetches, users.imported)
		}
	})

	t.Run("does not leave https and does not retry a failed photo", func(t *testing.T) {
		fetches := 0
		server := photoServer(t, &fetches)
		users := &recordingImporter{}
		photos := NewPhotoImporter(users, server.Client())
		for _, photo := range []string{server.URL + "/away", server.URL + "/away", "http://example.com/photo.jpg"} {
			photos.Sync(context.Background(), account, photo)
			photos.Wait()
		}
		photos.Wait()
		if len(users.imported) != 0 || fetches != 1 {
			t.Fatalf("fetches %d, imported %v", fetches, users.imported)
		}
	})

	t.Run("forgets a photo taken before once the messenger has none", func(t *testing.T) {
		users := &recordingImporter{}
		photos := NewPhotoImporter(users, nil)
		photos.Sync(context.Background(), usecase.Account{User: account.User}, "")
		imported := account
		imported.AvatarSource = "https://i.max.ru/old.jpg"
		photos.Sync(context.Background(), imported, "")
		photos.Wait()
		if users.forgotten != 1 {
			t.Fatalf("forgotten %d", users.forgotten)
		}
	})

	t.Run("a nil importer does nothing", func(_ *testing.T) {
		var photos *PhotoImporter
		photos.Sync(context.Background(), account, "https://i.max.ru/photo.jpg")
	})
}

func TestAuthMiddlewareTakesThePhotoFromTheLaunchData(t *testing.T) {
	fetches := 0
	server := photoServer(t, &fetches)
	users := &mockUserService{maxFunc: func(context.Context, usecase.MAXUserParams) (usecase.Account, error) {
		return usecase.Account{User: contracts.User{ID: testAuthMaxID, FirstName: "Saved"}, MaxID: 42, ProfileComplete: true}, nil
	}}
	importer := &recordingImporter{}
	photos := NewPhotoImporter(importer, server.Client())
	raw := generateValidInitData(map[string]string{
		"auth_date": fmt.Sprint(time.Now().Unix()),
		"user":      fmt.Sprintf(`{"id":42,"first_name":"Remote","photo_url":%q}`, server.URL+"/photo.jpg"),
	}, "secret")
	request := httptest.NewRequest("GET", "/api/sets", nil)
	request.Header.Set("Authorization", "max "+raw)
	response := httptest.NewRecorder()
	AuthMiddleware("secret", "", users, photos)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, request)
	photos.Wait()
	if response.Code != 200 || len(importer.imported) != 1 || importer.imported[0] != server.URL+"/photo.jpg" {
		t.Fatalf("%d, imported %v", response.Code, importer.imported)
	}
}
