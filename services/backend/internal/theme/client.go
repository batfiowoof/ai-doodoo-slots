package theme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	maxTokens      = 4096
	temperature    = 0.7
	modelCacheTTL  = time.Hour
	cheapModelN    = 5
)

// schema is the strict JSON schema sent as response_format. The provider
// must satisfy it (require_parameters), and Validate re-checks everything
// server-side regardless.
const schema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "palette", "sprites"],
  "properties": {
    "name": {"type": "string", "minLength": 1, "maxLength": 48},
    "palette": {
      "type": "array",
      "minItems": 8,
      "maxItems": 8,
      "items": {"type": "string", "pattern": "^#[0-9a-fA-F]{8}$|^#[0-9a-fA-F]{6}$"}
    },
    "sprites": {
      "type": "array",
      "minItems": 8,
      "maxItems": 8,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "rows"],
        "properties": {
          "name": {"type": "string", "minLength": 1, "maxLength": 24},
          "rows": {
            "type": "array",
            "minItems": 16,
            "maxItems": 16,
            "items": {"type": "string", "pattern": "^[0-9a-fA-F]{16}$"}
          }
        }
      }
    }
  }
}`

// ErrNoAPIKey means generation is not configured on this deployment.
var ErrNoAPIKey = errors.New("theme generation unavailable: no API key configured")

// Client talks to OpenRouter. Model slugs churn constantly, so cheap
// structured-output-capable models are discovered live and passed as a
// fallback chain rather than trusting any hardcoded slug.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
	clock   clock.Clock

	models       []string
	modelsLoaded time.Time
}

func NewClient(apiKey string, modelOverride []string, clk clock.Clock) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 60 * time.Second},
		clock:   clk,
		models:  modelOverride,
	}
}

// SetBaseURL points the client at a test server.
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// ---- model discovery ----

type modelsResponse struct {
	Data []modelEntry `json:"data"`
}

type modelEntry struct {
	ID                  string   `json:"id"`
	SupportedParameters []string `json:"supported_parameters"`
	Pricing             struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// pickModels returns up to cheapModelN cheap model IDs that support
// structured outputs. Overrides skip discovery entirely.
func (c *Client) pickModels(ctx context.Context) ([]string, error) {
	if len(c.models) > 0 && c.clock.Now().Sub(c.modelsLoaded) < modelCacheTTL {
		return c.models, nil
	}
	if len(c.models) > 0 {
		return c.models, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("fetch models: status %d: %s", res.StatusCode, body)
	}

	var mr modelsResponse
	if err := json.NewDecoder(res.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	type priced struct {
		id   string
		cost float64
	}
	var candidates []priced
	for _, m := range mr.Data {
		supported := false
		for _, p := range m.SupportedParameters {
			if p == "structured_outputs" {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		var prompt, completion float64
		fmt.Sscanf(m.Pricing.Prompt, "%f", &prompt)
		fmt.Sscanf(m.Pricing.Completion, "%f", &completion)
		candidates = append(candidates, priced{id: m.ID, cost: prompt + completion})
	}
	if len(candidates) == 0 {
		return nil, errors.New("no structured-output models available")
	}
	// Cheap tier first.
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].cost < candidates[j-1].cost; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
	n := cheapModelN
	if len(candidates) < n {
		n = len(candidates)
	}
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = candidates[i].id
	}
	c.models = ids
	c.modelsLoaded = c.clock.Now()
	return ids, nil
}

// ---- generation ----

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonSchemaOpt struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type responseFormat struct {
	Type       string        `json:"type"`
	JSONSchema *jsonSchemaOpt `json:"json_schema,omitempty"`
}

type providerPref struct {
	RequireParameters bool `json:"require_parameters"`
}

type chatRequest struct {
	Model         string         `json:"model"`
	Models        []string       `json:"models,omitempty"`
	Messages      []chatMessage  `json:"messages"`
	ResponseFormat responseFormat `json:"response_format"`
	Provider      providerPref   `json:"provider"`
	MaxTokens     int            `json:"max_tokens"`
	Temperature   float64        `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

const systemPrompt = `You design pixel-art themes for a retro slot machine. ` +
	`Respond only with JSON matching the schema. Exactly 8 sprites ordered ` +
	`common to rare, each a 16x16 grid as 16 strings of 16 hex characters ` +
	`where each character indexes the palette and "0" is transparent. Give ` +
	`sprites bold, distinct silhouettes that read at small sizes. Palette has ` +
	`exactly 8 colors; index 0 should be "#00000000".`

// Generate calls the provider and validates the whole theme. Any failure —
// transport, HTTP, parse, or validation — is an error; the caller keeps its
// current theme untouched.
func (c *Client) Generate(ctx context.Context, prompt string) (Theme, error) {
	models, err := c.pickModels(ctx)
	if err != nil {
		return Theme{}, err
	}

	payload := chatRequest{
		Model:  models[0],
		Models: models,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "Theme: " + prompt},
		},
		ResponseFormat: responseFormat{
			Type:       "json_schema",
			JSONSchema: &jsonSchemaOpt{Name: "theme", Strict: true, Schema: json.RawMessage(schema)},
		},
		Provider:    providerPref{RequireParameters: true},
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Theme{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Theme{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	res, err := c.http.Do(req)
	if err != nil {
		return Theme{}, fmt.Errorf("generate: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
	 snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return Theme{}, fmt.Errorf("generate: status %d: %s", res.StatusCode, snippet)
	}

	var cr chatResponse
	if err := json.NewDecoder(res.Body).Decode(&cr); err != nil {
		return Theme{}, fmt.Errorf("decode response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return Theme{}, errors.New("generate: no choices in response")
	}
	return ParseContent(cr.Choices[0].Message.Content)
}

// ParseContent extracts and validates a Theme from model message content.
// Strict json_schema should yield bare JSON, but fences are stripped
// defensively before parsing.
func ParseContent(content string) (Theme, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var t Theme
	if err := json.Unmarshal([]byte(content), &t); err != nil {
		return Theme{}, fmt.Errorf("model content is not a valid theme: %w", err)
	}
	t = Normalize(t)
	if err := Validate(t); err != nil {
		return Theme{}, fmt.Errorf("model content failed validation: %w", err)
	}
	return t, nil
}
