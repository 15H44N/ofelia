# Webhook Middleware

The webhook middleware allows you to send HTTP notifications to any service after job execution. It supports templated requests, multiple webhooks, retry logic, and works seamlessly with Docker Compose inline configs.

## Table of Contents

- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Template Variables](#template-variables)
- [Template Helper Functions](#template-helper-functions)
- [Examples](#examples)
- [Docker Compose Setup](#docker-compose-setup)
- [Troubleshooting](#troubleshooting)

## Quick Start

### 1. Create a webhook configuration file

Create a file `webhooks.json`:

```json
{
  "webhooks": [
    {
      "name": "ntfy",
      "url": "https://ntfy.sh/my-topic",
      "method": "POST",
      "body": "Job {{.JobName}} {{if .Failed}}failed{{else}}completed{{end}} in {{.Duration}}"
    }
  ]
}
```

### 2. Configure Ofelia to use the webhook file

In your `ofelia.ini`:

```ini
[global]
webhook-config-file = /config/webhooks.json
```

If you omit `webhook-config-file`, it defaults to `/etc/config/middlewares.json`.

### 3. Done!

Ofelia will now send webhooks after every job execution.

## Configuration

### Webhook Configuration File Structure

```json
{
  "webhooks": [
    {
      "name": "webhook-name",
      "priority": 100,
      "url": "https://example.com/webhook",
      "method": "POST",
      "headers": {
        "Header-Name": "header-value"
      },
      "body": "string or JSON object",
      "onlyOnError": false,
      "timeout": 10,
      "retry": {
        "count": 3,
        "backoff": "1s"
      }
    }
  ]
}
```

### Field Reference

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | No | - | Identifier for logging purposes |
| `priority` | number | No | 0 | Execution order (lower runs first) |
| `url` | string | **Yes** | - | HTTP endpoint (supports templates) |
| `method` | string | No | `POST` | HTTP method (GET, POST, PUT, etc.) |
| `headers` | object | No | `{}` | Custom headers (values support templates) |
| `body` | string or object | No | - | Request body (supports templates) |
| `onlyOnError` | boolean | No | `false` | Send webhook only when job fails |
| `timeout` | number | No | `10` | HTTP request timeout in seconds |
| `retry.count` | number | No | `0` | Number of retry attempts |
| `retry.backoff` | string | No | `1s` | Initial backoff duration (e.g., "1s", "500ms") |

### Multiple Webhooks

You can define multiple webhooks in the same file. They'll execute in `priority` order (lower numbers first):

```json
{
  "webhooks": [
    {
      "name": "critical-alerts",
      "priority": 1,
      "url": "https://pager.example.com/alert"
    },
    {
      "name": "logging",
      "priority": 100,
      "url": "https://logs.example.com/events"
    }
  ]
}
```

## Template Variables

All webhook fields (`url`, `headers`, `body`) support Go templates with access to these variables:

| Variable | Type | Description | Example |
|----------|------|-------------|---------|
| `.JobName` | string | Name of the job | `"backup-job"` |
| `.JobSchedule` | string | Cron schedule | `"@daily"` or `"0 2 * * *"` |
| `.JobCommand` | string | Command that was executed | `"/scripts/backup.sh"` |
| `.ExecutionID` | string | Unique execution identifier | `"abc123..."` |
| `.StartTime` | time.Time | Job start time | `2024-01-15 14:30:00` |
| `.EndTime` | time.Time | Job end time | `2024-01-15 14:31:23` |
| `.Duration` | string | Human-readable duration | `"1m23s"` |
| `.IsRunning` | bool | Whether job is still running | `false` |
| `.Failed` | bool | Whether job failed | `false` |
| `.Skipped` | bool | Whether job was skipped | `false` |
| `.Success` | bool | Derived: `!Failed && !Skipped` | `true` |
| `.Error` | string | Error message if failed | `"command not found"` |
| `.HasError` | bool | Whether an error occurred | `false` |
| `.Stdout` | string | Standard output | `"Backup completed"` |
| `.Stderr` | string | Standard error | `""` |
| `.Hostname` | string | Host running Ofelia | `"server-01"` |
| `.Timestamp` | string | ISO8601 formatted time | `"2024-01-15T14:30:00Z"` |

### Template Syntax

```
Job {{.JobName}} completed in {{.Duration}}
```

#### Conditionals

```
{{if .Failed}}FAILED{{else}}SUCCESS{{end}}
```

```
{{if .Failed}}
  Status: Failed
  Error: {{.Error}}
{{else if .Skipped}}
  Status: Skipped
{{else}}
  Status: Success
{{end}}
```

## Template Helper Functions

### String Manipulation

| Function | Description | Example |
|----------|-------------|---------|
| `upper` | Convert to uppercase | `{{.JobName \| upper}}` → `"BACKUP-JOB"` |
| `lower` | Convert to lowercase | `{{.JobName \| lower}}` → `"backup-job"` |
| `trim` | Trim whitespace | `{{.Stdout \| trim}}` |
| `truncate N` | Truncate to N characters | `{{.Stdout \| truncate 100}}` |

### JSON Encoding

| Function | Description | Example |
|----------|-------------|---------|
| `json` | Encode as JSON | `{{.Stdout \| json}}` → `"\"output\""` |
| `jsonEscape` | Escape JSON special chars | `{{.Error \| jsonEscape}}` |

### Time Formatting

| Function | Description | Example |
|----------|-------------|---------|
| `formatTime LAYOUT` | Custom time format | `{{formatTime "2006-01-02" .StartTime}}` |
| `unixTime` | Unix timestamp | `{{unixTime .StartTime}}` → `1705329000` |

### Conditionals & Defaults

| Function | Description | Example |
|----------|-------------|---------|
| `default` | Provide default value | `{{.Error \| default "No error"}}` |

### Status Helpers

| Function | Description | Example |
|----------|-------------|---------|
| `statusCode` | Numeric status code | `{{statusCode .}}` → `0` (success), `1` (failure), `2` (skipped) |
| `colorHex` | Hex color for status | `{{colorHex .}}` → `"#00FF00"` (green for success) |

### Using Helper Functions

```
Body: {{.Stdout | truncate 500}}
Error: {{.Error | default "No error"}}
Time: {{formatTime "2006-01-02 15:04:05" .StartTime}}
```

## Examples

### ntfy Push Notifications

```json
{
  "name": "ntfy",
  "url": "https://ntfy.sh/my-ofelia-alerts",
  "method": "POST",
  "headers": {
    "Title": "Ofelia Alert",
    "Tags": "{{if .Failed}}warning,x{{else}}white_check_mark{{end}}",
    "Priority": "{{if .Failed}}high{{else}}default{{end}}"
  },
  "body": "Job \"{{.JobName}}\" {{if .Failed}}FAILED{{else}}completed{{end}} in {{.Duration}}{{if .Error}}\nError: {{.Error}}{{end}}"
}
```

### Discord Webhook

```json
{
  "name": "discord",
  "url": "https://discord.com/api/webhooks/YOUR_ID/YOUR_TOKEN",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "content": "Job **{{.JobName}}** {{if .Failed}}:x: failed{{else}}:white_check_mark: succeeded{{end}}",
    "embeds": [{
      "color": "{{if .Failed}}15158332{{else}}3066993{{end}}",
      "fields": [
        {
          "name": "Duration",
          "value": "{{.Duration}}",
          "inline": true
        },
        {
          "name": "Host",
          "value": "{{.Hostname}}",
          "inline": true
        }
      ],
      "timestamp": "{{.Timestamp}}"
    }]
  }
}
```

### Slack Webhook

```json
{
  "name": "slack",
  "url": "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "text": "Job *{{.JobName}}* {{if .Failed}}failed{{else}}completed{{end}}",
    "attachments": [{
      "color": "{{if .Failed}}danger{{else}}good{{end}}",
      "fields": [
        {
          "title": "Duration",
          "value": "{{.Duration}}",
          "short": true
        },
        {
          "title": "Command",
          "value": "`{{.JobCommand}}`",
          "short": false
        }
      ]
    }]
  }
}
```

### Telegram Bot

```json
{
  "name": "telegram",
  "url": "https://api.telegram.org/botYOUR_BOT_TOKEN/sendMessage",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "chat_id": "YOUR_CHAT_ID",
    "text": "{{if .Failed}}❌{{else}}✅{{end}} *{{.JobName}}*\n\nStatus: {{if .Failed}}Failed{{else}}Success{{end}}\nDuration: {{.Duration}}",
    "parse_mode": "Markdown"
  }
}
```

### Healthchecks.io Integration

```json
{
  "name": "healthchecks",
  "url": "https://hc-ping.com/YOUR-UUID-HERE{{if .Failed}}/fail{{end}}",
  "method": "POST",
  "body": "{{.Stdout}}"
}
```

### Custom Monitoring API

```json
{
  "name": "monitoring",
  "url": "https://monitoring.example.com/api/events",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json",
    "Authorization": "Bearer YOUR_API_TOKEN"
  },
  "body": {
    "source": "ofelia",
    "job": "{{.JobName}}",
    "status": "{{if .Failed}}failed{{else}}success{{end}}",
    "duration": "{{.Duration}}",
    "timestamp": "{{.Timestamp}}",
    "error": "{{.Error | default \"\"}}"
  },
  "retry": {
    "count": 5,
    "backoff": "2s"
  }
}
```

## Docker Compose Setup

### Using Docker Compose Inline Configs (Recommended)

```yaml
version: '3.8'

configs:
  ofelia_webhooks:
    content: |
      {
        "webhooks": [
          {
            "name": "ntfy",
            "url": "https://ntfy.sh/my-topic",
            "method": "POST",
            "body": "Job {{.JobName}} completed in {{.Duration}}"
          }
        ]
      }

services:
  ofelia:
    image: mcuadros/ofelia:latest
    command: daemon --docker
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    configs:
      - source: ofelia_webhooks
        target: /config/webhooks.json
    environment:
      - WEBHOOK_CONFIG_FILE=/config/webhooks.json

  app:
    image: alpine:3.22
    labels:
      ofelia.enabled: "true"
      ofelia.job-exec.test.schedule: "@every 5m"
      ofelia.job-exec.test.command: "echo 'Hello World'"
```

### Using Volume Mounts

```yaml
version: '3.8'

services:
  ofelia:
    image: mcuadros/ofelia:latest
    command: daemon --docker
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./webhooks.json:/config/webhooks.json:ro
    environment:
      - WEBHOOK_CONFIG_FILE=/config/webhooks.json
```

### INI Configuration

Create `ofelia.ini`:

```ini
[global]
webhook-config-file = /config/webhooks.json

[job-local "my-local-job"]
schedule = @every 1h
command = /scripts/backup.sh
```

## Troubleshooting

### Webhook not firing

1. **Check if config file exists:**
   ```bash
   docker exec ofelia ls -la /config/webhooks.json
   ```

2. **Check Ofelia logs:**
   ```bash
   docker logs ofelia | grep -i webhook
   ```

3. **Verify JSON syntax:**
   ```bash
   cat webhooks.json | jq .
   ```

### Template errors

- **Error: "template parse error"**
  - Check that all `{{` have matching `}}`
  - Ensure template syntax is valid Go template syntax

- **Error: "template execution error"**
  - Verify you're using correct variable names (case-sensitive)
  - Check that helper functions are spelled correctly

### HTTP errors

- **"non-2xx status code"**: The webhook endpoint rejected the request
  - Check webhook URL is correct
  - Verify authentication tokens/headers
  - Review webhook service's API documentation

- **"request failed: connection refused"**: Can't connect to webhook endpoint
  - Check URL is accessible from the Ofelia container
  - Verify firewall rules

### Performance considerations

- Webhooks are sent **asynchronously** and don't block job execution
- Failed webhooks retry with exponential backoff
- Set appropriate `timeout` values to avoid hanging connections
- Use `onlyOnError: true` for error-specific notifications to reduce noise

### Debug mode

Enable debug logging to see webhook details:

```yaml
services:
  ofelia:
    image: mcuadros/ofelia:latest
    command: daemon --docker --debug
```

## Advanced Topics

### Environment Variables in Templates

Currently, environment variables are not directly accessible in templates. To use secrets:

1. **Use Docker secrets or environment variables in the config file path**
2. **Or use a init script to template the webhook file before starting Ofelia**

### Dynamic Webhook URLs

You can template the webhook URL itself:

```json
{
  "url": "https://api.example.com/{{.JobName}}/status"
}
```

### Conditional Webhooks

Use `onlyOnError` or implement conditional logic in your webhook endpoint:

```json
{
  "onlyOnError": true,
  "url": "https://pager.example.com/alert"
}
```

### Rate Limiting

If sending many webhooks, consider:

- Using `priority` to sequence webhooks
- Setting appropriate `timeout` values
- Using webhook services with rate limiting (ntfy, etc.)

## Migration from Slack Middleware

If you're currently using the built-in Slack middleware:

**Old configuration:**
```ini
[global]
slack-webhook = https://hooks.slack.com/services/XXX
slack-only-on-error = true
```

**New configuration:**

Create `webhooks.json`:
```json
{
  "webhooks": [{
    "name": "slack",
    "url": "https://hooks.slack.com/services/XXX",
    "method": "POST",
    "headers": {"Content-Type": "application/json"},
    "body": {
      "text": "Job *{{.JobName}}* {{if .Failed}}failed{{else}}completed{{end}}"
    },
    "onlyOnError": true
  }]
}
```

In `ofelia.ini`:
```ini
[global]
webhook-config-file = /config/webhooks.json
```

## Support

- **GitHub Issues**: https://github.com/mcuadros/ofelia/issues
- **Examples**: See `/examples` directory in the repository
