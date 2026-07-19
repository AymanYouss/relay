package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/gateway"
	"github.com/AymanYouss/relay/internal/provider"
)

const maxBodyBytes = 16 << 20 // 16 MiB request cap

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message, typ, code string) {
	writeJSON(w, status, apitypes.NewAPIError(message, typ, code))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": s.deps.Config.Telemetry.ServiceName,
		"version": s.deps.Version,
		"time":    time.Now().UTC(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	list := apitypes.ModelList{Object: "list"}
	for _, m := range s.deps.Config.Models {
		list.Data = append(list.Data, apitypes.Model{
			ID:      m.Name,
			Object:  "model",
			Created: 0,
			OwnedBy: m.Provider,
		})
	}
	// Expose routing aliases as callable "models" too.
	for _, alias := range []string{"auto", "cost", "quality"} {
		list.Data = append(list.Data, apitypes.Model{ID: alias, Object: "model", OwnedBy: "relay"})
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	var req apitypes.ChatCompletionRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages is required", "invalid_request_error", "messages")
		return
	}

	if req.Stream {
		s.streamChat(w, r, req)
		return
	}

	resp, err := s.deps.Gateway.Complete(r.Context(), p, &req)
	if err != nil {
		s.writeGatewayError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// streamChat writes an SSE stream of chat completion chunks.
func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, req apitypes.ChatCompletionRequest) {
	p := principalFrom(r.Context())
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported", "internal_error", "")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	enc := json.NewEncoder(w)
	wroteHeader := false
	onChunk := func(chunk apitypes.StreamChunk) error {
		if !wroteHeader {
			w.WriteHeader(http.StatusOK)
			wroteHeader = true
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return err
		}
		if err := enc.Encode(chunk); err != nil { // Encode appends a newline
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	meta, err := s.deps.Gateway.Stream(r.Context(), p, &req, onChunk)
	if err != nil {
		// If nothing has been written yet, we can still return a proper status.
		if !wroteHeader {
			s.writeGatewayError(w, err)
			return
		}
		// Otherwise surface the error inside the stream and terminate.
		_ = writeSSEError(w, err)
		flusher.Flush()
		return
	}

	// Emit a final relay metadata event, then the OpenAI terminator.
	if meta != nil {
		if b, mErr := json.Marshal(map[string]any{"relay": meta}); mErr == nil {
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req apitypes.EmbeddingRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
		return
	}
	if len(req.Input) == 0 {
		writeError(w, http.StatusBadRequest, "input is required", "invalid_request_error", "input")
		return
	}
	entry, ok := s.deps.Gateway.Catalog().Resolve(req.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown model %q", req.Model), "invalid_request_error", "model")
		return
	}
	resp, err := entry.Provider().Embeddings(r.Context(), req)
	if err != nil {
		s.writeGatewayError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// writeGatewayError maps domain errors to OpenAI-compatible HTTP responses.
func (s *Server) writeGatewayError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gateway.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, err.Error(), "rate_limit_error", "")
	case errors.Is(err, gateway.ErrBudgetExceeded):
		writeError(w, http.StatusPaymentRequired, err.Error(), "budget_error", "budget_exceeded")
	case errors.Is(err, gateway.ErrModelNotAllowed):
		writeError(w, http.StatusForbidden, err.Error(), "invalid_request_error", "model_not_allowed")
	case errors.Is(err, gateway.ErrNoUpstream):
		writeError(w, http.StatusServiceUnavailable, err.Error(), "api_error", "no_upstream")
	default:
		var pe *provider.Error
		if errors.As(err, &pe) {
			status := pe.StatusCode
			if status == 0 || status < 400 {
				status = http.StatusBadGateway
			}
			writeError(w, status, pe.Message, "api_error", "upstream_error")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), "api_error", "")
	}
}

func writeSSEError(w http.ResponseWriter, err error) error {
	env := apitypes.NewAPIError(err.Error(), "api_error", "")
	b, _ := json.Marshal(env)
	_, e := fmt.Fprintf(w, "data: %s\n\n", b)
	return e
}
