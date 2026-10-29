package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/mgrubiyan/pomnibot/backend/internal/speechkit"
)

// speechStreamPath is a WebSocket, outside the OpenAPI contract: ogen
// serves plain requests only.
//
// The mini app sends {"authorization": "max …" or "tma …"} first (browsers
// set no headers on a WebSocket), then the recording as binary messages of
// mono 16-bit PCM at 16 kHz while it is spoken, then {"type": "end"}. The
// server answers {"text": "…"} each time the text heard so far changes and
// {"text": "…", "done": true} once it is fixed. A refusal is
// {"error": "forbidden" | "sign_in_expired" | "profile_required" | "failed"}
// before the connection closes.
const speechStreamPath = "/api/speech/stream"

const (
	// speechStreamTime bounds a session: 30 seconds of speech and the time
	// to fix the last phrase.
	speechStreamTime = 45 * time.Second
	// speechMessageLimit is far over the pieces of a fraction of a second
	// the mini app sends.
	speechMessageLimit = 64 << 10
)

// speechAuthTime is how long the server waits for the first message,
// unless APIHandler.speechAuthTime says otherwise.
const speechAuthTime = 5 * time.Second

// SpeechStreamer opens live recognition sessions.
type SpeechStreamer interface {
	Open(ctx context.Context) (SpeechSession, error)
}

// SpeechSession is one recording, ended by its context. Send and CloseSend
// are called from one goroutine, Next from another. Next returns the whole
// text heard so far each time it changes, and io.EOF once it is fixed.
type SpeechSession interface {
	Send(pcm []byte) error
	CloseSend() error
	Next() (string, error)
}

// SpeechKitStreamer serves sessions from SpeechKit.
func SpeechKitStreamer(s *speechkit.Streamer) SpeechStreamer { return speechKitStreamer{s} }

type speechKitStreamer struct{ s *speechkit.Streamer }

func (s speechKitStreamer) Open(ctx context.Context) (SpeechSession, error) {
	session, err := s.s.Open(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// WithSpeechStream shows readers their spoken answer while they speak.
func (h *APIHandler) WithSpeechStream(s SpeechStreamer) *APIHandler {
	h.speechStream = s
	return h
}

type speechReply struct {
	Text  string `json:"text,omitempty"`
	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
}

type speechControl struct {
	Authorization string `json:"authorization"`
	Type          string `json:"type"`
}

// speechStream serves speechStreamPath; auth checks the first message as
// AuthMiddleware checks a header.
func speechStream(h *APIHandler, auth authenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h == nil || h.speechStream == nil {
			writeAuthError(w, http.StatusServiceUnavailable, "speech recognition is off", "")
			return
		}
		// The sign-in comes inside the connection and no cookie is used, so
		// checking the Origin would protect nothing.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(speechMessageLimit)

		// The upgrade lifts the server's 15 s timeouts; this bounds the
		// session instead.
		ctx, cancel := context.WithTimeout(r.Context(), speechStreamTime)
		defer cancel()
		wait := h.speechAuthTime
		if wait == 0 {
			wait = speechAuthTime
		}
		msg, ok := readSignIn(ctx, conn, wait)
		if !ok {
			return
		}
		ctx, refusal := signInSpeech(ctx, h, auth, msg.Authorization, r.URL.Path)
		if refusal != "" {
			writeSpeech(ctx, conn, speechReply{Error: refusal})
			_ = conn.Close(websocket.StatusPolicyViolation, refusal)
			return
		}

		session, err := h.speechStream.Open(ctx)
		if err != nil {
			failSpeech(ctx, conn, err)
			return
		}
		read := make(chan struct{})
		go func() {
			defer close(read)
			defer cancel() // a reader who leaves stops the paid session
			pumpSpeech(ctx, conn, session)
		}()
		defer func() {
			cancel()
			<-read
		}()

		var text string
		for {
			next, err := session.Next()
			if errors.Is(err, io.EOF) {
				writeSpeech(ctx, conn, speechReply{Text: text, Done: true})
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			if err != nil {
				if ctx.Err() == nil {
					failSpeech(ctx, conn, err)
				}
				return
			}
			text = next
			if !writeSpeech(ctx, conn, speechReply{Text: text}) {
				return
			}
		}
	})
}

// readSignIn waits briefly for the first message. Anything but a sign-in
// closes the connection.
func readSignIn(ctx context.Context, conn *websocket.Conn, wait time.Duration) (speechControl, bool) {
	// Read closes the connection when its context ends: a client that says
	// nothing is let go.
	first, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	kind, data, err := conn.Read(first)
	if err != nil {
		return speechControl{}, false
	}
	var msg speechControl
	if kind != websocket.MessageText || json.Unmarshal(data, &msg) != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "sign in first")
		return speechControl{}, false
	}
	return msg, true
}

// signInSpeech gives ctx the reader's account, or names the refusal.
func signInSpeech(ctx context.Context, h *APIHandler, auth authenticator, authorization, path string) (context.Context, string) {
	signed, fail := auth.authenticate(ctx, authorization, path)
	if fail != nil {
		if fail.code != "" {
			return ctx, fail.code
		}
		return ctx, "failed"
	}
	// In production the unsigned reader is the shared account, and every
	// session costs money; locally Browser Preview may speak.
	_, ok, err := h.profileAccount(signed)
	if err != nil {
		internalError(signed, "SpeechStream", err)
		return ctx, "failed"
	}
	if !ok {
		return ctx, "forbidden"
	}
	return signed, ""
}

// pumpSpeech passes the recording on until the reader ends it, leaves or
// reaches 30 seconds.
func pumpSpeech(ctx context.Context, conn *websocket.Conn, session SpeechSession) {
	sent, ended := 0, false
	end := func() {
		if !ended {
			ended = true
			_ = session.CloseSend()
		}
	}
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if ended {
			continue // read on for the reader leaving
		}
		switch kind {
		case websocket.MessageBinary:
			if len(data)%2 != 0 {
				end() // not 16-bit samples: the rest would be noise
				continue
			}
			data = data[:min(len(data), speechkit.MaxPCMBytes-sent)]
			if len(data) > 0 {
				if err := session.Send(data); err != nil {
					return
				}
				sent += len(data)
			}
			if sent >= speechkit.MaxPCMBytes {
				end()
			}
		case websocket.MessageText:
			var msg speechControl
			if json.Unmarshal(data, &msg) == nil && msg.Type == "end" {
				end()
			}
		}
	}
}

func failSpeech(ctx context.Context, conn *websocket.Conn, err error) {
	internalError(ctx, "SpeechStream", err)
	writeSpeech(ctx, conn, speechReply{Error: "failed"})
	_ = conn.Close(websocket.StatusInternalError, "failed")
}

func writeSpeech(ctx context.Context, conn *websocket.Conn, reply speechReply) bool {
	data, err := json.Marshal(reply)
	if err != nil {
		return false
	}
	return conn.Write(ctx, websocket.MessageText, data) == nil
}
