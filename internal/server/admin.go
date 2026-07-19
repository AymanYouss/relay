package server

import (
	"io/fs"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/AymanYouss/relay/internal/server/webui"
	"github.com/AymanYouss/relay/internal/usage"
)

// handleDashboard returns the aggregated analytics payload for the admin UI,
// enriched with each key's configured budget and rate limit.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	window := 7
	if q := r.URL.Query().Get("window"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 90 {
			window = v
		}
	}

	dash, err := s.deps.Recorder.Dashboard(r.Context(), window)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "api_error", "")
		return
	}

	// Merge configured keys so keys with no usage still appear, and attach limits.
	usageByID := make(map[string]usage.KeyUsage, len(dash.Keys))
	for _, k := range dash.Keys {
		usageByID[k.KeyID] = k
	}
	merged := make([]usage.KeyUsage, 0, len(usageByID))
	seen := map[string]bool{}
	for _, p := range s.deps.Keys.Principals() {
		ku := usageByID[p.ID]
		ku.KeyID = p.ID
		ku.Name = p.Name
		ku.BudgetUSD = p.MonthlyBudgetUSD
		ku.RateLimitRPM = p.RateLimitRPM
		if p.MonthlyBudgetUSD > 0 {
			ku.BudgetUsedPct = ku.CostUSD / p.MonthlyBudgetUSD * 100
		}
		merged = append(merged, ku)
		seen[p.ID] = true
	}
	// Preserve any usage rows whose key is no longer configured.
	for _, k := range dash.Keys {
		if !seen[k.KeyID] {
			merged = append(merged, k)
		}
	}
	dash.Keys = merged

	writeJSON(w, http.StatusOK, dash)
}

// handleAdminConfig returns a redacted view of the running configuration so the
// dashboard can render providers, models and routing without exposing secrets.
func (s *Server) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.deps.Config
	type modelView struct {
		Name            string  `json:"name"`
		Provider        string  `json:"provider"`
		Tier            string  `json:"tier"`
		InputPricePerM  float64 `json:"input_price_per_m"`
		OutputPricePerM float64 `json:"output_price_per_m"`
	}
	type providerView struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	out := map[string]any{
		"service_name": cfg.Telemetry.ServiceName,
		"version":      s.deps.Version,
		"cache": map[string]any{
			"enabled":              cfg.Cache.Enabled,
			"similarity_threshold": cfg.Cache.SimilarityThreshold,
			"ttl_seconds":          cfg.Cache.TTL.Seconds(),
		},
		"router": map[string]any{
			"strategy":             cfg.Router.Strategy,
			"cheap_model":          cfg.Router.CheapModel,
			"strong_model":         cfg.Router.StrongModel,
			"complexity_threshold": cfg.Router.ComplexityThreshold,
		},
	}
	var models []modelView
	for _, m := range cfg.Models {
		models = append(models, modelView{m.Name, m.Provider, m.Tier, m.InputPricePerM, m.OutputPricePerM})
	}
	var providers []providerView
	for _, p := range cfg.Providers {
		providers = append(providers, providerView{p.Name, p.Kind})
	}
	out["models"] = models
	out["providers"] = providers
	writeJSON(w, http.StatusOK, out)
}

// mountDashboard serves the built single-page dashboard with SPA fallback so
// client-side routes resolve to index.html.
func (s *Server) mountDashboard(r chi.Router) {
	sub, err := fs.Sub(webui.Assets, "dist")
	if err != nil {
		s.deps.Logger.Warn("dashboard assets unavailable", "err", err)
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path
		if path != "/" {
			if _, err := fs.Stat(sub, trimLeadingSlash(path)); err != nil {
				// Unknown path: serve index.html for client-side routing.
				req = cloneWithPath(req, "/")
			}
		}
		fileServer.ServeHTTP(w, req)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}

func cloneWithPath(r *http.Request, path string) *http.Request {
	r2 := r.Clone(r.Context())
	r2.URL.Path = path
	return r2
}
