package factories

import (
	"fmt"
	"time"

	"github.com/evisdrenova/devgru/internal/provider"
	"github.com/evisdrenova/devgru/internal/provider/openai"
)

// DefaultFactory is the default provider factory
type DefaultFactory struct{}

// NewDefaultFactory creates a new default factory
func NewDefaultFactory() *DefaultFactory {
	return &DefaultFactory{}
}

// CreateProvider creates a provider based on the configuration
func (f *DefaultFactory) CreateProvider(config provider.ProviderConfig) (provider.Provider, error) {
	// Set default timeout if not specified
	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}

	// Set default retries if not specified
	if config.Retries == 0 {
		config.Retries = 3
	}

	switch config.Kind {
	case "openai":
		return openai.NewClient(config)

	// case "anthropic":
	// 	// TODO: Implement Anthropic provider
	// 	return nil, fmt.Errorf("anthropic provider not yet implemented")

	// case "ollama":
	// 	// TODO: Implement Ollama provider
	// 	return nil, fmt.Errorf("ollama provider not yet implemented")

	default:
		return nil, &provider.ProviderError{
			Provider: config.Kind,
			Type:     provider.ErrorTypeValidation,
			Message:  fmt.Sprintf("unsupported provider kind: %s", config.Kind),
		}
	}
}

// SupportedKinds returns the list of supported provider kinds
func (f *DefaultFactory) SupportedKinds() []string {
	return []string{
		"openai",
		// "anthropic", // TODO: Uncomment when implemented
		// "ollama",    // TODO: Uncomment when implemented
	}
}

// ProviderManager manages multiple providers and provides utilities
type ProviderManager struct {
	factory   provider.Factory
	providers map[string]provider.Provider
}

// NewProviderManager creates a new provider manager
func NewProviderManager(factory provider.Factory) *ProviderManager {
	return &ProviderManager{
		factory:   factory,
		providers: make(map[string]provider.Provider),
	}
}

// CreateProviders creates all providers from a config map
func (pm *ProviderManager) CreateProviders(configs map[string]provider.ProviderConfig) error {
	for name, config := range configs {
		provider, err := pm.factory.CreateProvider(config)
		if err != nil {
			return fmt.Errorf("failed to create provider %s: %w", name, err)
		}
		pm.providers[name] = provider
	}
	return nil
}

// GetProvider returns a provider by name
func (pm *ProviderManager) GetProvider(name string) (provider.Provider, error) {
	prov, exists := pm.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return prov, nil
}

// GetAllProviders returns all managed providers
func (pm *ProviderManager) GetAllProviders() map[string]provider.Provider {
	return pm.providers
}

// CloseAll closes all managed providers
func (pm *ProviderManager) CloseAll() error {
	var errors []error

	for name, prov := range pm.providers {
		if err := prov.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close provider %s: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to close some providers: %v", errors)
	}

	return nil
}

// ValidateProvider checks if a provider configuration is valid
func ValidateProvider(config provider.ProviderConfig) error {
	if config.Kind == "" {
		return &provider.ProviderError{
			Provider: "unknown",
			Type:     provider.ErrorTypeValidation,
			Message:  "provider kind is required",
		}
	}

	if config.Model == "" {
		return &provider.ProviderError{
			Provider: config.Kind,
			Type:     provider.ErrorTypeValidation,
			Message:  "model is required",
		}
	}

	switch config.Kind {
	case "openai", "anthropic":
		if config.BaseURL == "" {
			return &provider.ProviderError{
				Provider: config.Kind,
				Type:     provider.ErrorTypeValidation,
				Message:  "base_url is required for " + config.Kind,
			}
		}
		if config.APIKey == "" {
			return &provider.ProviderError{
				Provider: config.Kind,
				Type:     provider.ErrorTypeValidation,
				Message:  "api_key is required for " + config.Kind,
			}
		}

	// case "ollama":
	// 	if config.Host == "" {
	// 		return &provider.ProviderError{
	// 			Provider: config.Kind,
	// 			Type:     provider.ErrorTypeValidation,
	// 			Message:  "host is required for ollama",
	// 		}
	// 	}

	default:
		return &provider.ProviderError{
			Provider: config.Kind,
			Type:     provider.ErrorTypeValidation,
			Message:  "unsupported provider kind: " + config.Kind,
		}
	}

	return nil
}
