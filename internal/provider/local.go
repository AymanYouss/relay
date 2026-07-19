package provider

import "time"

// Local adapts a self-hosted, OpenAI-compatible inference server such as vLLM,
// Ollama (with its OpenAI shim), Text Generation Inference, or llama.cpp's
// server. The wire format is identical to OpenAI, so it reuses that adapter and
// only overrides the reported Kind for routing and telemetry.
type Local struct {
	*OpenAI
}

// NewLocal constructs a provider for a self-hosted OpenAI-compatible server.
func NewLocal(name, baseURL, apiKey string, timeout time.Duration) *Local {
	// Local servers often ignore auth; a placeholder keeps the header well-formed.
	if apiKey == "" {
		apiKey = "sk-local"
	}
	return &Local{OpenAI: NewOpenAI(name, baseURL, apiKey, timeout)}
}

func (l *Local) Kind() string { return "local" }

// Compile-time interface checks.
var (
	_ Provider = (*OpenAI)(nil)
	_ Provider = (*Local)(nil)
	_ Provider = (*Anthropic)(nil)
)
