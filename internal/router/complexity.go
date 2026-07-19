package router

import (
	"regexp"
	"strings"

	"github.com/AymanYouss/relay/internal/apitypes"
)

// Classifier scores prompt complexity in [0,1]. A higher score means the prompt
// is more likely to need a stronger (and more expensive) model.
type Classifier interface {
	Score(req *apitypes.ChatCompletionRequest) float64
}

// HeuristicClassifier estimates complexity from signals that correlate with
// task difficulty without an extra model round-trip: prompt length, multi-turn
// depth, presence of code or reasoning cues, and structural markers. It is fast,
// deterministic and dependency-free; production deployments can swap in a
// learned classifier behind the same interface.
type HeuristicClassifier struct{}

// NewHeuristicClassifier returns a HeuristicClassifier.
func NewHeuristicClassifier() *HeuristicClassifier { return &HeuristicClassifier{} }

var (
	codeFence    = regexp.MustCompile("```|\\bfunction\\b|\\bclass\\b|\\bdef \\b|=>|;\\s*$")
	reasoningCue = regexp.MustCompile(`(?i)\b(why|prove|derive|analy[sz]e|explain in detail|step[- ]by[- ]step|trade[- ]?offs?|design|architect|optimi[sz]e|debug|refactor|compare|evaluate|reason)\b`)
	easyCue      = regexp.MustCompile(`(?i)\b(hi|hello|hey|thanks|thank you|yes|no|ok|okay|what is|who is|define|translate|summari[sz]e|list|capital of)\b`)
	mathCue      = regexp.MustCompile(`[∫∑√≥≤≠∞]|\bintegral\b|\bderivative\b|\bmatrix\b|\btheorem\b|\bequation\b`)
)

// Score returns a complexity estimate in [0,1].
func (h *HeuristicClassifier) Score(req *apitypes.ChatCompletionRequest) float64 {
	prompt := req.PromptText()
	lower := strings.ToLower(prompt)
	words := len(strings.Fields(prompt))

	var score float64

	// Length: longer prompts trend harder. Saturates around 400 words.
	switch {
	case words <= 12:
		score += 0.05
	case words <= 40:
		score += 0.20
	case words <= 120:
		score += 0.35
	case words <= 400:
		score += 0.45
	default:
		score += 0.55
	}

	// Multi-turn conversations carry accumulated context and intent.
	turns := 0
	for _, m := range req.Messages {
		if m.Role == apitypes.RoleUser || m.Role == apitypes.RoleAssistant {
			turns++
		}
	}
	if turns >= 6 {
		score += 0.15
	} else if turns >= 3 {
		score += 0.08
	}

	// Content signals.
	if codeFence.MatchString(prompt) {
		score += 0.20
	}
	if reasoningCue.MatchString(lower) {
		score += 0.18
	}
	if mathCue.MatchString(lower) {
		score += 0.15
	}
	// Tool use implies an agentic, multi-step task.
	if len(req.Tools) > 0 {
		score += 0.12
	}
	// Question marks hint at open-ended queries; many suggests decomposition.
	if q := strings.Count(prompt, "?"); q >= 2 {
		score += 0.05
	}

	// Trivial small talk pulls the score back down.
	if easyCue.MatchString(lower) && words <= 20 && !codeFence.MatchString(prompt) {
		score -= 0.25
	}

	return clamp01(score)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
