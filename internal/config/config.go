package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config represents the complete poly configuration
type Config struct {
	Providers map[string]Provider `koanf:"providers"`
	Workers   []Worker            `koanf:"workers"`
	Judges    []Judge             `koanf:"judges"`
	Consensus Consensus           `koanf:"consensus"`
	Cache     Cache               `koanf:"cache"`
	Logging   Logging             `koanf:"logging"`
	IDE       IDE                 `koanf:"ide`
}

// Provider defines configuration for an LLM provider
type Provider struct {
	Kind    string `koanf:"kind"`     // openai, anthropic, ollama
	Model   string `koanf:"model"`    // gpt-4o-mini, claude-3-sonnet, etc.
	BaseURL string `koanf:"base_url"` // API endpoint
	Host    string `koanf:"host"`     // for ollama
	APIKey  string `koanf:"api_key"`  // will be populated from env vars
}

// Worker represents a configured LLM worker
type Worker struct {
	ID           string  `koanf:"id"`
	Provider     string  `koanf:"provider"`
	Temperature  float64 `koanf:"temperature"`
	MaxTokens    int     `koanf:"max_tokens"`
	SystemPrompt string  `koanf:"system_prompt"`
}

// Judge represents a model that evaluates worker responses
type Judge struct {
	ID           string `koanf:"id"`
	Provider     string `koanf:"provider"`
	SystemPrompt string `koanf:"system_prompt"`
}

// Consensus defines how to reach consensus among workers
type Consensus struct {
	Algorithm string        `koanf:"algorithm"` // majority, score_top1, embedding_cluster, referee
	MinScore  float64       `koanf:"min_score"`
	Timeout   time.Duration `koanf:"timeout"`
}

// Cache configuration
type Cache struct {
	Dir     string `koanf:"dir"`
	Enabled bool   `koanf:"enabled"`
}

// Logging configuration
type Logging struct {
	Level string `koanf:"level"` // debug, info, warn, error
}

// IDE integration configuration
type IDE struct {
	Enable    bool   `koanf:"enable"`
	Transport string `koanf:"transport"` // websocket or stdio
	DiffTool  string `koanf:"diff_tool"` // auto, vscode, or disabled
	Port      int    `koanf:"port"`      // WebSocket port (default: 8123)
}

// Load loads configuration from the specified file path
func Load(configPath string) (*Config, error) {
	k := koanf.New(".")

	// Load from file
	if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("failed to load config file %s: %w", configPath, err)
	}

	// Load environment variables with DEVGRU_ prefix
	if err := k.Load(env.Provider("DEVGRU_", ".", func(s string) string {
		return strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, "DEVGRU_")), "_", ".", -1)
	}), nil); err != nil {
		return nil, fmt.Errorf("failed to load environment variables: %w", err)
	}

	// Unmarshal into struct
	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Post-process and validate
	if err := config.postProcess(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// LoadDefault loads configuration from default locations
func LoadDefault() (*Config, error) {
	locations := []string{
		"devgru.yaml",
		"devgru.yml",
		filepath.Join(os.Getenv("HOME"), ".devgru", "devgru.yaml"),
		filepath.Join(os.Getenv("HOME"), ".devgru", "devgru.yml"),
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return Load(loc)
		}
	}

	return nil, fmt.Errorf("no config file found in default locations: %v", locations)
}

// postProcess handles validation and environment variable substitution
func (c *Config) postProcess() error {
	// Set defaults
	c.setDefaults()

	// Validate required fields
	if err := c.validate(); err != nil {
		return err
	}

	// Inject API keys from environment variables
	c.injectAPIKeys()

	return nil
}

