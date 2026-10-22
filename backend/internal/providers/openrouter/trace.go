package openrouter

import (
	"context"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

const (
	traceCreated int32 = iota
	traceConnecting
	traceDNS
	traceTLS
	traceWriting
	traceWaitingHeaders
	traceResponseStarted
	traceReadingBody
)

// Callbacks can run on transport goroutines. Only fixed phase labels are
// recorded: never headers, URLs, DNS addresses, prompts or response contents.
type requestTrace struct {
	phase     atomic.Int32
	started   time.Time
	written   atomic.Int64
	firstByte atomic.Int64
	reused    atomic.Bool
}

func traceRequest(ctx context.Context) (context.Context, *requestTrace) {
	state := &requestTrace{started: time.Now()}
	state.phase.Store(traceCreated)
	trace := &httptrace.ClientTrace{
		GetConn:           func(string) { state.phase.Store(traceConnecting) },
		DNSStart:          func(httptrace.DNSStartInfo) { state.phase.Store(traceDNS) },
		ConnectStart:      func(string, string) { state.phase.Store(traceConnecting) },
		TLSHandshakeStart: func() { state.phase.Store(traceTLS) },
		GotConn: func(info httptrace.GotConnInfo) {
			state.reused.Store(info.Reused)
			state.phase.Store(traceWriting)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				state.written.Store(int64(time.Since(state.started)))
				state.phase.Store(traceWaitingHeaders)
			}
		},
		GotFirstResponseByte: func() {
			state.firstByte.Store(int64(time.Since(state.started)))
			state.phase.Store(traceResponseStarted)
		},
	}
	return httptrace.WithClientTrace(ctx, trace), state
}

func (s *requestTrace) attributes() []any {
	return []any{
		"connection_reused", s.reused.Load(),
		"request_written_after_ms", float64(s.written.Load()) / float64(time.Millisecond),
		"first_byte_after_ms", float64(s.firstByte.Load()) / float64(time.Millisecond),
	}
}
func (s *requestTrace) label() string {
	return [...]string{"request_created", "connecting", "dns", "tls_handshake", "writing_request", "waiting_response_headers", "response_started", "reading_response_body"}[s.phase.Load()]
}
