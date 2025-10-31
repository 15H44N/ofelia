package middlewares

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	. "gopkg.in/check.v1"
)

type SuiteWebhook struct {
	BaseSuite
}

var _ = Suite(&SuiteWebhook{})

// Test template functions
func (s *SuiteWebhook) TestTemplateHelpers(c *C) {
	// Test truncate
	c.Assert(truncateString(5, "hello world"), Equals, "he...")
	c.Assert(truncateString(20, "short"), Equals, "short")

	// Test jsonEscape
	c.Assert(jsonEscapeString("hello\"world"), Equals, "hello\\\"world")
	c.Assert(jsonEscapeString("line1\nline2"), Equals, "line1\\nline2")

	// Test defaultValue
	c.Assert(defaultValue("fallback", ""), Equals, "fallback")
	c.Assert(defaultValue("fallback", "value"), Equals, "value")
}

// Test simple text webhook
func (s *SuiteWebhook) TestSimpleTextWebhook(c *C) {
	received := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	def := WebhookDefinition{
		Name:    "test",
		URL:     ts.URL,
		Method:  "POST",
		Body:    "Job {{.JobName}} completed in {{.Duration}}",
		Timeout: 5,
	}

	webhook, err := NewWebhookFromDefinition(def, &TestLogger{})
	c.Assert(err, IsNil)
	c.Assert(webhook, NotNil)

	err = webhook.Run(s.ctx)
	c.Assert(err, IsNil)

	// Wait a bit for async webhook
	time.Sleep(100 * time.Millisecond)

	select {
	case body := <-received:
		c.Assert(body, Matches, "Job .* completed in .*")
	case <-time.After(2 * time.Second):
		c.Fatal("webhook not received")
	}
}

// Test JSON webhook
func (s *SuiteWebhook) TestJSONWebhook(c *C) {
	received := make(chan map[string]interface{}, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		received <- data
		w.WriteHeader(200)
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	def := WebhookDefinition{
		Name:   "test",
		URL:    ts.URL,
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]interface{}{
			"job":    "{{.JobName}}",
			"status": "{{if .Failed}}failed{{else}}success{{end}}",
		},
		Timeout: 5,
	}

	webhook, err := NewWebhookFromDefinition(def, &TestLogger{})
	c.Assert(err, IsNil)

	err = webhook.Run(s.ctx)
	c.Assert(err, IsNil)

	// Wait for async webhook
	time.Sleep(100 * time.Millisecond)

	select {
	case data := <-received:
		c.Assert(data["status"], Equals, "success")
	case <-time.After(2 * time.Second):
		c.Fatal("webhook not received")
	}
}

// Test onlyOnError flag
func (s *SuiteWebhook) TestOnlyOnError(c *C) {
	called := false
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		called = true
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer ts.Close()

	// Test 1: Success with onlyOnError=true should NOT send
	s.ctx.Start()
	s.ctx.Stop(nil)

	def := WebhookDefinition{
		Name:        "test",
		URL:         ts.URL,
		Method:      "POST",
		Body:        "test",
		OnlyOnError: true,
		Timeout:     5,
	}

	webhook, err := NewWebhookFromDefinition(def, &TestLogger{})
	c.Assert(err, IsNil)

	err = webhook.Run(s.ctx)
	c.Assert(err, IsNil)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	c.Assert(called, Equals, false)
	mu.Unlock()

	// Test 2: Failure with onlyOnError=true SHOULD send
	called = false

	// Create a new context with middleware chain that will fail
	s.SetUpTest(c)
	s.ctx.Start()
	testErr := errors.New("test error")
	s.ctx.Execution.Failed = true
	s.ctx.Execution.Error = testErr
	s.ctx.Stop(testErr)

	webhook2, _ := NewWebhookFromDefinition(def, &TestLogger{})
	err = webhook2.Run(s.ctx)
	// Middleware should pass through the error from Next()
	c.Assert(err, IsNil)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	c.Assert(called, Equals, true)
	mu.Unlock()
}

