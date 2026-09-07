package plinko

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

func stream(seed byte, nonce int64) *fair.Stream {
	return fair.NewPersonalStream(bytes.Repeat([]byte{seed}, fair.SeedSize), "plinko-test", nonce)
}

func params(t *testing.T, rows int, risk string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(Params{Rows: rows, Risk: risk})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return raw
}

func TestValidateBet(t *testing.T) {
	g := New()
	for _, ok := range []int64{1, 7, 10000} {
		if err := g.ValidateBet(ok); err != nil {
			t.Errorf("ValidateBet(%d): %v", ok, err)
		}
	}
	for _, bad := range []int64{0, -1, 10001} {
		if err := g.ValidateBet(bad); err == nil {
			t.Errorf("ValidateBet(%d) accepted", bad)
		}
	}
}

func TestValidateParams(t *testing.T) {
	g := New()
	for _, risk := range RiskOptions {
		for _, rows := range RowsOptions {
			if err := g.ValidateParams(params(t, rows, risk)); err != nil {
				t.Errorf("rows %d risk %s rejected: %v", rows, risk, err)
			}
		}
	}
	for _, bad := range []Params{{Rows: 7, Risk: "low"}, {Rows: 16, Risk: "extreme"}, {Rows: 0, Risk: "low"}, {Rows: 12, Risk: ""}} {
		raw, _ := json.Marshal(bad)
		if err := g.ValidateParams(raw); err == nil {
			t.Errorf("params %+v accepted", bad)
		}
	}
}

func TestTablesWellFormed(t *testing.T) {
	for _, risk := range RiskOptions {
		for _, rows := range RowsOptions {
			table := Table(risk, rows)
			if len(table) != rows+1 {
				t.Fatalf("%s/%d: %d buckets, want %d", risk, rows, len(table), rows+1)
			}
			for i, m := range table {
				if m <= 0 {
					t.Fatalf("%s/%d bucket %d: non-positive multiplier %v", risk, rows, i, m)
				}
				// Symmetric board.
				if table[len(table)-1-i] != m {
					t.Fatalf("%s/%d: not symmetric at %d", risk, rows, i)
				}
			}
		}
	}
}

// TestTableRTP gates every board's binomial return near 0.99.
func TestTableRTP(t *testing.T) {
	for _, risk := range RiskOptions {
		for _, rows := range RowsOptions {
			rtp := exactRTP(risk, rows)
			if rtp < 0.985 || rtp > 0.995 {
				t.Errorf("%s/%d exact RTP %.5f outside [0.985, 0.995]", risk, rows, rtp)
			}
		}
	}
}

func TestDropDeterministic(t *testing.T) {
	a, err := New().PlayWithParams(stream(0x5A, 1), 10, params(t, 12, "high"))
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	b, err := New().PlayWithParams(stream(0x5A, 1), 10, params(t, 12, "high"))
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if a.PayoutCredits != b.PayoutCredits || string(a.Payload) != string(b.Payload) {
		t.Fatalf("same seed diverged")
	}
}

func TestDropPayoutMatchesBucket(t *testing.T) {
	g := New()
	for _, risk := range RiskOptions {
		for _, rows := range RowsOptions {
			table := Table(risk, rows)
			for nonce := int64(1); nonce <= 300; nonce++ {
				out, err := g.PlayWithParams(stream(0x42, nonce), 7, params(t, rows, risk))
				if err != nil {
					t.Fatalf("play: %v", err)
				}
				var p struct {
					Bucket     int     `json:"bucket"`
					Multiplier float64 `json:"multiplier"`
					Path       []bool  `json:"path"`
				}
				if err := json.Unmarshal(out.Payload, &p); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if len(p.Path) != rows {
					t.Fatalf("path length %d, want %d", len(p.Path), rows)
				}
				rights := 0
				for _, step := range p.Path {
					if step {
						rights++
					}
				}
				if rights != p.Bucket || p.Bucket < 0 || p.Bucket >= len(table) {
					t.Fatalf("bucket %d vs rights %d", p.Bucket, rights)
				}
				if p.Multiplier != table[p.Bucket] {
					t.Fatalf("multiplier %v != table %v", p.Multiplier, table[p.Bucket])
				}
				want := int64(math.Floor(7 * table[p.Bucket]))
				if out.PayoutCredits != want {
					t.Fatalf("payout %d, want %d", out.PayoutCredits, want)
				}
			}
		}
	}
}

// TestSimulationRTP gates the simulated return against each board's exact
// binomial value.
func TestSimulationRTP(t *testing.T) {
	g := New()
	const drops = 120000
	const bet = int64(100)
	for _, risk := range RiskOptions {
		for _, rows := range RowsOptions {
			var wagered, returned int64
			for nonce := int64(1); nonce <= drops; nonce++ {
				st := fair.NewPersonalStream(bytes.Repeat([]byte{byte(nonce/1000 + 1)}, fair.SeedSize), "plinko-sim", nonce%1000+1)
				out, err := g.PlayWithParams(st, bet, params(t, rows, risk))
				if err != nil {
					t.Fatalf("play: %v", err)
				}
				wagered += bet
				returned += out.PayoutCredits
			}
			rtp := float64(returned) / float64(wagered)
			exact := exactRTP(risk, rows)
			// Floor quantization + sampling noise; 3% headroom is generous.
			if math.Abs(rtp-exact) > 0.03 {
				t.Errorf("%s/%d: sim RTP %.4f vs exact %.4f", risk, rows, rtp, exact)
			}
		}
	}
}
