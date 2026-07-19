// Package config loads and validates Relay's runtime configuration.
//
// Configuration is expressed in YAML and layered with environment variables:
// any string value of the form ${VAR} or ${VAR:-default} is expanded at load
// time, which keeps provider secrets out of the config file and lets the same
// file promote unchanged from local Docker to Kubernetes.
package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration document.
type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Redis     RedisConfig      `yaml:"redis"`
	Cache     CacheConfig      `yaml:"cache"`
	Router    RouterConfig     `yaml:"router"`
	Providers []ProviderConfig `yaml:"providers"`
	Models    []ModelConfig    `yaml:"models"`
	Embedding EmbeddingConfig  `yaml:"embedding"`
	Telemetry TelemetryConfig  `yaml:"telemetry"`
	APIKeys   []APIKeyConfig   `yaml:"api_keys"`
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	Addr            string        `yaml:"addr"`
	AdminAddr       string        `yaml:"admin_addr"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	// TrustedAdminToken guards the admin/analytics API and dashboard.
	TrustedAdminToken string `yaml:"admin_token"`
}

// RedisConfig configures the shared Redis connection used for the semantic
// cache index, rate-limit counters, and usage accounting.
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	// Backend selects the vector store implementation. "redis" uses a Redis
	// vector index; "memory" is used for tests and single-node dev.
	Backend string `yaml:"backend"`
}

// CacheConfig controls the semantic cache.
type CacheConfig struct {
	Enabled             bool          `yaml:"enabled"`
	SimilarityThreshold float64       `yaml:"similarity_threshold"`
	TTL                 time.Duration `yaml:"ttl"`
	// MaxCandidates bounds how many neighbours the vector search returns.
	MaxCandidates int `yaml:"max_candidates"`
	// Namespace isolates cache entries per tenant/environment.
	Namespace string `yaml:"namespace"`
}

// RouterConfig controls semantic model routing.
type RouterConfig struct {
	// Default strategy: "auto" (complexity-aware), "cost", or "quality".
	Strategy string `yaml:"strategy"`
	// CheapModel and StrongModel name entries in the Models list.
	CheapModel  string `yaml:"cheap_model"`
	StrongModel string `yaml:"strong_model"`
	// ComplexityThreshold in [0,1]; prompts scoring above route to StrongModel.
	ComplexityThreshold float64 `yaml:"complexity_threshold"`
	// MaxRetries per request across the failover chain.
	MaxRetries int `yaml:"max_retries"`
	// RetryBackoff is the base backoff between attempts.
	RetryBackoff time.Duration `yaml:"retry_backoff"`
}

// ProviderConfig configures one upstream model provider.
type ProviderConfig struct {
	Name    string        `yaml:"name"`
	Kind    string        `yaml:"kind"` // openai | anthropic | local
	BaseURL string        `yaml:"base_url"`
	APIKey  string        `yaml:"api_key"`
	Timeout time.Duration `yaml:"timeout"`
	// Weight biases load when several providers can serve the same model.
	Weight int `yaml:"weight"`
}

// ModelConfig maps a logical model name to a provider and pricing.
type ModelConfig struct {
	Name     string `yaml:"name"`
	Provider string `yaml:"provider"`
	// Upstream is the provider-specific model id (defaults to Name).
	Upstream string `yaml:"upstream"`
	// Pricing is per 1M tokens, in USD.
	InputPricePerM  float64 `yaml:"input_price_per_m"`
	OutputPricePerM float64 `yaml:"output_price_per_m"`
	// Tier is a hint for routing: "cheap" or "strong".
	Tier string `yaml:"tier"`
	// Fallbacks are model names tried in order on upstream failure.
	Fallbacks []string `yaml:"fallbacks"`
}

// EmbeddingConfig configures the embedding model used for semantic caching.
type EmbeddingConfig struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
}

// TelemetryConfig controls tracing and metrics.
type TelemetryConfig struct {
	ServiceName    string  `yaml:"service_name"`
	OTLPEndpoint   string  `yaml:"otlp_endpoint"`
	TraceSampling  float64 `yaml:"trace_sampling"`
	MetricsEnabled bool    `yaml:"metrics_enabled"`
}

// APIKeyConfig defines a client API key, its budget and rate limit.
type APIKeyConfig struct {
	Key  string `yaml:"key"`
	Name string `yaml:"name"`
	// RateLimitRPM caps requests per minute (0 = unlimited).
	RateLimitRPM int `yaml:"rate_limit_rpm"`
	// MonthlyBudgetUSD caps spend per calendar month (0 = unlimited).
	MonthlyBudgetUSD float64 `yaml:"monthly_budget_usd"`
	// AllowedModels restricts callable models (empty = all).
	AllowedModels []string `yaml:"allowed_models"`
}

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// expandEnv replaces ${VAR} and ${VAR:-default} tokens using the process env.
func expandEnv(in []byte) []byte {
	return envPattern.ReplaceAllFunc(in, func(m []byte) []byte {
		groups := envPattern.FindSubmatch(m)
		name := string(groups[1])
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return []byte(v)
		}
		// groups[3] is the default value when the :- form is used.
		return groups[3]
	})
}

// Load reads, expands, parses and validates the config at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw)
}

// Parse expands env vars, decodes YAML, applies defaults and validates.
func Parse(raw []byte) (*Config, error) {
	expanded := expandEnv(raw)
	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Server.AdminAddr == "" {
		c.Server.AdminAddr = ":9090"
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 60 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 0 // 0 = no write timeout, required for long streams
	}
	if c.Server.ShutdownTimeout == 0 {
		c.Server.ShutdownTimeout = 20 * time.Second
	}
	if c.Redis.Backend == "" {
		c.Redis.Backend = "redis"
	}
	if c.Cache.SimilarityThreshold == 0 {
		c.Cache.SimilarityThreshold = 0.92
	}
	if c.Cache.TTL == 0 {
		c.Cache.TTL = 24 * time.Hour
	}
	if c.Cache.MaxCandidates == 0 {
		c.Cache.MaxCandidates = 5
	}
	if c.Cache.Namespace == "" {
		c.Cache.Namespace = "default"
	}
	if c.Router.Strategy == "" {
		c.Router.Strategy = "auto"
	}
	if c.Router.ComplexityThreshold == 0 {
		c.Router.ComplexityThreshold = 0.55
	}
	if c.Router.MaxRetries == 0 {
		c.Router.MaxRetries = 2
	}
	if c.Router.RetryBackoff == 0 {
		c.Router.RetryBackoff = 200 * time.Millisecond
	}
	if c.Embedding.Dimensions == 0 {
		c.Embedding.Dimensions = 1536
	}
	if c.Telemetry.ServiceName == "" {
		c.Telemetry.ServiceName = "relay"
	}
	for i := range c.Providers {
		if c.Providers[i].Timeout == 0 {
			c.Providers[i].Timeout = 120 * time.Second
		}
		if c.Providers[i].Weight == 0 {
			c.Providers[i].Weight = 1
		}
	}
	for i := range c.Models {
		if c.Models[i].Upstream == "" {
			c.Models[i].Upstream = c.Models[i].Name
		}
	}
}

// Validate checks referential integrity of the configuration.
func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("config: at least one provider is required")
	}
	providers := map[string]bool{}
	for _, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("config: provider missing name")
		}
		switch p.Kind {
		case "openai", "anthropic", "local":
		default:
			return fmt.Errorf("config: provider %q has unknown kind %q", p.Name, p.Kind)
		}
		providers[p.Name] = true
	}
	models := map[string]bool{}
	for _, m := range c.Models {
		if m.Name == "" {
			return fmt.Errorf("config: model missing name")
		}
		if !providers[m.Provider] {
			return fmt.Errorf("config: model %q references unknown provider %q", m.Name, m.Provider)
		}
		models[m.Name] = true
	}
	for _, m := range c.Models {
		for _, fb := range m.Fallbacks {
			if !models[fb] {
				return fmt.Errorf("config: model %q has unknown fallback %q", m.Name, fb)
			}
		}
	}
	if c.Router.CheapModel != "" && !models[c.Router.CheapModel] {
		return fmt.Errorf("config: router cheap_model %q is not defined", c.Router.CheapModel)
	}
	if c.Router.StrongModel != "" && !models[c.Router.StrongModel] {
		return fmt.Errorf("config: router strong_model %q is not defined", c.Router.StrongModel)
	}
	return nil
}

// ModelByName returns the model config for name, if defined.
func (c *Config) ModelByName(name string) (ModelConfig, bool) {
	for _, m := range c.Models {
		if m.Name == name {
			return m, true
		}
	}
	return ModelConfig{}, false
}

// ProviderByName returns the provider config for name, if defined.
func (c *Config) ProviderByName(name string) (ProviderConfig, bool) {
	for _, p := range c.Providers {
		if p.Name == name {
			return p, true
		}
	}
	return ProviderConfig{}, false
}
