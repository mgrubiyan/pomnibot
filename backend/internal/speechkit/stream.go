package speechkit

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"strings"

	stt "github.com/yandex-cloud/go-genproto/yandex/cloud/ai/stt/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

// Streamer recognizes speech while it is spoken, on SpeechKit v3 over one
// gRPC connection shared by every session. It is safe for concurrent use.
type Streamer struct {
	cfg    Config
	conn   *grpc.ClientConn // nil when the connection is not ours to close
	client stt.RecognizerClient
}

// NewStreamer returns a Streamer. The connection opens on the first session.
func NewStreamer(cfg Config) (*Streamer, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.FolderID = strings.TrimSpace(cfg.FolderID)
	cfg.GRPCAddr = strings.TrimSpace(cfg.GRPCAddr)
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = DefaultGRPCAddr
	}
	if cfg.APIKey == "" {
		return nil, errors.New("speechkit: YC_API_KEY is required but empty")
	}
	conn, err := grpc.NewClient(cfg.GRPCAddr, grpc.WithTransportCredentials(credentials.NewTLS(
		&tls.Config{RootCAs: tlsroot.Pool(), MinVersion: tls.VersionTLS12})))
	if err != nil {
		return nil, fmt.Errorf("speechkit: connect %s: %w", cfg.GRPCAddr, err)
	}
	s := newStreamer(cfg, conn)
	s.conn = conn
	return s, nil
}

func newStreamer(cfg Config, conn grpc.ClientConnInterface) *Streamer {
	return &Streamer{cfg: cfg, client: stt.NewRecognizerClient(conn)}
}

// Close closes the connection; sessions still open fail.
func (s *Streamer) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// Session is one recording. Send and CloseSend belong to one goroutine,
// Next to one other.
type Session struct {
	stream grpc.BidiStreamingClient[stt.StreamingRequest, stt.StreamingResponse]

	// finals are the phrases the service has fixed, refined in place;
	// partial is its guess at the phrase being spoken.
	finals  []string
	partial string
	told    string
}

// Open starts a session for Russian speech in mono 16-bit PCM at
// SampleRate. It ends with ctx.
func (s *Streamer) Open(ctx context.Context) (*Session, error) {
	md := metadata.Pairs(
		"authorization", "Api-Key "+s.cfg.APIKey,
		// Students' voices: no logging on the service side.
		"x-data-logging-enabled", "false",
	)
	if s.cfg.FolderID != "" {
		md.Set("x-folder-id", s.cfg.FolderID)
	}
	stream, err := s.client.RecognizeStreaming(metadata.NewOutgoingContext(ctx, md))
	if err != nil {
		return nil, fmt.Errorf("speechkit: open stream: %w", err)
	}
	err = stream.Send(&stt.StreamingRequest{Event: &stt.StreamingRequest_SessionOptions{SessionOptions: &stt.StreamingOptions{
		RecognitionModel: &stt.RecognitionModelOptions{
			AudioFormat: &stt.AudioFormatOptions{AudioFormat: &stt.AudioFormatOptions_RawAudio{RawAudio: &stt.RawAudio{
				AudioEncoding: stt.RawAudio_LINEAR16_PCM, SampleRateHertz: SampleRate, AudioChannelCount: 1,
			}}},
			TextNormalization:   &stt.TextNormalizationOptions{TextNormalization: stt.TextNormalizationOptions_TEXT_NORMALIZATION_ENABLED},
			LanguageRestriction: &stt.LanguageRestrictionOptions{RestrictionType: stt.LanguageRestrictionOptions_WHITELIST, LanguageCode: []string{"ru-RU"}},
			AudioProcessingType: stt.RecognitionModelOptions_REAL_TIME,
		},
	}}})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("speechkit: send options: %w", err)
	}
	// io.EOF here means the service has already ended the stream: Next
	// tells why.
	return &Session{stream: stream}, nil
}

// Send passes on a piece of the recording.
func (s *Session) Send(pcm []byte) error {
	err := s.stream.Send(&stt.StreamingRequest{Event: &stt.StreamingRequest_Chunk{Chunk: &stt.AudioChunk{Data: pcm}}})
	if err != nil {
		return fmt.Errorf("speechkit: send audio: %w", err)
	}
	return nil
}

// CloseSend says the recording is over; the service then fixes the last
// phrase and ends the stream.
func (s *Session) CloseSend() error {
	return s.stream.CloseSend()
}

// Next waits until the text heard so far changes and returns it whole.
// After the last phrase is fixed it returns io.EOF.
func (s *Session) Next() (string, error) {
	for {
		resp, err := s.stream.Recv()
		if errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		if err != nil {
			return "", fmt.Errorf("speechkit: stream: %w", err)
		}
		switch e := resp.GetEvent().(type) {
		case *stt.StreamingResponse_Partial:
			// An empty partial comes just before the final: kept, the
			// text would blink out for a moment.
			if text := firstText(e.Partial); text != "" {
				s.partial = text
			}
		case *stt.StreamingResponse_Final:
			s.finals = append(s.finals, firstText(e.Final))
			s.partial = ""
		case *stt.StreamingResponse_FinalRefinement:
			i := e.FinalRefinement.GetFinalIndex()
			if text := firstText(e.FinalRefinement.GetNormalizedText()); i >= 0 && i < int64(len(s.finals)) && text != "" {
				s.finals[i] = text
			}
		default:
			continue // keep-alives, end of utterance
		}
		if text := s.text(); text != s.told {
			s.told = text
			return text, nil
		}
	}
}

func (s *Session) text() string {
	parts := make([]string, 0, len(s.finals)+1)
	for _, f := range append(s.finals, s.partial) {
		if f = strings.TrimSpace(f); f != "" {
			parts = append(parts, f)
		}
	}
	return strings.Join(parts, " ")
}

func firstText(u *stt.AlternativeUpdate) string {
	if alts := u.GetAlternatives(); len(alts) > 0 {
		return alts[0].GetText()
	}
	return ""
}
