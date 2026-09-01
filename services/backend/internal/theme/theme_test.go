package theme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/jackc/pgx/v5/pgxpool"
)

func validTheme() Theme {
	palette := []string{"#00000000", "#1b3a4b", "#2e6f8e", "#3aa4c9", "#63c7e0", "#9fe3ef", "#e63f8c", "#ffffff"}
	sprites := make([]Sprite, SpriteCount)
	for i := range sprites {
		rows := make([]string, SpriteSize)
		for y := range rows {
			rows[y] = strings.Repeat("0", 7) + string(rune('1'+i%7)) + strings.Repeat("0", 8)
		}
		sprites[i] = Sprite{Name: spriteNames[i], Rows: rows}
	}
	return Theme{Name: "Sunken Arcade", Palette: palette, Sprites: sprites}
}

var spriteNames = []string{"a", "b", "c", "d", "e", "f", "g", "h"}

func row16(ch byte) string { return strings.Repeat(string(ch), SpriteSize) }

func TestValidateAcceptsGoodTheme(t *testing.T) {
	if err := Validate(validTheme()); err != nil {
		t.Fatalf("valid theme rejected: %v", err)
	}
}

func TestValidateRejectsMalformed(t *testing.T) {
	t.Run("palette wrong count", func(t *testing.T) {
		tt := validTheme()
		tt.Palette = tt.Palette[:7]
		if err := Validate(tt); !errors.Is(err, ErrBadPalette) {
			t.Fatalf("want ErrBadPalette, got %v", err)
		}
	})
	t.Run("palette unparseable", func(t *testing.T) {
		tt := validTheme()
		tt.Palette[3] = "#12345"
		if err := Validate(tt); err == nil || !strings.Contains(err.Error(), "palette") {
			t.Fatalf("want palette error, got %v", err)
		}
	})
	t.Run("too few sprites", func(t *testing.T) {
		tt := validTheme()
		tt.Sprites = tt.Sprites[:7]
		if err := Validate(tt); !errors.Is(err, ErrBadSpriteCount) {
			t.Fatalf("want ErrBadSpriteCount, got %v", err)
		}
	})
	t.Run("row too short", func(t *testing.T) {
		tt := validTheme()
		tt.Sprites[2].Rows[5] = strings.Repeat("1", 15)
		if err := Validate(tt); !errors.Is(err, ErrBadSprite) {
			t.Fatalf("want ErrBadSprite, got %v", err)
		}
	})
	t.Run("non-hex character", func(t *testing.T) {
		tt := validTheme()
		bad := row16('1')
		tt.Sprites[0].Rows[3] = bad[:8] + "g" + bad[9:]
		if err := Validate(tt); !errors.Is(err, ErrBadSprite) {
			t.Fatalf("want ErrBadSprite, got %v", err)
		}
	})
	t.Run("empty name", func(t *testing.T) {
		tt := validTheme()
		tt.Name = ""
		if err := Validate(tt); !errors.Is(err, ErrEmptyName) {
			t.Fatalf("want ErrEmptyName, got %v", err)
		}
	})
}

func TestParseContentRejectsGarbage(t *testing.T) {
	for _, bad := range []string{
		``,                                       // empty
		`not json at all`,                        // prose
		`{"name":"x"}`,                           // truncated
		`{"name":"x","palette":[],"sprites":[]}`, // wrong shape
	} {
		if _, err := ParseContent(bad); err == nil {
			t.Fatalf("ParseContent(%q) accepted garbage", bad)
		}
	}
	// Fenced JSON parses.
	good, _ := json.Marshal(validTheme())
	fenced := "```json\n" + string(good) + "\n```"
	if _, err := ParseContent(fenced); err != nil {
		t.Fatalf("fenced valid JSON rejected: %v", err)
	}
}

// fakeOpenRouter is a test double for the OpenRouter chat completions API.
type fakeOpenRouter struct {
	t       *testing.T
	calls   int
	content string
	status  int
}

func (f *fakeOpenRouter) handler(w http.ResponseWriter, r *http.Request) {
	f.calls++
	if f.status != 0 && f.status != http.StatusOK {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
		return
	}
	// Authorization header must carry the key.
	if r.Header.Get("Authorization") != "Bearer test-key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Fatalf("bad request body: %v", err)
	}
	if !req.ResponseFormat.JSONSchema.Strict {
		f.t.Fatal("expected strict json_schema response_format")
	}
	if !req.Provider.RequireParameters {
		f.t.Fatal("expected provider.require_parameters")
	}
	if req.MaxTokens != maxTokens {
		f.t.Fatal("max_tokens not capped")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"content": f.content}},
		},
	})
}

func newTestService(t *testing.T, fake *fakeOpenRouter) (*Service, *pgxpool.Pool, int64) {
	t.Helper()
	pool := testdb.Pool(t)
	userID := testdb.NewUser(t, pool, 0)
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	client := NewClient("test-key", []string{"test-model-a", "test-model-b"}, clock.Real{})
	client.SetBaseURL(srv.URL + "/v1")
	return NewService(pool, client, clock.Real{}), pool, userID
}

