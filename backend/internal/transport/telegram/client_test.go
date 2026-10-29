package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientDownloadLimitAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int64
		status int
		want   string
	}{
		{"ok", 4, 200, ""}, {"metadata too large", MaxFileSize + 1, 200, "large"}, {"download failure", 4, 502, "download"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "getFile") {
					_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"file_path":"docs/a.txt","file_size":%d}}`, tc.size)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("text"))
			}))
			defer server.Close()
			c := NewClient("SECRET")
			c.baseURL = server.URL
			data, err := c.Download(context.Background(), "id")
			if tc.want == "" {
				if err != nil || string(data) != "text" {
					t.Fatalf("%s %v", data, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestRetryAfterAndCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`))
	}))
	defer server.Close()
	c := NewClient("secret")
	c.baseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Send(ctx, 1, 0, "hello", ""); err == nil {
		t.Fatal("expected cancellation")
	}
	if calls != 1 {
		t.Fatalf("retried early: %d", calls)
	}
}

func TestDownloadEnforcesActualByteLimit(t *testing.T) {
	for _, knownLength := range []bool{false, true} {
		t.Run(fmt.Sprint(knownLength), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "getFile") {
					_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"large.txt","file_size":1}}`))
					return
				}
				if knownLength {
					w.Header().Set("Content-Length", fmt.Sprint(MaxFileSize+1))
				} else {
					w.(http.Flusher).Flush()
				}
				chunk := strings.Repeat("x", 1024*1024)
				for range 21 {
					if _, err := w.Write([]byte(chunk)); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			client := NewClient("SECRET")
			client.baseURL = server.URL
			if _, err := client.Download(context.Background(), "file"); err != errFileTooLarge {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestRetryAfterSucceeds(t *testing.T) {
	var mu sync.Mutex
	var times []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		attempt := len(times)
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`))
			return
		}
		sendOK(w)
	}))
	defer server.Close()
	client := NewClient("SECRET")
	client.baseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := client.Send(ctx, 42, 0, "hello", ""); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(times) != 2 || times[1].Sub(times[0]) < time.Second {
		t.Fatalf("retry_after ignored: %v", times)
	}
}