// setDefaults sets sensible defaults for missing configuration
func (c *Config) setDefaults() {
	// Cache defaults
	if c.Cache.Dir == "" {
		homeDir, _ := os.UserHomeDir()
		c.Cache.Dir = filepath.Join(homeDir, ".devgru", "cache")
	}
	if !c.Cache.Enabled {
		c.Cache.Enabled = true
	}

	// Logging defaults
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}

	// Consensus defaults
	if c.Consensus.Algorithm == "" {
		c.Consensus.Algorithm = "majority"
	}
	if c.Consensus.Timeout == 0 {
		c.Consensus.Timeout = 30 * time.Second
	}

	// IDE defaults
	if c.IDE.Transport == "" {
		c.IDE.Transport = "websocket"
	}
	if c.IDE.DiffTool == "" {
		c.IDE.DiffTool = "auto"
	}
	if c.IDE.Port == 0 {
		c.IDE.Port = 8123
	}

	// Worker defaults
	for i := range c.Workers {
		if c.Workers[i].Temperature == 0 {
			c.Workers[i].Temperature = 0.7
		}
		if c.Workers[i].MaxTokens == 0 {
			c.Workers[i].MaxTokens = 2048
		}
	}
}

// validate performs configuration validation
func (c *Config) validate() error {
	// Validate providers exist
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured")
	}

	// Validate workers
	if len(c.Workers) == 0 {
		return fmt.Errorf("at least one worker must be configured")
	}

	for _, worker := range c.Workers {
		if worker.ID == "" {
			return fmt.Errorf("worker ID cannot be empty")
		}
		if worker.Provider == "" {
			return fmt.Errorf("worker %s must specify a provider", worker.ID)
		}
		if _, exists := c.Providers[worker.Provider]; !exists {
			return fmt.Errorf("worker %s references unknown provider %s", worker.ID, worker.Provider)
		}
		if worker.Temperature < 0 || worker.Temperature > 2 {
			return fmt.Errorf("worker %s temperature must be between 0 and 2", worker.ID)
		}
	}

	// Validate judges (if any)
	for _, judge := range c.Judges {
		if judge.ID == "" {
			return fmt.Errorf("judge ID cannot be empty")
		}
		if judge.Provider == "" {
			return fmt.Errorf("judge %s must specify a provider", judge.ID)
		}
		if _, exists := c.Providers[judge.Provider]; !exists {
			return fmt.Errorf("judge %s references unknown provider %s", judge.ID, judge.Provider)
		}
	}

	// Validate provider configurations
	for name, provider := range c.Providers {
		if provider.Kind == "" {
			return fmt.Errorf("provider %s must specify a kind", name)
		}
		if provider.Model == "" {
			return fmt.Errorf("provider %s must specify a model", name)
		}

		switch provider.Kind {
		case "openai", "anthropic":
			if provider.BaseURL == "" {
				return fmt.Errorf("provider %s of kind %s must specify base_url", name, provider.Kind)
			}
		case "ollama":
			if provider.Host == "" {
				return fmt.Errorf("provider %s of kind ollama must specify host", name)
			}
		default:
			return fmt.Errorf("unsupported provider kind: %s", provider.Kind)
		}
	}

	// Validate consensus algorithm
	validAlgorithms := []string{"majority", "score_top1", "embedding_cluster", "referee"}
	valid := false
	for _, alg := range validAlgorithms {
		if c.Consensus.Algorithm == alg {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid consensus algorithm: %s (valid: %v)", c.Consensus.Algorithm, validAlgorithms)
	}

	return nil
}

// injectAPIKeys populates API keys from environment variables
func (c *Config) injectAPIKeys() {
	for name, provider := range c.Providers {
		switch provider.Kind {
		case "openai":
			if key := os.Getenv("OPENAI_API_KEY"); key != "" {
				provider.APIKey = key
				c.Providers[name] = provider
			}
		case "anthropic":
			if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
				provider.APIKey = key
				c.Providers[name] = provider
			}
		}
	}
}

// GetWorkerByID returns a worker by its ID
func (c *Config) GetWorkerByID(id string) (*Worker, error) {
	for _, worker := range c.Workers {
		if worker.ID == id {
			return &worker, nil
		}
	}
	return nil, fmt.Errorf("worker with ID %s not found", id)
}

// GetJudgeByID returns a judge by its ID
func (c *Config) GetJudgeByID(id string) (*Judge, error) {
	for _, judge := range c.Judges {
		if judge.ID == id {
			return &judge, nil
		}
	}
	return nil, fmt.Errorf("judge with ID %s not found", id)
}

// GetProvider returns a provider by name
func (c *Config) GetProvider(name string) (*Provider, error) {
	provider, exists := c.Providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return &provider, nil
}
