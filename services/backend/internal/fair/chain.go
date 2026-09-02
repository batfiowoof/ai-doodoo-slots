package fair

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChainLength is the number of pre-generated links in the commit-reveal
// chain. Each round consumes one link in reverse; when exhausted, a new
// chain is generated with indexes continuing after the old ones.
const ChainLength = 1000

// ErrChainExhausted means every link has been revealed and a new chain is
// needed.
var ErrChainExhausted = fmt.Errorf("chain exhausted")

// ChainService generates and reveals the commit-reveal hash chain shared by
// all round games. seed[i] = sha256(seed[i+1]); only hashes are stored until
// a seed is revealed at its round's settlement, and each revealed seed must
// hash to the previously revealed one — nobody, including the operator, can
// reorder or skip links.
type ChainService struct {
	pool *pgxpool.Pool
}

func NewChainService(pool *pgxpool.Pool) *ChainService {
	return &ChainService{pool: pool}
}

// EnsureChain generates a fresh chain when none exists (or the active one
// is exhausted). Indexes continue after the highest existing index so the
// UNIQUE constraint holds and backwards verification spans chains.
func (s *ChainService) EnsureChain(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	remaining, err := q.CountUnrevealedChainSeeds(ctx)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return tx.Commit(ctx)
	}
	maxIndex, err := q.MaxChainIndex(ctx)
	if err != nil {
		return err
	}
	group, err := q.NextChainGroup(ctx)
	if err != nil {
		return err
	}

	// Terminal seed first, then hash backwards: seed[i] = sha256(seed[i+1]).
	// The row for index j stores plain = seed[j] and hash = sha256(seed[j]).
	terminal := make([]byte, SeedSize)
	if _, err := rand.Read(terminal); err != nil {
		return fmt.Errorf("generate terminal seed: %w", err)
	}
	seed := terminal
	for j := ChainLength; j >= 1; j-- {
		sum := sha256.Sum256(seed)
		if err := q.InsertChainSeed(ctx, store.InsertChainSeedParams{
			ChainGroup: group,
			Index:      maxIndex + int64(j),
			SeedHash:   hex.EncodeToString(sum[:]),
			SeedPlain:  pgtype.Text{String: hex.EncodeToString(seed), Valid: true},
		}); err != nil {
			return err
		}
		seed = sum[:]
	}
	return tx.Commit(ctx)
}

// RevealNext pops the next unrevealed link in reverse order (highest index
// first), marks it revealed, and verifies it two ways: the stored hash must
// match sha256 of the stored plain, and the plain must hash to the
// previously revealed link's seed. The returned seed feeds NewChainStream.
func (s *ChainService) RevealNext(ctx context.Context) (seed []byte, index int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)

	next, err := q.GetNextUnrevealedChainSeed(ctx)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, 0, ErrChainExhausted
		}
		return nil, 0, err
	}
	plain, err := hex.DecodeString(next.SeedPlain.String)
	if err != nil {
		return nil, 0, fmt.Errorf("stored seed not hex: %w", err)
	}
	// Commitment check: the stored hash must match the stored plain.
	sum := sha256.Sum256(plain)
	if hex.EncodeToString(sum[:]) != next.SeedHash {
		return nil, 0, fmt.Errorf("chain integrity violated at index %d", next.Index)
	}
	if prev, perr := q.GetRevealedChainSeedAfter(ctx, store.GetRevealedChainSeedAfterParams{
		Index:      next.Index,
		ChainGroup: next.ChainGroup,
	}); perr == nil {
		// Chain relation: seed[i] = sha256(seed[i+1]). The previously
		// revealed link (index+1) must hash to this seed. A gap means the
		// previous link belonged to an older, independent chain.
		if prev.Index == next.Index+1 {
			prevPlain, derr := hex.DecodeString(prev.SeedPlain.String)
			if derr != nil {
				return nil, 0, fmt.Errorf("stored seed not hex: %w", derr)
			}
			prevSum := sha256.Sum256(prevPlain)
			if hex.EncodeToString(prevSum[:]) != next.SeedPlain.String {
				return nil, 0, fmt.Errorf("chain break: revealed seed[%d] does not hash to seed[%d]", prev.Index, next.Index)
			}
		}
	} else if perr != pgx.ErrNoRows {
		return nil, 0, perr
	}
	if err := q.RevealChainSeed(ctx, next.ID); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return plain, next.Index, nil
}

// CurrentHead returns the index and hash commitment of the next link to be
// revealed, for display. Only the hash is published.
func (s *ChainService) CurrentHead(ctx context.Context) (index int64, hash string, err error) {
	row, err := store.New(s.pool).GetNextUnrevealedChainSeed(ctx)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, "", ErrChainExhausted
		}
		return 0, "", err
	}
	return row.Index, row.SeedHash, nil
}
