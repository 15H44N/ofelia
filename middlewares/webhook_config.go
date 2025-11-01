package middlewares

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mcuadros/ofelia/core"
)

const (
	defaultWebhookConfigPath = "/etc/config/middlewares.json"
	defaultTimeout           = 10 * time.Second
	defaultRetryCount        = 0
	defaultRetryBackoff      = 1 * time.Second

	// Webhook types
	WebhookTypeError = "error"
	WebhookTypeInfo  = "info"
	WebhookTypeAll   = "all"
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
	Name        string            `json:"name"`
	Type        string            `json:"type"`   // "error" | "info" | "all" - REQUIRED
	Active      bool              `json:"active"` // defaults to false
	Priority    int               `json:"priority"`
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	Body        interface{}       `json:"body"`
	OnlyOnError bool              `json:"onlyOnError"`
	Timeout     int               `json:"timeout"`
	Retry       *RetryConfig      `json:"retry"`
}

// RetryConfig defines retry behavior for webhooks
type RetryConfig struct {
	Count   int    `json:"count"`
	Backoff string `json:"backoff"`
}

// WebhookRegistry stores loaded webhooks for per-job lookups
type WebhookRegistry struct {
	webhooks map[string]*WebhookDefinition
}

// NewWebhookRegistry creates a new webhook registry
func NewWebhookRegistry() *WebhookRegistry {
	return &WebhookRegistry{
		webhooks: make(map[string]*WebhookDefinition),
	}
}

// Register adds a webhook to the registry
func (r *WebhookRegistry) Register(def WebhookDefinition) {
	r.webhooks[def.Name] = &def
}

// Get retrieves a webhook by name
func (r *WebhookRegistry) Get(name string) (*WebhookDefinition, bool) {
	def, ok := r.webhooks[name]
	return def, ok
}

// GetAll returns all registered webhooks
func (r *WebhookRegistry) GetAll() []*WebhookDefinition {
	all := make([]*WebhookDefinition, 0, len(r.webhooks))
	for _, def := range r.webhooks {
		all = append(all, def)
	}
	return all
}

// validateWebhookType validates the webhook type field
func validateWebhookType(webhookType string) error {
	switch webhookType {
	case WebhookTypeError, WebhookTypeInfo, WebhookTypeAll:
		return nil
	default:
		return fmt.Errorf("invalid webhook type %q, must be one of: %q, %q, %q",
			webhookType, WebhookTypeError, WebhookTypeInfo, WebhookTypeAll)
	}
}

// LoadWebhookMiddlewares loads webhook configurations from a file and returns middlewares and registry
func LoadWebhookMiddlewares(config *WebhookFileConfig, logger core.Logger) ([]core.Middleware, *WebhookRegistry) {
	// Create registry
	registry := NewWebhookRegistry()

	// Determine config file path - check environment variable first, then config, then default
	configPath := os.Getenv("WEBHOOK_CONFIG")
	if configPath == "" {
		configPath = config.WebhookConfigFile
	}
	if configPath == "" {
		configPath = defaultWebhookConfigPath
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Debugf("Webhook config file not found at %q, skipping webhook middleware", configPath)
		return nil, registry
	}

	// Read and parse the config file
	webhookDefs, err := parseWebhookConfigFile(configPath)
	if err != nil {
		logger.Errorf("Failed to parse webhook config file %q: %v", configPath, err)
		return nil, registry
	}

	if len(webhookDefs) == 0 {
		logger.Debugf("No webhooks defined in config file %q", configPath)
		return nil, registry
	}

	// Sort by priority (lower number = higher priority = runs first)
	sort.Slice(webhookDefs, func(i, j int) bool {
		return webhookDefs[i].Priority < webhookDefs[j].Priority
	})

	// Create middlewares from definitions and register them
	middlewares := make([]core.Middleware, 0, len(webhookDefs))
	for _, def := range webhookDefs {
		// Register webhook in registry
		registry.Register(def)

		middleware, err := NewWebhookFromDefinition(def, logger)
		if err != nil {
			logger.Errorf("Failed to create webhook middleware %q: %v", def.Name, err)
			continue
		}
		middlewares = append(middlewares, middleware)
		logger.Noticef("Loaded webhook middleware %q (type: %s, active: %t, priority: %d)",
			def.Name, def.Type, def.Active, def.Priority)
	}

	return middlewares, registry
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

		// Validate type field (REQUIRED)
		if def.Type == "" {
			return nil, fmt.Errorf("webhook %q is missing required 'type' field", def.Name)
		}
		if err := validateWebhookType(def.Type); err != nil {
			return nil, fmt.Errorf("webhook %q has invalid type: %w", def.Name, err)
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
		// Note: Active defaults to false (zero value)
	}

	return config.Webhooks, nil
}

