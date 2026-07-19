package gateway

import (
	"fmt"

	"github.com/AymanYouss/relay/internal/config"
	"github.com/AymanYouss/relay/internal/provider"
	"github.com/AymanYouss/relay/internal/usage"
)

// modelEntry binds a logical model name to the concrete provider that serves it,
// the upstream (provider-native) model id, and its pricing and tier.
type modelEntry struct {
	name     string
	provider provider.Provider
	upstream string
	pricing  usage.Pricing
	tier     string
}

// catalog resolves logical model names to providers and pricing.
type catalog struct {
	models map[string]modelEntry
}

// newCatalog builds the catalog from configuration and a provider registry.
func newCatalog(cfg *config.Config, reg *provider.Registry) (*catalog, error) {
	c := &catalog{models: make(map[string]modelEntry, len(cfg.Models))}
	for _, m := range cfg.Models {
		p, ok := reg.Get(m.Provider)
		if !ok {
			return nil, fmt.Errorf("catalog: model %q references unknown provider %q", m.Name, m.Provider)
		}
		c.models[m.Name] = modelEntry{
			name:     m.Name,
			provider: p,
			upstream: m.Upstream,
			pricing:  usage.Pricing{InputPerM: m.InputPricePerM, OutputPerM: m.OutputPricePerM},
			tier:     m.Tier,
		}
	}
	return c, nil
}

// resolve returns the entry for a logical model name.
func (c *catalog) resolve(model string) (modelEntry, bool) {
	e, ok := c.models[model]
	return e, ok
}

// ModelInfo is a read-only view of a catalog entry for use by other packages
// (the HTTP layer resolves the provider for direct passthrough endpoints).
type ModelInfo struct {
	entry modelEntry
}

// Provider returns the provider serving the model.
func (m ModelInfo) Provider() provider.Provider { return m.entry.provider }

// Upstream returns the provider-native model id.
func (m ModelInfo) Upstream() string { return m.entry.upstream }

// Resolve looks up a logical model name and returns a read-only view.
func (c *catalog) Resolve(model string) (ModelInfo, bool) {
	e, ok := c.models[model]
	return ModelInfo{entry: e}, ok
}