// Test templated URL
func (s *SuiteWebhook) TestTemplatedURL(c *C) {
	received := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.URL.Path
		w.WriteHeader(200)
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	def := WebhookDefinition{
		Name:    "test",
		URL:     ts.URL + "/{{if .Failed}}fail{{else}}success{{end}}",
		Method:  "GET",
		Timeout: 5,
	}

	webhook, err := NewWebhookFromDefinition(def, &TestLogger{})
	c.Assert(err, IsNil)

	err = webhook.Run(s.ctx)
	c.Assert(err, IsNil)

	time.Sleep(100 * time.Millisecond)

	select {
	case path := <-received:
		c.Assert(path, Equals, "/success")
	case <-time.After(2 * time.Second):
		c.Fatal("webhook not received")
	}
}

// Test templated headers
func (s *SuiteWebhook) TestTemplatedHeaders(c *C) {
	received := make(chan http.Header, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header
		w.WriteHeader(200)
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	def := WebhookDefinition{
		Name:   "test",
		URL:    ts.URL,
		Method: "POST",
		Headers: map[string]string{
			"X-Job-Name":   "{{.JobName}}",
			"X-Job-Status": "{{if .Failed}}failed{{else}}success{{end}}",
		},
		Body:    "test",
		Timeout: 5,
	}

	webhook, err := NewWebhookFromDefinition(def, &TestLogger{})
	c.Assert(err, IsNil)

	err = webhook.Run(s.ctx)
	c.Assert(err, IsNil)

	time.Sleep(100 * time.Millisecond)

	select {
	case headers := <-received:
		c.Assert(headers.Get("X-Job-Status"), Equals, "success")
	case <-time.After(2 * time.Second):
		c.Fatal("webhook not received")
	}
}

// Test retry logic
func (s *SuiteWebhook) TestRetryLogic(c *C) {
	attempts := 0
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()

		// Fail first 2 attempts, succeed on 3rd
		if current < 3 {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	def := WebhookDefinition{
		Name:    "test",
		URL:     ts.URL,
		Method:  "POST",
		Body:    "test",
		Timeout: 5,
		Retry: &RetryConfig{
			Count:   3,
			Backoff: "10ms",
		},
	}

	webhook, err := NewWebhookFromDefinition(def, &TestLogger{})
	c.Assert(err, IsNil)

	err = webhook.Run(s.ctx)
	c.Assert(err, IsNil)

	// Wait for retries to complete
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	c.Assert(attempts, Equals, 3)
	mu.Unlock()
}

// Test config file parsing
func (s *SuiteWebhook) TestParseWebhookConfigFile(c *C) {
	// Create temp config file
	content := `{
		"webhooks": [
			{
				"name": "test1",
				"priority": 100,
				"url": "https://example.com/webhook1",
				"method": "POST",
				"body": "test"
			},
			{
				"name": "test2",
				"priority": 200,
				"url": "https://example.com/webhook2",
				"body": "test"
			}
		]
	}`

	tmpfile, err := os.CreateTemp("", "webhook-test-*.json")
	c.Assert(err, IsNil)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(content))
	c.Assert(err, IsNil)
	tmpfile.Close()

	// Parse the file
	defs, err := parseWebhookConfigFile(tmpfile.Name())
	c.Assert(err, IsNil)
	c.Assert(len(defs), Equals, 2)
	c.Assert(defs[0].Name, Equals, "test1")
	c.Assert(defs[0].Method, Equals, "POST")
	c.Assert(defs[1].Name, Equals, "test2")
	c.Assert(defs[1].Method, Equals, "POST") // Default
}

// Test malformed JSON
func (s *SuiteWebhook) TestParseMalformedJSON(c *C) {
	tmpfile, err := os.CreateTemp("", "webhook-test-*.json")
	c.Assert(err, IsNil)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte("{invalid json"))
	c.Assert(err, IsNil)
	tmpfile.Close()

	_, err = parseWebhookConfigFile(tmpfile.Name())
	c.Assert(err, NotNil)
}

// Test missing URL validation
func (s *SuiteWebhook) TestMissingURL(c *C) {
	content := `{
		"webhooks": [
			{
				"name": "test",
				"body": "test"
			}
		]
	}`

	tmpfile, err := os.CreateTemp("", "webhook-test-*.json")
	c.Assert(err, IsNil)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(content))
	c.Assert(err, IsNil)
	tmpfile.Close()

	_, err = parseWebhookConfigFile(tmpfile.Name())
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Matches, ".*missing required 'url'.*")
}