// WebhookConfig is the per-job webhook configuration
type WebhookConfig struct {
	WebhookErrorNames string `gcfg:"webhook-error-names" mapstructure:"webhook-error-names"`
	WebhookInfoNames  string `gcfg:"webhook-info-names" mapstructure:"webhook-info-names"`
}

// NewWebhookFromConfig creates a per-job webhook middleware from config
func NewWebhookFromConfig(c *WebhookConfig, registry *WebhookRegistry, logger core.Logger) (core.Middleware, error) {
	// If config is empty, return nil (no per-job webhooks)
	if IsEmpty(c) {
		return nil, nil
	}

	// Parse error webhook names
	errorNames := parseWebhookNames(c.WebhookErrorNames)
	infoNames := parseWebhookNames(c.WebhookInfoNames)

	// If no webhooks configured, return nil
	if len(errorNames) == 0 && len(infoNames) == 0 {
		return nil, nil
	}

	// Validate and collect error webhooks
	errorWebhooks := make([]*WebhookDefinition, 0, len(errorNames))
	for _, name := range errorNames {
		def, ok := registry.Get(name)
		if !ok {
			return nil, fmt.Errorf("webhook-error-names references unknown webhook %q", name)
		}

		// Validate type
		if def.Type != WebhookTypeError && def.Type != WebhookTypeAll {
			return nil, fmt.Errorf("webhook %q has type %q but is referenced in webhook-error-names (must be %q or %q)",
				name, def.Type, WebhookTypeError, WebhookTypeAll)
		}

		if !def.Active {
			logger.Noticef("Webhook %q is inactive and will not fire", name)
		}

		errorWebhooks = append(errorWebhooks, def)
	}

	// Validate and collect info webhooks
	infoWebhooks := make([]*WebhookDefinition, 0, len(infoNames))
	for _, name := range infoNames {
		def, ok := registry.Get(name)
		if !ok {
			return nil, fmt.Errorf("webhook-info-names references unknown webhook %q", name)
		}

		// Validate type
		if def.Type != WebhookTypeInfo && def.Type != WebhookTypeAll {
			return nil, fmt.Errorf("webhook %q has type %q but is referenced in webhook-info-names (must be %q or %q)",
				name, def.Type, WebhookTypeInfo, WebhookTypeAll)
		}

		if !def.Active {
			logger.Noticef("Webhook %q is inactive and will not fire", name)
		}

		infoWebhooks = append(infoWebhooks, def)
	}

	return &PerJobWebhook{
		errorWebhooks: errorWebhooks,
		infoWebhooks:  infoWebhooks,
		logger:        logger,
	}, nil
}

// parseWebhookNames parses comma-separated or JSON array of webhook names
func parseWebhookNames(namesStr string) []string {
	if namesStr == "" {
		return nil
	}

	// Try parsing as JSON array first
	var names []string
	if err := json.Unmarshal([]byte(namesStr), &names); err == nil {
		return names
	}

	// Fall back to comma-separated
	parts := strings.Split(namesStr, ",")
	names = make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			names = append(names, trimmed)
		}
	}

	return names
}
