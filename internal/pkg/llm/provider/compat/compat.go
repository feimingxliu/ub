// Package compat registers OpenAI-compatible chat providers.
package compat

import (
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	openaiadapter "github.com/feimingxliu/ub/internal/pkg/llm/provider/openai"
)

func init() {
	provider.Register("openai-compat", NewFromConfig)
}

// NewFromConfig creates an OpenAI-compatible provider from one config entry.
func NewFromConfig(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	return openaiadapter.NewCompatibleFromConfig(name, cfg)
}
