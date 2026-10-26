package http

import (
	"context"
	"io"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/speechkit"
)

// maxSpeechRequest fits the longest recording and its multipart headers.
const maxSpeechRequest = speechkit.MaxPCMBytes + 16<<10

// speechTime bounds recognition well inside the server's 15 s timeouts.
const speechTime = 10 * time.Second

// SpeechRecognizer turns a recording into text; speechkit.Client is one.
type SpeechRecognizer interface {
	Recognize(ctx context.Context, pcm []byte) (string, error)
}

// WithSpeech lets readers answer a card by voice.
func (h *APIHandler) WithSpeech(stt SpeechRecognizer) *APIHandler {
	h.speech = stt
	return h
}

// RecognizeSpeech turns a spoken answer into text for the reader to check
// and send. Only for a verified account: in production a request without a
// signature is the shared account, and every recording costs money.
func (h *APIHandler) RecognizeSpeech(ctx context.Context, req *contracts.SpeechUploadMultipart) (contracts.RecognizeSpeechRes, error) {
	if h.speech == nil {
		return &contracts.RecognizeSpeechServiceUnavailable{Message: "speech recognition is off"}, nil
	}
	_, ok, err := h.profileAccount(ctx)
	if err != nil {
		return &contracts.RecognizeSpeechInternalServerError{Message: internalError(ctx, "RecognizeSpeech", err)}, nil
	}
	if !ok {
		return &contracts.RecognizeSpeechForbidden{Message: "authorization required"}, nil
	}
	if req.Audio.Size > speechkit.MaxPCMBytes {
		return &contracts.RecognizeSpeechRequestEntityTooLarge{Message: "recording over 30 seconds"}, nil
	}
	pcm, err := io.ReadAll(io.LimitReader(req.Audio.File, speechkit.MaxPCMBytes+1))
	if err != nil {
		return &contracts.RecognizeSpeechBadRequest{Message: "read the recording: " + err.Error()}, nil
	}
	switch {
	case len(pcm) > speechkit.MaxPCMBytes:
		return &contracts.RecognizeSpeechRequestEntityTooLarge{Message: "recording over 30 seconds"}, nil
	case len(pcm) == 0:
		return &contracts.RecognizeSpeechBadRequest{Message: "the recording is empty"}, nil
	case len(pcm)%2 != 0:
		return &contracts.RecognizeSpeechBadRequest{Message: "the recording is not 16-bit PCM"}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, speechTime)
	defer cancel()
	text, err := h.speech.Recognize(ctx, pcm)
	if err != nil {
		// The service's reply may name the key or folder: only the log
		// gets it.
		internalError(ctx, "RecognizeSpeech", err)
		return &contracts.RecognizeSpeechInternalServerError{Message: "speech recognition failed"}, nil
	}
	return &contracts.SpeechText{Text: text}, nil
}
