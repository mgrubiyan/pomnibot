package speechkit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	stt "github.com/yandex-cloud/go-genproto/yandex/cloud/ai/stt/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeRecognizer plays RecognizeStreaming: it keeps what it was sent and
// answers each audio chunk with the events script gives for it.
type fakeRecognizer struct {
	stt.UnimplementedRecognizerServer

	// script answers the n-th chunk (from 0); nil events send nothing.
	script func(n int) []*stt.StreamingResponse
	// atEnd answers the client's CloseSend.
	atEnd []*stt.StreamingResponse
	fail  error

	mu      sync.Mutex
	md      metadata.MD
	options *stt.StreamingOptions
	audio   bytes.Buffer
}

func (f *fakeRecognizer) RecognizeStreaming(stream grpc.BidiStreamingServer[stt.StreamingRequest, stt.StreamingResponse]) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	f.mu.Lock()
	f.md = md
	f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	for n := 0; ; {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			for _, resp := range f.atEnd {
				if err := stream.Send(resp); err != nil {
					return err
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
		f.mu.Lock()
		if o := req.GetSessionOptions(); o != nil {
			f.options = o
		}
		chunk := req.GetChunk()
		if chunk != nil {
			f.audio.Write(chunk.Data)
		}
		f.mu.Unlock()
		if chunk == nil || f.script == nil {
			continue
		}
		for _, resp := range f.script(n) {
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
		n++
	}
}

func partial(text string) *stt.StreamingResponse {
	return &stt.StreamingResponse{Event: &stt.StreamingResponse_Partial{Partial: alternatives(text)}}
}

func final(text string) *stt.StreamingResponse {
	return &stt.StreamingResponse{Event: &stt.StreamingResponse_Final{Final: alternatives(text)}}
}

func refinement(index int64, text string) *stt.StreamingResponse {
	return &stt.StreamingResponse{Event: &stt.StreamingResponse_FinalRefinement{FinalRefinement: &stt.FinalRefinement{
		FinalIndex: index,
		Type:       &stt.FinalRefinement_NormalizedText{NormalizedText: alternatives(text)},
	}}}
}

func working() *stt.StreamingResponse {
	return &stt.StreamingResponse{Event: &stt.StreamingResponse_StatusCode{StatusCode: &stt.StatusCode{CodeType: stt.CodeType_WORKING}}}
}

func alternatives(text string) *stt.AlternativeUpdate {
	if text == "" {
		return &stt.AlternativeUpdate{}
	}
	return &stt.AlternativeUpdate{Alternatives: []*stt.Alternative{{Text: text}}}
}

func newTestStreamer(t *testing.T, f *fakeRecognizer, folder string) *Streamer {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	stt.RegisterRecognizerServer(server, f)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return newStreamer(Config{APIKey: "key", FolderID: folder}, conn)
}

// readAll reads updates until the session ends.
func readAll(t *testing.T, s *Session) []string {
	t.Helper()
	var got []string
	for {
		text, err := s.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		got = append(got, text)
	}
}

func TestStreamOpensWithRussianPCMOptions(t *testing.T) {
	f := &fakeRecognizer{}
	s := newTestStreamer(t, f, "folder-1")
	session, err := s.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Send([]byte{1, 0, 2, 0}); err != nil {
		t.Fatal(err)
	}
	if err := session.CloseSend(); err != nil {
		t.Fatal(err)
	}
	readAll(t, session)

	f.mu.Lock()
	defer f.mu.Unlock()
	for key, want := range map[string]string{"authorization": "Api-Key key", "x-folder-id": "folder-1", "x-data-logging-enabled": "false"} {
		if got := f.md.Get(key); len(got) != 1 || got[0] != want {
			t.Errorf("metadata %s = %v, want %q", key, got, want)
		}
	}
	model := f.options.GetRecognitionModel()
	raw := model.GetAudioFormat().GetRawAudio()
	if raw.GetAudioEncoding() != stt.RawAudio_LINEAR16_PCM || raw.GetSampleRateHertz() != SampleRate || raw.GetAudioChannelCount() != 1 {
		t.Errorf("audio format = %v", raw)
	}
	if model.GetAudioProcessingType() != stt.RecognitionModelOptions_REAL_TIME {
		t.Errorf("processing = %v, want real time", model.GetAudioProcessingType())
	}
	if lang := model.GetLanguageRestriction(); lang.GetRestrictionType() != stt.LanguageRestrictionOptions_WHITELIST || len(lang.GetLanguageCode()) != 1 || lang.GetLanguageCode()[0] != "ru-RU" {
		t.Errorf("language = %v", lang)
	}
	if model.GetTextNormalization().GetTextNormalization() != stt.TextNormalizationOptions_TEXT_NORMALIZATION_ENABLED {
		t.Errorf("normalization = %v", model.GetTextNormalization())
	}
	if !bytes.Equal(f.audio.Bytes(), []byte{1, 0, 2, 0}) {
		t.Errorf("audio = %v", f.audio.Bytes())
	}
}

func TestStreamWithoutFolder(t *testing.T) {
	f := &fakeRecognizer{}
	s := newTestStreamer(t, f, "")
	session, err := s.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = session.CloseSend()
	readAll(t, session)
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.md.Get("x-folder-id"); len(got) != 0 {
		t.Errorf("x-folder-id = %v without a folder", got)
	}
}

// The text grows with the partials; finals stay and the next phrase is
// added after them; a refinement replaces its final; repeats and
// keep-alives give nothing new.
func TestStreamTellsTheTextAsItGrows(t *testing.T) {
	steps := [][]*stt.StreamingResponse{
		{working(), partial("первичный")},
		{partial("первичный ключ"), working()},
		{partial("первичный ключ")},
		{partial(""), final("первичный ключ"), partial("")},
		{partial("однозначно")},
	}
	f := &fakeRecognizer{
		script: func(n int) []*stt.StreamingResponse { return steps[n] },
		atEnd:  []*stt.StreamingResponse{final("однозначно"), refinement(0, "Первичный ключ"), refinement(1, "однозначно"), partial("")},
	}
	s := newTestStreamer(t, f, "")
	session, err := s.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range steps {
			_ = session.Send([]byte{0, 0})
			time.Sleep(5 * time.Millisecond)
		}
		_ = session.CloseSend()
	}()

	got := readAll(t, session)
	want := []string{"первичный", "первичный ключ", "первичный ключ однозначно", "Первичный ключ однозначно"}
	if len(got) != len(want) {
		t.Fatalf("updates = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("updates = %q, want %q", got, want)
		}
	}
}

func TestStreamReportsServiceErrors(t *testing.T) {
	f := &fakeRecognizer{fail: status.Error(codes.PermissionDenied, "no access to the folder")}
	s := newTestStreamer(t, f, "")
	session, err := s.Open(context.Background())
	if err == nil {
		_ = session.CloseSend()
		_, err = session.Next()
	}
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want the service's refusal", err)
	}
	if status.Code(errors.Unwrap(err)) != codes.PermissionDenied && status.Code(err) != codes.PermissionDenied {
		t.Errorf("err = %v, want PermissionDenied kept", err)
	}
}

func TestStreamStopsWithItsContext(t *testing.T) {
	f := &fakeRecognizer{}
	s := newTestStreamer(t, f, "")
	ctx, cancel := context.WithCancel(context.Background())
	session, err := s.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := session.Next()
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("Next after cancel = %v, want an error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Next did not stop with its context")
	}
}

func TestStreamConfigFromEnv(t *testing.T) {
	t.Setenv("YC_API_KEY", "key")
	t.Setenv("YC_STT_GRPC_ADDR", "")
	cfg, _ := ConfigFromEnv()
	if cfg.GRPCAddr != DefaultGRPCAddr {
		t.Fatalf("GRPCAddr = %q", cfg.GRPCAddr)
	}
	t.Setenv("YC_STT_GRPC_ADDR", "stt.example.test:443")
	if cfg, _ := ConfigFromEnv(); cfg.GRPCAddr != "stt.example.test:443" {
		t.Fatalf("GRPCAddr = %q, want the override", cfg.GRPCAddr)
	}
}
