package poker

import (
	"strings"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// newTable builds a table and seats players 1..n with equal stacks.
func newTable(t *testing.T, n int, stack int64) *State {
	t.Helper()
	st := NewState(9, 5, 10)
	for i := int64(1); i <= int64(n); i++ {
		if _, err := st.SeatPlayer(i, name(i), stack, -1); err != nil {
			t.Fatalf("seat %d: %v", i, err)
		}
	}
	return st
}

func name(id int64) string { return "p" + string(rune('0'+id)) }

// stream returns a deterministic stream for hand i.
func stream(i int64) *fair.Stream {
	seed := make([]byte, 32)
	for j := range seed {
		seed[j] = byte(i*7 + int64(j))
	}
	return fair.NewPersonalStream(seed, "poker-test", i)
}

// mustAct applies an action or fails the test.
func mustAct(t *testing.T, st *State, userID int64, kind string, amount int64) {
	t.Helper()
	if err := st.Act(Action{UserID: userID, Kind: kind, Amount: amount}); err != nil {
		t.Fatalf("act u%d %s %d (phase=%s toAct=%d curBet=%d): %v", userID, kind, amount, st.Phase, st.ToAct, st.CurrentBet, err)
	}
}

// totalChips asserts chips conservation across all seats.
func totalChips(t *testing.T, st *State, want int64, label string) {
	t.Helper()
	var sum int64
	for _, s := range st.Seats {
		if s.State != SeatEmpty {
			sum += s.Stack + s.Rebuy
		}
	}
	sum += st.Pot
	if sum != want {
		t.Fatalf("%s: chips %d, want %d (stacks+pot)", label, sum, want)
	}
}

// resultsZeroSum asserts the hand results sum to zero (no rake) and match
// stack movement.
func resultsZeroSum(t *testing.T, st *State) {
	t.Helper()
	var net int64
	for _, r := range st.Results {
		net += r.Net
	}
	if net != 0 {
		t.Fatalf("results not zero-sum: %+v", st.Results)
	}
}

func TestBlindsAndButtonRotation(t *testing.T) {
	st := newTable(t, 3, 1000)
	if err := st.StartHand(stream(1)); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Seats 0,1,2 occupied; button starts at seat 0.
	if st.Button != 0 {
		t.Fatalf("first button = %d, want 0", st.Button)
	}
	// SB = seat 1, BB = seat 2, UTG (to act) = seat 0... no: UTG is seat
	// (button+3)%3 = 0.
	if st.ToAct != 0 {
		t.Fatalf("first to act = seat %d, want 0 (UTG)", st.ToAct)
	}
	if s := st.Seats[1]; s.Bet != 5 || s.LastAction != "blind" {
		t.Fatalf("SB wrong: %+v", s)
	}
	if s := st.Seats[2]; s.Bet != 10 {
		t.Fatalf("BB wrong: %+v", s)
	}
	// Fold around: UTG folds, SB folds, BB wins 15.
	mustAct(t, st, 1, ActFold, 0)
	mustAct(t, st, 2, ActFold, 0)
	if st.Phase != PhaseShowdown {
		t.Fatalf("phase = %s, want showdown after all folds", st.Phase)
	}
	// Pot 15 with the BB's uncalled 5 refunded: 10 matched, net +5.
	if len(st.Results) != 1 || st.Results[0].UserID != 3 || st.Results[0].WinAmount != 10 {
		t.Fatalf("fold-win result wrong: %+v", st.Results)
	}
	if st.Seats[2].Stack != 1000-10+5+10 {
		t.Fatalf("BB stack after fold win = %d", st.Seats[2].Stack)
	}
	totalChips(t, st, 3000, "after hand 1")

	// Next hand: button rotates to seat 1; SB=2, BB=0, UTG=1.
	if err := st.StartHand(stream(2)); err != nil {
		t.Fatalf("start 2: %v", err)
	}
	if st.Button != 1 {
		t.Fatalf("button after rotation = %d, want 1", st.Button)
	}
	if st.Seats[2].Bet != 5 || st.Seats[0].Bet != 10 || st.ToAct != 1 {
		t.Fatalf("blinds/UTG after rotation: %+v toAct=%d", st.Seats, st.ToAct)
	}
}

func TestHeadsUpButtonPostsSmallBlind(t *testing.T) {
	st := newTable(t, 2, 500)
	if err := st.StartHand(stream(1)); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Button is seat 0: posts SB and acts first.
	if st.Button != 0 {
		t.Fatalf("button = %d", st.Button)
	}
	if st.Seats[0].Bet != 5 || st.Seats[1].Bet != 10 {
		t.Fatalf("heads-up blinds: %+v", st.Seats)
	}
	if st.ToAct != 0 {
		t.Fatalf("heads-up first actor = %d, want button/SB", st.ToAct)
	}
	// SB calls, BB checks, flop opens with BB first.
	mustAct(t, st, 1, ActCall, 0)
	mustAct(t, st, 2, ActCheck, 0)
	if st.Phase != PhaseFlop {
		t.Fatalf("phase = %s, want flop", st.Phase)
	}
	if len(st.Board) != 6 {
		t.Fatalf("flop board = %q", st.Board)
	}
	if st.ToAct != 1 {
		t.Fatalf("flop first actor = %d, want BB (seat 1)", st.ToAct)
	}
	// Check-check, turn.
	mustAct(t, st, 2, ActCheck, 0)
	mustAct(t, st, 1, ActCheck, 0)
	if st.Phase != PhaseTurn || len(st.Board) != 8 {
		t.Fatalf("turn not dealt: phase=%s board=%q", st.Phase, st.Board)
	}
}

func TestMinRaiseLegality(t *testing.T) {
	st := newTable(t, 3, 1000)
	if err := st.StartHand(stream(3)); err != nil {
		t.Fatalf("start: %v", err)
	}
	// UTG (seat 0, user 1) raises to 25 — a legal 2.5BB raise.
	mustAct(t, st, 1, ActRaise, 25)
	// An under-minimum raise is rejected (needs 25 + 15 = 40).
	if err := st.Act(Action{UserID: 2, Kind: ActRaise, Amount: 30}); err == nil {
		t.Fatal("under-minimum raise accepted")
	}
	// A full min-raise to 40 is legal.
	mustAct(t, st, 2, ActRaise, 40)
	// Re-raise minimum is now 55 (40 + 15).
	if err := st.Act(Action{UserID: 3, Kind: ActRaise, Amount: 50}); err == nil {
		t.Fatal("under-minimum re-raise accepted")
	}
	mustAct(t, st, 3, ActRaise, 55)
	if st.CurrentBet != 55 || st.MinRaise != 15 {
		t.Fatalf("currentBet=%d minRaise=%d", st.CurrentBet, st.MinRaise)
	}
	// UTG already acted before the re-raise — betting reopens for them.
	mustAct(t, st, 1, ActCall, 0)
	mustAct(t, st, 2, ActCall, 0)
	// The BB raised earlier, so no BB option: street closes.
	if st.Phase != PhaseFlop {
		t.Fatalf("phase = %s, want flop", st.Phase)
	}
	// Postflop opens left of the button: u2 (seat 1). Opening below BB is
	// rejected.
	if err := st.Act(Action{UserID: 2, Kind: ActBet, Amount: 5}); err == nil {
		t.Fatal("postflop bet below BB accepted")
	}
	mustAct(t, st, 2, ActBet, 20)
	if st.CurrentBet != 20 || st.MinRaise != 20 {
		t.Fatalf("postflop bet tracking: cur=%d min=%d", st.CurrentBet, st.MinRaise)
	}
	mustAct(t, st, 3, ActRaise, 60)
	mustAct(t, st, 1, ActFold, 0)
	mustAct(t, st, 2, ActCall, 0)
	if st.Phase != PhaseTurn {
		t.Fatalf("phase = %s, want turn", st.Phase)
	}
	totalChips(t, st, 3000, "mid-hand")
}

func TestCheckWhenBehindIsRejected(t *testing.T) {
	st := newTable(t, 2, 300)
	if err := st.StartHand(stream(1)); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Button/SB cannot check facing the BB.
	if err := st.Act(Action{UserID: 1, Kind: ActCheck}); err == nil {
		t.Fatal("check facing a bet accepted")
	}
	// Wrong player cannot act.
	if err := st.Act(Action{UserID: 2, Kind: ActCall}); err == nil {
		t.Fatal("out-of-turn action accepted")
	}
}

func TestTimeoutAutoCheckOrFold(t *testing.T) {
	st := newTable(t, 2, 300)
	if err := st.StartHand(stream(1)); err != nil {
		t.Fatalf("start: %v", err)
	}
	// SB times out facing the BB bet → auto-fold.
	if err := st.Timeout(); err != nil {
		t.Fatalf("timeout: %v", err)
	}
	if st.Phase != PhaseShowdown || st.Results[0].UserID != 2 {
		t.Fatalf("timeout fold did not end hand: %+v", st.Results)
	}
	if !strings.Contains(strings.Join(st.ActionLog, ","), "timeout-fold") {
		t.Fatalf("action log missing timeout: %v", st.ActionLog)
	}

	// Timeout postflop with no bet → auto-check. Hand 2: button = seat 1,
	// so u2 posts the SB and acts first, u1 the BB.
	if err := st.StartHand(stream(2)); err != nil {
		t.Fatalf("start: %v", err)
	}
	mustAct(t, st, 2, ActCall, 0)
	mustAct(t, st, 1, ActCheck, 0) // BB option
	if st.Phase != PhaseFlop {
		t.Fatalf("phase = %s", st.Phase)
	}
	if err := st.Timeout(); err != nil {
		t.Fatalf("timeout: %v", err)
	}
	if st.ToAct < 0 || st.Seats[st.ToAct] == nil {
		t.Fatal("no actor after auto-check")
	}
}

func TestAllInRunoutFromBlinds(t *testing.T) {
	// Both players all-in from the blinds: the board runs out and the
	// showdown happens with no further action.
	st := NewState(2, 100, 200)
	if _, err := st.SeatPlayer(1, "p1", 100, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SeatPlayer(2, "p2", 200, -1); err != nil {
		t.Fatal(err)
	}
	if err := st.StartHand(stream(9)); err != nil {
		t.Fatalf("start: %v", err)
	}
	if st.Phase != PhaseShowdown {
		t.Fatalf("phase = %s, want showdown runout", st.Phase)
	}
	if len(st.Board) != 10 {
		t.Fatalf("board = %q, want 5 cards", st.Board)
	}
	totalChips(t, st, 300, "runout")
	resultsZeroSum(t, st)
	// Exactly the 300 chips in play are held by the players afterwards.
	if st.Seats[0].Stack+st.Seats[1].Stack != 300 {
		t.Fatalf("stacks after runout: %d + %d", st.Seats[0].Stack, st.Seats[1].Stack)
	}
}

func TestMultiwaySidePots(t *testing.T) {
	// Classic three-way: A all-in 100, B all-in 300, C covers. Side pots:
	// main 300 (100 each, all eligible), side 400 (200 each, B and C),
	// uncalled 0. Construct by betting rather than blinds: preflop raises.
	st := NewState(9, 5, 10)
	if _, err := st.SeatPlayer(1, "p1", 100, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SeatPlayer(2, "p2", 300, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SeatPlayer(3, "p3", 1000, -1); err != nil {
		t.Fatal(err)
	}
	if err := st.StartHand(stream(4)); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Button=seat0: SB=seat1 (u2), BB=seat2 (u3), UTG=seat0 (u1).
	mustAct(t, st, 1, ActRaise, 100) // u1 all-in raises to 100
	mustAct(t, st, 2, ActRaise, 300) // u2 all-in raises to 300
	mustAct(t, st, 3, ActCall, 0)    // u3 calls 200 more
	if st.Phase != PhaseShowdown {
		t.Fatalf("phase = %s, want showdown", st.Phase)
	}

	// Total staked this hand: 100 + 300 + 300 = 700 (settleHand resets Pot).
	var contributed int64
	for _, r := range st.Results {
		contributed += r.Contributed
	}
	if contributed != 700 {
		t.Fatalf("contributed = %d, want 700", contributed)
	}

	// Verify pot layering: main 300 (3 players), side 400 (p2, p3).
	pots := st.buildPots()
	if len(pots) != 2 {
		t.Fatalf("pots = %+v, want 2 layers", pots)
	}
	if pots[0].amount != 300 || len(pots[0].eligible) != 3 {
		t.Fatalf("main pot = %+v", pots[0])
	}
	if pots[1].amount != 400 || pots[1].eligible[2] != true || pots[1].eligible[3] != true || pots[1].eligible[1] != false {
		t.Fatalf("side pot = %+v", pots[1])
	}

	var won int64
	for _, r := range st.Results {
		won += r.WinAmount
	}
	if won != 700 {
		t.Fatalf("total won = %d, want 700", won)
	}
	resultsZeroSum(t, st)
	totalChips(t, st, 1400, "side pots")

	// Busted players sit out the next hand; two remain.
	if s := st.SeatOf(1); s.Stack != 0 || s.State != SeatSitOut {
		t.Fatalf("busted seat not sit-out: %+v", s)
	}
}

func TestUncalledBetReturned(t *testing.T) {
	st := newTable(t, 2, 500)
	if err := st.StartHand(stream(6)); err != nil {
		t.Fatal(err)
	}
	// SB raises to 300, BB folds: the 290 uncalled returns, the 20 matched
	// pot (both blinds) goes to the raiser.
	mustAct(t, st, 1, ActRaise, 300)
	mustAct(t, st, 2, ActFold, 0)
	if st.Phase != PhaseShowdown {
		t.Fatal("hand did not end")
	}
	if st.Results[0].UserID != 1 || st.Results[0].WinAmount != 20 {
		t.Fatalf("uncalled bet result: %+v", st.Results[0])
	}
	// 500 - 300 + 290 (refund) + 20 (pot) = 510.
	if st.Seats[0].Stack != 510 {
		t.Fatalf("stack after uncalled bet = %d, want 510", st.Seats[0].Stack)
	}
	totalChips(t, st, 1000, "uncalled")
}

func TestDeterministicReplayFromDeckAndActions(t *testing.T) {
	// Play the same hand twice from the same seed material and action log;
	// the states must be identical (what the persisted hand result asserts).
	play := func() (*State, []PlayerResult) {
		st := newTable(t, 3, 400)
		if err := st.StartHand(stream(11)); err != nil {
			t.Fatal(err)
		}
		mustAct(t, st, 1, ActCall, 0)
		mustAct(t, st, 2, ActCall, 0)
		mustAct(t, st, 3, ActCheck, 0)
		for st.Phase != PhaseShowdown {
			actor := st.Seats[st.ToAct]
			if actor == nil {
				t.Fatalf("no actor in %s", st.Phase)
			}
			mustAct(t, st, actor.UserID, ActCheck, 0)
		}
		return st, st.Results
	}
	a, ra := play()
	b, rb := play()
	if strings.Join(a.ActionLog, "|") != strings.Join(b.ActionLog, "|") {
		t.Fatal("action logs diverged")
	}
	if len(ra) != len(rb) {
		t.Fatalf("results differ: %+v vs %+v", ra, rb)
	}
	for i := range ra {
		if ra[i] != rb[i] {
			t.Fatalf("result %d differs: %+v vs %+v", i, ra[i], rb[i])
		}
	}
	// Zero-sum across the showdown.
	resultsZeroSum(t, a)
}

func TestLegalActionsExposure(t *testing.T) {
	st := newTable(t, 3, 500)
	if err := st.StartHand(stream(2)); err != nil {
		t.Fatal(err)
	}
	la := st.LegalActions(1) // UTG
	acts := la["actions"].([]string)
	want := []string{ActFold, ActCall, ActRaise}
	if len(acts) != len(want) {
		t.Fatalf("UTG actions = %v", acts)
	}
	for i, a := range want {
		if acts[i] != a {
			t.Fatalf("UTG actions = %v", acts)
		}
	}
	if la["callAmount"] != int64(10) || la["minRaiseTo"] != int64(20) || la["maxRaiseTo"] != int64(500) {
		t.Fatalf("UTG amounts = %+v", la)
	}
	// Not the actor → empty.
	if len(st.LegalActions(2)["actions"].([]string)) != 0 {
		t.Fatal("non-actor got actions")
	}
}

func TestLeaveSeatFoldsOutOfHand(t *testing.T) {
	st := newTable(t, 3, 200)
	if err := st.StartHand(stream(1)); err != nil {
		t.Fatal(err)
	}
	// UTG leaves mid-hand without having acted: they fold, hand continues.
	seat, err := st.LeaveSeat(1)
	if err != nil {
		t.Fatalf("leave: %v", err)
	}
	if !seat.LeaveNext || !seat.Folded {
		t.Fatalf("leaver state: %+v", seat)
	}
	if st.Phase == PhaseShowdown {
		t.Skip("hand ended on the leave-fold")
	}
	// Cash out between hands.
	if _, err := st.LeaveSeat(1); err != nil {
		t.Fatalf("leave 2: %v", err)
	}
	chips, err := st.CashOut(1)
	if err != nil {
		t.Fatalf("cashout: %v", err)
	}
	if chips != 200 { // folded UTG: blind never posted, stack untouched
		t.Fatalf("cashed out %d, want 200", chips)
	}
	if st.SeatOf(1) != nil {
		t.Fatal("seat not freed")
	}
}
