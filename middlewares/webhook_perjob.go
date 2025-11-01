package middlewares

import (
	"github.com/mcuadros/ofelia/core"
)

// PerJobWebhook is a middleware that sends webhooks based on per-job configuration
type PerJobWebhook struct {
	errorWebhooks []*WebhookDefinition
	infoWebhooks  []*WebhookDefinition
	logger        core.Logger
}

// ContinueOnStop returns true because we want to report final status
func (w *PerJobWebhook) ContinueOnStop() bool {
	return true
}

// Run sends the configured webhooks based on job success/failure
func (w *PerJobWebhook) Run(ctx *core.Context) error {
	// Execute the job first
	err := ctx.Next()
	ctx.Stop(err)

	// Determine which webhooks to fire based on job result
	var webhooks []*WebhookDefinition
	if ctx.Execution.Failed {
		webhooks = w.errorWebhooks
	} else {
		webhooks = w.infoWebhooks
	}

	// Fire webhooks
	for _, def := range webhooks {
		if !def.Active {
			ctx.Logger.Debugf("Webhook %q skipped (inactive)", def.Name)
			continue
		}

		// Create and send webhook
		go w.sendWebhook(ctx, def)
	}

	return err
}

// sendWebhook sends a single webhook based on the definition
func (w *PerJobWebhook) sendWebhook(ctx *core.Context, def *WebhookDefinition) {
	// Create a webhook instance from the definition
	webhook, err := NewWebhookFromDefinition(*def, w.logger)
	if err != nil {
		ctx.Logger.Errorf("Per-job webhook %q: failed to create webhook: %v", def.Name, err)
		return
	}

	// The webhook.Run will handle template execution and HTTP sending
	// We need to create a mock context that won't execute Next() again
	// Instead, we'll directly call the sendWebhook method from the Webhook struct

	// Build template data
	templateData := buildTemplateData(ctx)

	// Execute templates for URL
	url, err := executeTemplate(def.URL, templateData)
	if err != nil {
		ctx.Logger.Errorf("Per-job webhook %q: failed to execute URL template: %v", def.Name, err)
		return
	}

	// Execute templates for body
	var bodyBytes []byte
	if def.Body != nil {
		bodyBytes, err = executeTemplateForBody(def.Body, templateData)
		if err != nil {
			ctx.Logger.Errorf("Per-job webhook %q: failed to execute body template: %v", def.Name, err)
			return
		}
	}

	// Execute templates for headers
	headers := make(map[string]string)
	for key, value := range def.Headers {
		templatedValue, err := executeTemplate(value, templateData)
		if err != nil {
			ctx.Logger.Errorf("Per-job webhook %q: failed to execute header template for %q: %v", def.Name, key, err)
			return
		}
		headers[key] = templatedValue
	}

	// Send with retry logic using the webhook's sendWithRetry
	if wh, ok := webhook.(*Webhook); ok {
		err = wh.sendWithRetry(url, headers, bodyBytes)
		if err != nil {
			ctx.Logger.Errorf("Per-job webhook %q: failed after retries: %v", def.Name, err)
		} else {
			ctx.Logger.Debugf("Per-job webhook %q: sent successfully to %s", def.Name, url)
		}
	}
}
