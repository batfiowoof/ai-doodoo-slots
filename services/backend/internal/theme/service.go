package theme

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MaxPromptLen    = 200
	generateWindow  = time.Hour
	generateMaxPer  = 5
)

var (
	ErrPromptInvalid  = errors.New("prompt must be 1-200 characters")
	ErrRateLimited    = errors.New("too many themes generated, try again later")
	ErrProviderFailed = errors.New("theme generation failed")
)

// Service generates, validates, persists, and serves themes. Repeat use of
// the same prompt costs nothing: stored themes are returned without a
// provider call.
type Service struct {
	pool   *pgxpool.Pool
	q      *store.Queries
	client *Client // nil when no API key is configured

	mu    sync.Mutex
	hits  map[int64]*genBucket
	clock clock.Clock
}

type genBucket struct {
	start time.Time
	count int
}

func NewService(pool *pgxpool.Pool, client *Client, clk clock.Clock) *Service {
	return &Service{
		pool:   pool,
		q:      store.New(pool),
		client: client,
		hits:   make(map[int64]*genBucket),
		clock:  clk,
	}
}

// allow throttles generation per user: generateMaxPer per generateWindow.
func (s *Service) allow(userID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	b, ok := s.hits[userID]
	if !ok || now.Sub(b.start) >= generateWindow {
		b = &genBucket{start: now}
		s.hits[userID] = b
	}
	b.count++
	return b.count <= generateMaxPer
}

func promptHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// Generate returns a stored theme for a repeated prompt, or generates a new
// one. A failed or malformed provider response is an error; nothing is
// stored and previously saved themes are untouched.
// StoredTheme is a persisted theme returned by the service.
type StoredTheme struct {
	ID        int64
	Name      string
	Palette   []byte
	Sprites   []byte
	CreatedAt time.Time
}

func rowToTheme(id int64, name string, palette, sprites []byte, created time.Time) StoredTheme {
	return StoredTheme{ID: id, Name: name, Palette: palette, Sprites: sprites, CreatedAt: created}
}

func (s *Service) Generate(ctx context.Context, userID int64, prompt string) (StoredTheme, error) {
	if prompt == "" || len(prompt) > MaxPromptLen {
		return StoredTheme{}, ErrPromptInvalid
	}
	if !s.allow(userID) {
		return StoredTheme{}, ErrRateLimited
	}

	hash := promptHash(prompt)
	if existing, err := s.q.GetThemeByPromptHash(ctx, store.GetThemeByPromptHashParams{
		UserID:     userID,
		PromptHash: hash,
	}); err == nil {
		return rowToTheme(existing.ID, existing.Name, existing.Palette, existing.Sprites, existing.CreatedAt), nil // repeat use costs nothing
	}

	if s.client == nil {
		return StoredTheme{}, ErrNoAPIKey
	}
	t, err := s.client.Generate(ctx, prompt)
	if err != nil {
		return StoredTheme{}, fmt.Errorf("%w: %v", ErrProviderFailed, err)
	}

	paletteJSON, err := json.Marshal(t.Palette)
	if err != nil {
		return StoredTheme{}, err
	}
	spritesJSON, err := json.Marshal(t.Sprites)
	if err != nil {
		return StoredTheme{}, err
	}
	created, err := s.q.CreateTheme(ctx, store.CreateThemeParams{
		UserID:     userID,
		Name:       t.Name,
		Palette:    paletteJSON,
		Sprites:    spritesJSON,
		PromptHash: hash,
	})
	if err != nil {
		return StoredTheme{}, fmt.Errorf("store theme: %w", err)
	}
	return rowToTheme(created.ID, created.Name, created.Palette, created.Sprites, created.CreatedAt), nil
}

// List returns the user's saved themes.
func (s *Service) List(ctx context.Context, userID int64) ([]StoredTheme, error) {
	rows, err := s.q.ListThemesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]StoredTheme, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToTheme(r.ID, r.Name, r.Palette, r.Sprites, r.CreatedAt))
	}
	return out, nil
}