// TestMalformedResponseKeepsPreviousTheme is the phase 9 gate: a deliberately
// malformed model response is rejected whole, nothing is stored, and the
// machine keeps its previous theme.
func TestMalformedResponseKeepsPreviousTheme(t *testing.T) {
	good, _ := json.Marshal(validTheme())
	fake := &fakeOpenRouter{t: t, content: string(good)}
	svc, _, userID := newTestService(t, fake)
	ctx := context.Background()

	// 1. A good generation is stored and becomes the current theme.
	first, err := svc.Generate(ctx, userID, "sunken pirate arcade")
	if err != nil {
		t.Fatalf("good generate: %v", err)
	}

	// 2. Provider starts returning garbage.
	fake.content = `{"name":"Broken","palette":` + `"`
	if _, err := svc.Generate(ctx, userID, "haunted train station"); !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("want ErrProviderFailed, got %v", err)
	}
	fake.content = `I am a helpful assistant and here is your theme!`
	if _, err := svc.Generate(ctx, userID, "haunted train station"); !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("want ErrProviderFailed for prose, got %v", err)
	}
	fake.content = string(good) + ` {"extra": true}` // trailing junk
	if _, err := svc.Generate(ctx, userID, "haunted train station"); !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("want ErrProviderFailed for trailing junk, got %v", err)
	}

	// 3. The previous theme is untouched and still listed.
	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != first.ID || list[0].Name != "Sunken Arcade" {
		t.Fatalf("previous theme not intact: %d themes, first=%+v", len(list), first)
	}
}

func TestGeneratePersistsAndDedupes(t *testing.T) {
	good, _ := json.Marshal(validTheme())
	fake := &fakeOpenRouter{t: t, content: string(good)}
	svc, _, userID := newTestService(t, fake)
	ctx := context.Background()

	first, err := svc.Generate(ctx, userID, "sunken pirate arcade")
	if err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := fake.calls

	// Same prompt: served from storage, no provider call.
	second, err := svc.Generate(ctx, userID, "sunken pirate arcade")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatal("repeat prompt returned a different theme")
	}
	if fake.calls != callsAfterFirst {
		t.Fatalf("repeat prompt hit the provider (%d calls)", fake.calls)
	}

	list, _ := svc.List(ctx, userID)
	if len(list) != 1 {
		t.Fatalf("expected 1 stored theme, got %d", len(list))
	}
}

func TestGenerateRateLimit(t *testing.T) {
	good, _ := json.Marshal(validTheme())
	fake := &fakeOpenRouter{t: t, content: string(good)}
	svc, _, userID := newTestService(t, fake)
	ctx := context.Background()

	// Distinct prompts burn through the per-user window.
	for i := 0; i < generateMaxPer; i++ {
		if _, err := svc.Generate(ctx, userID, fmt.Sprintf("prompt-%d", i)); err != nil {
			t.Fatalf("generate %d: %v", i, err)
		}
	}
	if _, err := svc.Generate(ctx, userID, "one prompt too many"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestGenerateWithoutAPIKey(t *testing.T) {
	pool := testdb.Pool(t)
	userID := testdb.NewUser(t, pool, 0)
	svc := NewService(pool, nil, clock.Real{})
	if _, err := svc.Generate(context.Background(), userID, "neon subway"); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("want ErrNoAPIKey, got %v", err)
	}
}

func TestPromptValidation(t *testing.T) {
	good, _ := json.Marshal(validTheme())
	fake := &fakeOpenRouter{t: t, content: string(good)}
	svc, _, userID := newTestService(t, fake)

	if _, err := svc.Generate(context.Background(), userID, strings.Repeat("x", MaxPromptLen+1)); !errors.Is(err, ErrPromptInvalid) {
		t.Fatalf("want ErrPromptInvalid for long prompt, got %v", err)
	}
	if _, err := svc.Generate(context.Background(), userID, ""); !errors.Is(err, ErrPromptInvalid) {
		t.Fatalf("want ErrPromptInvalid for empty prompt, got %v", err)
	}
}

// TestModelDiscoveryFiltersAndSorts checks the live model picker against a
// fake /models endpoint: only structured-output models, cheapest first.
func TestModelDiscoveryFiltersAndSorts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "expensive-struct", "supported_parameters": []string{"structured_outputs"}, "pricing": map[string]string{"prompt": "0.001", "completion": "0.002"}},
				{"id": "cheap-nonstruct", "supported_parameters": []string{"tools"}, "pricing": map[string]string{"prompt": "0", "completion": "0"}},
				{"id": "cheap-struct", "supported_parameters": []string{"structured_outputs", "tools"}, "pricing": map[string]string{"prompt": "0.0001", "completion": "0.0001"}},
			},
		})
	})
	var gotModels []string
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModels = req.Models
		good, _ := json.Marshal(validTheme())
		content, _ := json.Marshal(string(good)) // content is a JSON string
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + string(content) + `}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient("test-key", nil, clock.Real{})
	client.SetBaseURL(srv.URL + "/v1")
	got, err := client.Generate(context.Background(), "sunken pirate arcade")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Sunken Arcade" {
		t.Fatalf("theme name = %q", got.Name)
	}
	if len(gotModels) != 2 || gotModels[0] != "cheap-struct" || gotModels[1] != "expensive-struct" {
		t.Fatalf("models = %v, want cheap-struct first (non-struct filtered)", gotModels)
	}
}
