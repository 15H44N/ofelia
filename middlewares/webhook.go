package middlewares

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mcuadros/ofelia/core"
)

// Webhook middleware sends HTTP requests to configured webhooks after job execution
type Webhook struct {
	name         string
	webhookType  string // "error" | "info" | "all"
	active       bool
	url          string
	method       string
	headers      map[string]string
	body         interface{}
	onlyOnError  bool
	timeout      time.Duration
	retryCount   int
	retryBackoff time.Duration

	logger core.Logger
	client *http.Client
}

// NewWebhookFromDefinition creates a webhook middleware from a definition
func NewWebhookFromDefinition(def WebhookDefinition, logger core.Logger) (core.Middleware, error) {
	// Parse timeout
	timeout := time.Duration(def.Timeout) * time.Second

	// Parse retry config
	retryCount := defaultRetryCount
	retryBackoff := defaultRetryBackoff
	if def.Retry != nil {
		retryCount = def.Retry.Count
		if def.Retry.Backoff != "" {
			duration, err := time.ParseDuration(def.Retry.Backoff)
			if err != nil {
				return nil, fmt.Errorf("invalid retry backoff duration %q: %w", def.Retry.Backoff, err)
			}
			retryBackoff = duration
		}
	}

	webhook := &Webhook{
		name:         def.Name,
		webhookType:  def.Type,
		active:       def.Active,
		url:          def.URL,
		method:       def.Method,
		headers:      def.Headers,
		body:         def.Body,
		onlyOnError:  def.OnlyOnError,
		timeout:      timeout,
		retryCount:   retryCount,
		retryBackoff: retryBackoff,
		logger:       logger,
		client: &http.Client{
			Timeout: timeout,
		},
	}

	return webhook, nil
}

// ContinueOnStop returns true because we want to report final status
func (w *Webhook) ContinueOnStop() bool {
	return true
}

// Run sends the webhook after job execution
func (w *Webhook) Run(ctx *core.Context) error {
	// Execute the job first
	err := ctx.Next()
	ctx.Stop(err)

	// Check if webhook is active
	if !w.active {
		ctx.Logger.Debugf("Webhook %q skipped (inactive)", w.name)
		return err
	}

	// Check if webhook type matches job result
	shouldSend := false
	if w.webhookType == WebhookTypeAll {
		shouldSend = true
	} else if ctx.Execution.Failed && w.webhookType == WebhookTypeError {
		shouldSend = true
	} else if !ctx.Execution.Failed && w.webhookType == WebhookTypeInfo {
		shouldSend = true
	}

	if !shouldSend {
		ctx.Logger.Debugf("Webhook %q skipped (type mismatch: webhook type=%s, job failed=%t)",
			w.name, w.webhookType, ctx.Execution.Failed)
		return err
	}

	// Also check the legacy onlyOnError flag for backward compatibility
	if w.onlyOnError && !ctx.Execution.Failed {
		ctx.Logger.Debugf("Webhook %q skipped (onlyOnError=true but job succeeded)", w.name)
		return err
	}

	// Send webhook asynchronously to avoid blocking
	go w.sendWebhook(ctx)

	return err
}

// sendWebhook sends the HTTP request to the configured webhook
func (w *Webhook) sendWebhook(ctx *core.Context) {
	// Build template data
	templateData := buildTemplateData(ctx)

	// Execute templates for URL
	url, err := executeTemplate(w.url, templateData)
	if err != nil {
		ctx.Logger.Errorf("Webhook %q: failed to execute URL template: %v", w.name, err)
		return
	}

	// Execute templates for body
	var bodyBytes []byte
	if w.body != nil {
		bodyBytes, err = executeTemplateForBody(w.body, templateData)
		if err != nil {
			ctx.Logger.Errorf("Webhook %q: failed to execute body template: %v", w.name, err)
			return
		}
	}

	// Execute templates for headers
	headers := make(map[string]string)
	for key, value := range w.headers {
		templatedValue, err := executeTemplate(value, templateData)
		if err != nil {
			ctx.Logger.Errorf("Webhook %q: failed to execute header template for %q: %v", w.name, key, err)
			return
		}
		headers[key] = templatedValue
	}

	// Send with retry logic
	err = w.sendWithRetry(url, headers, bodyBytes)
	if err != nil {
		ctx.Logger.Errorf("Webhook %q: failed after %d attempts: %v", w.name, w.retryCount+1, err)
	} else {
		ctx.Logger.Debugf("Webhook %q: sent successfully to %s", w.name, url)
	}
}

// sendWithRetry sends the HTTP request with exponential backoff retry
func (w *Webhook) sendWithRetry(url string, headers map[string]string, body []byte) error {
	var lastErr error
	backoff := w.retryBackoff

	for attempt := 0; attempt <= w.retryCount; attempt++ {
		if attempt > 0 {
			w.logger.Debugf("Webhook %q: retry attempt %d/%d after %v", w.name, attempt, w.retryCount, backoff)
			time.Sleep(backoff)
			backoff *= 2 // Exponential backoff
		}

		err := w.sendRequest(url, headers, body)
		if err == nil {
			return nil
		}

		lastErr = err
	}

	return lastErr
}

// sendRequest sends a single HTTP request
func (w *Webhook) sendRequest(url string, headers map[string]string, body []byte) error {
	// Create request
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(w.method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Send request
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read response body for error details
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("non-2xx status code: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}
