package round

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
	"github.com/ai-doodoo-slots/services/backend/internal/game"
	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSettleTwoHundredPlayersAtomically is the phase-14 gate: 200 simulated
// concurrent players settle in one transaction with the ledger balanced, no
// deadlocks across repeated runs, and duplicated settlement cannot pay
// twice. Skips without a test database.
func TestSettleTwoHundredPlayersAtomically(t *testing.T) {
	pool := testdb.Pool(t)
	persist := NewPersister(pool)
	ctx := context.Background()

	const players = 200
	const stake = int64(100)

	for run := 0; run < 3; run++ {
		run := run
		m, err := Start("crash-1", stubGame{crash: 2.0}, fair.NewChainStream([]byte{byte(run), 1}, "s"), testConfig(), testBase)
		if err != nil {
			t.Fatalf("start: %v", err)
		}

		// Real room + round rows: bets reference rounds(rooms(id)).
		var roomID int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO rooms (game_id, slug, name, min_bet, max_bet, capacity)
			 VALUES ('crash', $1, 'gate room', 1, 1000, 500) RETURNING id`,
			fmt.Sprintf("gate-%d-%d", run, time.Now().UnixNano())).Scan(&roomID); err != nil {
			t.Fatalf("room: %v", err)
		}
		roundID, err := persist.OpenRound(ctx, roomID, "crash", 0, "")
		if err != nil {
			t.Fatalf("open round: %v", err)
		}

		// 200 users with funded wallets; concurrent debits through the
		// production PlaceBet path.
		var wg sync.WaitGroup
		userIDs := make([]int64, players)
		for i := 0; i < players; i++ {
			userIDs[i] = testdb.NewUser(t, pool, 1000)
		}
		start := time.Now()
		for i := 0; i < players; i++ {
			wg.Add(1)
			go func(userID int64) {
				defer wg.Done()
				if err := m.AddBet(userID, stake, 200); err != nil { // auto 2.00x
					t.Errorf("add bet: %v", err)
					return
				}
				betID, _, err := persist.PlaceBet(ctx, userID, roundID, stake, 200, fmt.Sprintf("run%d-%d", run, userID))
				if err != nil {
					t.Errorf("place bet: %v", err)
					return
				}
				m.SetBetID(userID, betID)
			}(userIDs[i])
		}
		wg.Wait()

		// Drive the round to settlement.
		now := testBase
		for !m.Done() {
			now = now.Add(200 * time.Millisecond)
			m.Step(now)
		}
		settled := m.Settlements()
		if len(settled) != players {
			t.Fatalf("settlements = %d, want %d", len(settled), players)
		}
		if err := persist.SettleRound(ctx, roundID, toSettled(settled)); err != nil {
			t.Fatalf("settle round: %v", err)
		}
		t.Logf("run %d: %d players settled in %v", run, players, time.Since(start))

		// Ledger balanced: SUM(transactions) == wallets.balance_credits for
		// every player.
		payoutByUser := make(map[int64]int64, players)
		for _, s := range settled {
			payoutByUser[s.UserID] = s.PayoutCredits
		}
		for _, uid := range userIDs {
			var bal, ledger int64
			if err := pool.QueryRow(ctx,
				`SELECT balance_credits FROM wallets WHERE user_id = $1`, uid).Scan(&bal); err != nil {
				t.Fatalf("balance: %v", err)
			}
			if err := pool.QueryRow(ctx,
				`SELECT COALESCE(SUM(amount_credits),0) FROM transactions WHERE wallet_id = $1`, uid).Scan(&ledger); err != nil {
				t.Fatalf("ledger: %v", err)
			}
			if bal != ledger {
				t.Fatalf("run %d user %d: balance %d != ledger %d", run, uid, bal, ledger)
			}
			if want := int64(1000) - stake + payoutByUser[uid]; bal != want {
				t.Fatalf("run %d user %d: balance %d, want %d", run, uid, bal, want)
			}
		}

		// Duplicated settlement must not pay twice: the ledger idempotency
		// key is unique, so a replay is rejected outright.
		if err := persist.SettleRound(ctx, roundID, toSettled(settled)); err == nil {
			t.Fatal("duplicate settlement accepted")
		}
	}
}

func toSettled(s []Settlement) []SettledBet {
	out := make([]SettledBet, 0, len(s))
	for _, x := range s {
		out = append(out, SettledBet{
			BetID: x.BetID, UserID: x.UserID,
			PayoutCredits:        x.PayoutCredits,
			MultiplierHundredths: x.MultiplierHundredths,
		})
	}
	return out
}

var _ = game.RoundGame(nil)
var _ = (*pgxpool.Pool)(nil)


