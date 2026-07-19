package router

import (
	"strings"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/config"
)

// Strategy names.
const (
	StrategyAuto    = "auto"
	StrategyCost    = "cost"
	StrategyQuality = "quality"
)

// Hop is one step in a failover chain: a logical model to attempt.
type Hop struct {
	Model string
}

// Decision is the router's output for a request.
type Decision struct {
	// Chain is the ordered list of models to attempt (primary first).
	Chain []Hop
	// Strategy that produced the decision.
	Strategy string
	// Complexity is the classifier score, when the auto strategy was used.
	Complexity float64
	// Reason is a short human-readable explanation, surfaced in traces and the
	// dashboard's routing view.
	Reason string
}

// Primary returns the first hop's model, or "" for an empty chain.
func (d Decision) Primary() string {
	if len(d.Chain) == 0 {
		return ""
	}
	return d.Chain[0].Model
}

// Router turns a request into a routing decision using the configured strategy
// and per-model fallback chains.
type Router struct {
	strategy    string
	cheapModel  string
	strongModel string
	threshold   float64
	models      map[string]config.ModelConfig
	classifier  Classifier
}

// New builds a Router from configuration.
func New(cfg config.RouterConfig, models []config.ModelConfig, classifier Classifier) *Router {
	m := make(map[string]config.ModelConfig, len(models))
	for _, mc := range models {
		m[mc.Name] = mc
	}
	if classifier == nil {
		classifier = NewHeuristicClassifier()
	}
	return &Router{
		strategy:    cfg.Strategy,
		cheapModel:  cfg.CheapModel,
		strongModel: cfg.StrongModel,
		threshold:   cfg.ComplexityThreshold,
		models:      m,
		classifier:  classifier,
	}
}

// routingAliases map user-facing model strings to a strategy.
var routingAliases = map[string]string{
	"auto":          StrategyAuto,
	"relay-auto":    StrategyAuto,
	"relay/auto":    StrategyAuto,
	"cost":          StrategyCost,
	"relay-cost":    StrategyCost,
	"relay/cheap":   StrategyCost,
	"quality":       StrategyQuality,
	"relay-quality": StrategyQuality,
	"relay/best":    StrategyQuality,
}

// Resolve computes the routing decision for a request.
func (r *Router) Resolve(req *apitypes.ChatCompletionRequest) Decision {
	// An explicit relay_route wins; otherwise the model field may itself be a
	// routing alias; otherwise the model is pinned.
	route := strings.TrimSpace(req.RelayRoute)
	if route == "" {
		if strat, ok := routingAliases[strings.ToLower(req.Model)]; ok {
			route = strat
		} else if req.Model == "" {
			route = r.strategy
		} else if _, defined := r.models[req.Model]; defined {
			return r.pinned(req.Model, "client pinned model")
		} else {
			// Unknown model: fall back to the configured default strategy so the
			// gateway degrades gracefully rather than 404-ing.
			route = r.strategy
		}
	}

	// route may be a strategy or a pinned model name.
	if _, defined := r.models[route]; defined {
		return r.pinned(route, "client requested route model")
	}

	switch route {
	case StrategyCost:
		return r.pinned(r.cheapModel, "cost strategy: cheapest model")
	case StrategyQuality:
		return r.pinned(r.strongModel, "quality strategy: strongest model")
	default: // auto
		score := r.classifier.Score(req)
		if score >= r.threshold {
			d := r.pinned(r.strongModel, "auto: high complexity")
			d.Strategy = StrategyAuto
			d.Complexity = score
			return d
		}
		d := r.pinned(r.cheapModel, "auto: low complexity")
		d.Strategy = StrategyAuto
		d.Complexity = score
		return d
	}
}

// pinned builds a decision whose chain is the model plus its configured
// fallbacks, de-duplicated and skipping undefined models.
func (r *Router) pinned(model, reason string) Decision {
	chain := r.chainFor(model)
	strat := StrategyQuality
	if model == r.cheapModel {
		strat = StrategyCost
	}
	return Decision{Chain: chain, Strategy: strat, Reason: reason}
}

func (r *Router) chainFor(model string) []Hop {
	seen := map[string]bool{}
	var chain []Hop
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		if _, ok := r.models[name]; !ok {
			return
		}
		seen[name] = true
		chain = append(chain, Hop{Model: name})
	}
	add(model)
	if mc, ok := r.models[model]; ok {
		for _, fb := range mc.Fallbacks {
			add(fb)
		}
	}
	return chain
}
