package openrouter

import (
	"context"
	"net/http/httptrace"
	"sync/atomic"
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
type requestTrace struct{ phase atomic.Int32 }

func traceRequest(ctx context.Context) (context.Context, *requestTrace) {
	state := &requestTrace{}
	state.phase.Store(traceCreated)
	trace := &httptrace.ClientTrace{
		GetConn:           func(string) { state.phase.Store(traceConnecting) },
		DNSStart:          func(httptrace.DNSStartInfo) { state.phase.Store(traceDNS) },
		ConnectStart:      func(string, string) { state.phase.Store(traceConnecting) },
		TLSHandshakeStart: func() { state.phase.Store(traceTLS) },
		GotConn:           func(httptrace.GotConnInfo) { state.phase.Store(traceWriting) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				state.phase.Store(traceWaitingHeaders)
			}
		},
		GotFirstResponseByte: func() { state.phase.Store(traceResponseStarted) },
	}
	return httptrace.WithClientTrace(ctx, trace), state
}
func (s *requestTrace) label() string {
	return [...]string{"request_created", "connecting", "dns", "tls_handshake", "writing_request", "waiting_response_headers", "response_started", "reading_response_body"}[s.phase.Load()]
}
