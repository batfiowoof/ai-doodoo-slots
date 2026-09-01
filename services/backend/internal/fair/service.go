package fair

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedText stores a raw seed as hex so it round-trips through a TEXT column.
func seedText(plain []byte) pgtype.Text {
	return pgtype.Text{String: hex.EncodeToString(plain), Valid: true}
}

// ErrNoActiveSeed means the user has no active server seed (should not
// happen after EnsureForUser).
var ErrNoActiveSeed = errors.New("no active server seed")

// Service manages per-user seed pairs.
type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

// EnsureForUser returns the user's active server seed, creating one on
// first use. The player sees only seed_hash; seed_plain stays server-side
// until rotation reveals it.
func (s *Service) EnsureForUser(ctx context.Context, userID int64) (store.ServerSeed, error) {
	seed, err := s.q.GetActiveServerSeed(ctx, userID)
	if err == nil {
		return seed, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.ServerSeed{}, fmt.Errorf("get active seed: %w", err)
	}

	plain, err := GenerateSeed()
	if err != nil {
		return store.ServerSeed{}, err
	}
	seed, err = s.q.CreateServerSeed(ctx, store.CreateServerSeedParams{
		UserID:     userID,
		SeedHash:   HashSeed(plain),
		SeedPlain:  seedText(plain),
		ClientSeed: auth.NewOpaqueID(8), // default client seed; user may rotate
	})
	if err != nil {
		return store.ServerSeed{}, fmt.Errorf("create seed: %w", err)
	}
	return seed, nil
}

// Current exposes the fairness triple's client-visible state: the active
// seed hash, the client seed, and the next nonce (stored nonce counts bets
// consumed, so next = nonce, 0-based).
func (s *Service) Current(ctx context.Context, userID int64) (seedHash, clientSeed string, nextNonce int64, err error) {
	seed, err := s.EnsureForUser(ctx, userID)
	if err != nil {
		return "", "", 0, err
	}
	return seed.SeedHash, seed.ClientSeed, seed.Nonce, nil
}

// Rotate reveals the old server seed and issues a new pair. Returns the
// revealed seed (hex of the raw bytes) and the new pair's hash and client seed.
func (s *Service) Rotate(ctx context.Context, userID int64, newClientSeed string) (revealed, newHash, clientSeed string, err error) {
	old, err := s.EnsureForUser(ctx, userID)
	if err != nil {
		return "", "", "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	q := store.New(tx)
	if err := q.RevealAndDeactivateServerSeed(ctx, old.ID); err != nil {
		return "", "", "", fmt.Errorf("deactivate old seed: %w", err)
	}

	if newClientSeed == "" {
		newClientSeed = auth.NewOpaqueID(8)
	}
	if len(newClientSeed) > MaxClientSeedLen {
		return "", "", "", fmt.Errorf("client seed exceeds %d bytes", MaxClientSeedLen)
	}

	plain, err := GenerateSeed()
	if err != nil {
		return "", "", "", err
	}
	created, err := q.CreateServerSeed(ctx, store.CreateServerSeedParams{
		UserID:     userID,
		SeedHash:   HashSeed(plain),
		SeedPlain:  seedText(plain),
		ClientSeed: newClientSeed,
	})
	if err != nil {
		return "", "", "", fmt.Errorf("create new seed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", "", fmt.Errorf("commit: %w", err)
	}

	return old.SeedPlain.String, created.SeedHash, created.ClientSeed, nil
}
