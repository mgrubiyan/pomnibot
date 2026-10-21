package providers

import "errors"

// ErrCapability means a backend cannot process the complete task.
var ErrCapability = errors.New("provider does not support the task")

// Capabilities are configured for a concrete model, not inferred from a vendor
// name. MaxInputBytes conservatively bounds textual input; images are separate.
type Capabilities struct {
	Images        bool
	Structured    bool
	MaxInputBytes int
}

// Profile separates document/task concurrency from a backend-wide request cap.
// These conservative defaults can be tuned independently of document queues.
type Profile struct {
	Requests, Tasks, SourceBytes int
	Capabilities                 Capabilities
}

// GigaChatProfile supplies conservative limits for the current integration.
func GigaChatProfile() Profile {
	return Profile{Requests: 1, Tasks: 1, SourceBytes: 20000, Capabilities: Capabilities{Images: true, Structured: false, MaxInputBytes: 100000}}
}

// QwenProfile supplies independent task and request limits.
func QwenProfile() Profile {
	return Profile{Requests: 5, Tasks: 2, SourceBytes: 24000, Capabilities: Capabilities{Images: true, Structured: true, MaxInputBytes: 160000}}
}

// OpenRouterProfile keeps parallel topic work independent of GigaChat.
func OpenRouterProfile() Profile {
	return Profile{Requests: 5, Tasks: 3, SourceBytes: 24000, Capabilities: Capabilities{Images: true, Structured: true, MaxInputBytes: 160000}}
}
