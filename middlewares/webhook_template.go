package middlewares

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/mcuadros/ofelia/core"
)

// WebhookTemplateData contains all data available to webhook templates
type WebhookTemplateData struct {
	// Job information
	JobName     string
	JobSchedule string
	JobCommand  string

	// Execution metadata
	ExecutionID string
	StartTime   time.Time
	EndTime     time.Time
	Duration    string

	// Status flags
	IsRunning bool
	Failed    bool
	Skipped   bool
	Success   bool

	// Error details
	Error    string
	HasError bool

	// Output streams
	Stdout string
	Stderr string

	// Metadata
	Hostname  string
	Timestamp string
}

// buildTemplateData creates template data from execution context
func buildTemplateData(ctx *core.Context) *WebhookTemplateData {
	hostname, _ := os.Hostname()

	data := &WebhookTemplateData{
		// Job info
		JobName:     ctx.Job.GetName(),
		JobSchedule: ctx.Job.GetSchedule(),
		JobCommand:  ctx.Job.GetCommand(),

		// Execution metadata
		ExecutionID: ctx.Execution.ID,
		StartTime:   ctx.Execution.Date,
		EndTime:     ctx.Execution.Date.Add(ctx.Execution.Duration),
		Duration:    ctx.Execution.Duration.String(),

		// Status
		IsRunning: ctx.Execution.IsRunning,
		Failed:    ctx.Execution.Failed,
		Skipped:   ctx.Execution.Skipped,
		Success:   !ctx.Execution.Failed && !ctx.Execution.Skipped,

		// Metadata
		Hostname:  hostname,
		Timestamp: ctx.Execution.Date.Format(time.RFC3339),
	}

	// Error handling
	if ctx.Execution.Error != nil {
		data.Error = ctx.Execution.Error.Error()
		data.HasError = true
	}

	// Output streams
	if ctx.Execution.OutputStream != nil {
		data.Stdout = ctx.Execution.OutputStream.String()
	}
	if ctx.Execution.ErrorStream != nil {
		data.Stderr = ctx.Execution.ErrorStream.String()
	}

	return data
}

// webhookFuncMap provides template helper functions
var webhookFuncMap = template.FuncMap{
	// String manipulation
	"upper":    strings.ToUpper,
	"lower":    strings.ToLower,
	"trim":     strings.TrimSpace,
	"truncate": truncateString,

	// JSON encoding
	"json":       jsonEncode,
	"jsonEscape": jsonEscapeString,

	// Time formatting
	"formatTime": formatTime,
	"unixTime":   unixTimestamp,

	// Conditionals
	"default": defaultValue,

	// Status helpers
	"statusCode": statusCode,
	"colorHex":   statusColorHex,
}

// truncateString truncates a string to a maximum length
func truncateString(maxLen int, s string) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// jsonEncode encodes a value as JSON
func jsonEncode(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// jsonEscapeString escapes a string for safe inclusion in JSON
func jsonEscapeString(s string) string {
	data, _ := json.Marshal(s)
	// Remove surrounding quotes added by json.Marshal
	result := string(data)
	if len(result) >= 2 {
		return result[1 : len(result)-1]
	}
	return result
}

// formatTime formats a time value with a custom layout
func formatTime(layout string, t time.Time) string {
	return t.Format(layout)
}

// unixTimestamp returns the Unix timestamp for a time value
func unixTimestamp(t time.Time) int64 {
	return t.Unix()
}

// defaultValue returns the value if it's not empty, otherwise returns the default
func defaultValue(defaultVal, value string) string {
	if value == "" {
		return defaultVal
	}
	return value
}

// statusCode returns a numeric status code (0=success, 1=failure, 2=skipped)
func statusCode(data *WebhookTemplateData) int {
	if data.Skipped {
		return 2
	}
	if data.Failed {
		return 1
	}
	return 0
}

// statusColorHex returns a hex color code based on status
func statusColorHex(data *WebhookTemplateData) string {
	if data.Skipped {
		return "#FFA500" // Orange
	}
	if data.Failed {
		return "#FF0000" // Red
	}
	return "#00FF00" // Green
}

// executeTemplate executes a template string with the given data
func executeTemplate(templateStr string, data *WebhookTemplateData) (string, error) {
	tmpl, err := template.New("webhook").Funcs(webhookFuncMap).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}

	return buf.String(), nil
}

// executeTemplateForBody handles both string and object body templates
func executeTemplateForBody(body interface{}, data *WebhookTemplateData) ([]byte, error) {
	switch v := body.(type) {
	case string:
		// Simple string template
		result, err := executeTemplate(v, data)
		if err != nil {
			return nil, err
		}
		return []byte(result), nil

	case map[string]interface{}, []interface{}:
		// JSON object/array - need to marshal, then template, then parse back
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal JSON body: %w", err)
		}

		// Execute template on the JSON string
		result, err := executeTemplate(string(jsonBytes), data)
		if err != nil {
			return nil, err
		}

		// Validate it's still valid JSON
		var test interface{}
		if err := json.Unmarshal([]byte(result), &test); err != nil {
			return nil, fmt.Errorf("template resulted in invalid JSON: %w", err)
		}

		return []byte(result), nil

	default:
		return nil, fmt.Errorf("unsupported body type: %T", v)
	}
}
