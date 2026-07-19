// Command mockllm is an OpenAI-compatible mock upstream used for local
// benchmarking and demos. It simulates realistic per-model inference latency and
// token usage so the gateway's routing, caching and accounting can be exercised
// without real provider credentials or spend.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type chatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

// modelProfile describes the simulated behavior of a model.
type modelProfile struct {
	baseMs   int
	jitterMs int
}

func profileFor(model string) modelProfile {
	switch {
	case strings.Contains(model, "mini") || strings.Contains(model, "haiku") || strings.Contains(model, "8b"):
		return modelProfile{baseMs: 320, jitterMs: 260}
	default: // strong models are slower
		return modelProfile{baseMs: 680, jitterMs: 520}
	}
}

func main() {
	addr := flag.String("addr", ":1234", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleChat)
	mux.HandleFunc("/chat/completions", handleChat)

	log.Printf("mockllm listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := profileFor(req.Model)
	delay := time.Duration(p.baseMs+rand.Intn(p.jitterMs+1)) * time.Millisecond
	time.Sleep(delay)

	promptTokens := 0
	for _, m := range req.Messages {
		promptTokens += len(strings.Fields(m.Content))*4/3 + 4
	}
	completionTokens := 120 + rand.Intn(320)
	content := fmt.Sprintf("Simulated response from %s (%d tokens).", req.Model, completionTokens)

	resp := map[string]any{
		"id":      "chatcmpl-mock",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]string{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
