// Package compat registers OpenAI-compatible chat providers.
package compat

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/provider"
	openaiadapter "github.com/feimingxliu/ub/internal/provider/openai"
)

func init() {
	provider.Register("openai-compat", NewFromConfig)
}

// NewFromConfig creates an OpenAI-compatible provider from one config entry.
func NewFromConfig(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("openai-compatible provider %q missing base_url", name)
	}
	return openaiadapter.NewCompatibleFromConfig(name, cfg)
}
