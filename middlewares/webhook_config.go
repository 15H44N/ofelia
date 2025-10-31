package middlewares

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/mcuadros/ofelia/core"
)

const (
	defaultWebhookConfigPath = "/etc/config/middlewares.json"
	defaultTimeout           = 10 * time.Second
	defaultRetryCount        = 0
	defaultRetryBackoff      = 1 * time.Second
)

// WebhookFileConfig is the global config that specifies the webhook config file location
type WebhookFileConfig struct {
	WebhookConfigFile string `gcfg:"webhook-config-file" mapstructure:"webhook-config-file"`
}

// WebhooksFile represents the structure of the webhooks configuration JSON file
type WebhooksFile struct {
	Webhooks []WebhookDefinition `json:"webhooks"`
}

// WebhookDefinition defines a single webhook configuration
type WebhookDefinition struct {
	Name        string                 `json:"name"`
	Priority    int                    `json:"priority"`
	URL         string                 `json:"url"`
	Method      string                 `json:"method"`
	Headers     map[string]string      `json:"headers"`
	Body        interface{}            `json:"body"`
	OnlyOnError bool                   `json:"onlyOnError"`
	Timeout     int                    `json:"timeout"`
	Retry       *RetryConfig           `json:"retry"`
}

// RetryConfig defines retry behavior for webhooks
type RetryConfig struct {
	Count   int    `json:"count"`
	Backoff string `json:"backoff"`
}

// LoadWebhookMiddlewares loads webhook configurations from a file and returns middlewares
func LoadWebhookMiddlewares(config *WebhookFileConfig, logger core.Logger) []core.Middleware {
	// Determine config file path
	configPath := config.WebhookConfigFile
	if configPath == "" {
		configPath = defaultWebhookConfigPath
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Debugf("Webhook config file not found at %q, skipping webhook middleware", configPath)
		return nil
	}

	// Read and parse the config file
	webhookDefs, err := parseWebhookConfigFile(configPath)
	if err != nil {
		logger.Errorf("Failed to parse webhook config file %q: %v", configPath, err)
		return nil
	}

	if len(webhookDefs) == 0 {
		logger.Debugf("No webhooks defined in config file %q", configPath)
		return nil
	}

	// Sort by priority (lower number = higher priority = runs first)
	sort.Slice(webhookDefs, func(i, j int) bool {
		return webhookDefs[i].Priority < webhookDefs[j].Priority
	})

	// Create middlewares from definitions
	middlewares := make([]core.Middleware, 0, len(webhookDefs))
	for _, def := range webhookDefs {
		middleware, err := NewWebhookFromDefinition(def, logger)
		if err != nil {
			logger.Errorf("Failed to create webhook middleware %q: %v", def.Name, err)
			continue
		}
		middlewares = append(middlewares, middleware)
		logger.Noticef("Loaded webhook middleware %q (priority: %d)", def.Name, def.Priority)
	}

	return middlewares
}

// parseWebhookConfigFile reads and parses the webhook configuration file
func parseWebhookConfigFile(path string) ([]WebhookDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var config WebhooksFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate webhook definitions
	for i, def := range config.Webhooks {
		if def.URL == "" {
			return nil, fmt.Errorf("webhook at index %d is missing required 'url' field", i)
		}

		// Set defaults
		if def.Method == "" {
			config.Webhooks[i].Method = "POST"
		}
		if def.Timeout == 0 {
			config.Webhooks[i].Timeout = int(defaultTimeout.Seconds())
		}
		if def.Headers == nil {
			config.Webhooks[i].Headers = make(map[string]string)
		}
	}

	return config.Webhooks, nil
}
