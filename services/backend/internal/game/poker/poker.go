// Package poker is the multiplayer Texas Hold'em engine: a pure, replayable
// state machine over seats and chips. All randomness comes from the
// shuffled deck derived once at hand start from the shared fair stream;
// actions never draw. The full hand replays from (deck order + seat layout
// + action log), which is what the persisted hand result records.
//
// Deal-order contract (verification depends on it): after the Fisher-Yates
// shuffle, hole cards are dealt two per seat clockwise starting left of the
// button, in seat order; the board is the next five cards, revealed by
// street (flop = 3, turn = 1, river = 1). No burns.
package poker

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ai-doodoo-slots/services/backend/internal/cards"
	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// GameID identifies the game in rooms and bets.
const GameID = "holdem"

// Hand streets / table phases.
const (
	PhaseWaiting  = "waiting"  // between hands: seats can change freely
	PhasePreflop  = "preflop"
	PhaseFlop     = "flop"
	PhaseTurn     = "turn"
	PhaseRiver    = "river"
	PhaseShowdown = "showdown" // hand complete, results computed
)

// Seat lifecycle.
const (
	SeatEmpty   = "empty"
	SeatSitOut  = "sitout"  // seated but not in the current hand
	SeatPlaying = "playing" // dealt in
)

// Action kinds (wire values).
const (
	ActFold  = "fold"
	ActCheck = "check"
	ActCall  = "call"
	ActBet   = "bet"
	ActRaise = "raise"
)

// Engine errors. The intake maps these to wire codes.
var (
	ErrNoHand         = errors.New("no hand in progress")
	ErrHandInProgress = errors.New("hand in progress")
	ErrNotYourTurn    = errors.New("not your turn")
	ErrNotSeated      = errors.New("not seated at this table")
	ErrSeatTaken      = errors.New("seat taken")
	ErrTableFull      = errors.New("table full")
	ErrNeedPlayers    = errors.New("need at least two players with chips")
	ErrIllegalAction  = errors.New("action not allowed")
	ErrBadAmount      = errors.New("invalid amount")
)

// Seat is one chair at the table. Stack chips are house-held: debited from
// the wallet at buy-in, credited back on cash-out.
type Seat struct {
	SeatNo      int    `json:"seatNo"`
	UserID      int64  `json:"userId"`
	DisplayName string `json:"displayName"`
	State       string `json:"state"`
	Stack       int64  `json:"stack"`

	// Per-hand fields (reset by StartHand).
	Cards      string `json:"cards"` // hole cards as codes; masked in views
	Folded     bool   `json:"folded"`
	AllIn      bool   `json:"allIn"`
	Bet        int64  `json:"bet"`       // committed this street
	TotalBet   int64  `json:"totalBet"`  // committed this hand
	Acted      bool   `json:"-"`         // acted since the last full raise
	Showed     bool   `json:"showed"`    // revealed at showdown
	LastAction string `json:"lastAction"`
	LeaveNext  bool   `json:"-"`         // cash out after the hand
	Rebuy      int64  `json:"-"`         // chips queued mid-hand, applied at StartHand
}

// PlayerResult is one player's outcome of a completed hand.
type PlayerResult struct {
	UserID      int64  `json:"userId"`
	DisplayName string `json:"displayName"`
	Cards       string `json:"cards"`
	HandName    string `json:"handName,omitempty"`
	WinAmount   int64  `json:"winAmount"` // chips received from the pots
	Contributed int64  `json:"contributed"`
	Net         int64  `json:"net"`
}

// State is the full authoritative table state.
type State struct {
	Phase     string  `json:"phase"`
	HandNo    int64   `json:"handNo"`
	Seats     []*Seat `json:"seats"`
	Button    int     `json:"button"` // seat index; -1 before the first hand
	SB        int64   `json:"sb"`
	BB        int64   `json:"bb"`
	Board     string  `json:"board"` // revealed board cards (codes)
	Deck      string  `json:"-"`     // remaining deck after hole cards + burns-by-street reveals
	BoardRest string  `json:"-"`     // unrevealed board cards, in reveal order
	Pot       int64   `json:"pot"`   // chips in play = sum of TotalBet

	CurrentBet int64 `json:"currentBet"` // street high water
	MinRaise   int64 `json:"minRaise"`
	ToAct      int   `json:"toAct"` // seat index; -1 when nobody acts

	Results   []PlayerResult `json:"results,omitempty"` // set at showdown
	ActionLog []string       `json:"-"`                 // "u<id>:<action>[:<n>]"
}

// NewState builds an empty table.
func NewState(capacity int, sb, bb int64) *State {
	seats := make([]*Seat, capacity)
	for i := range seats {
		seats[i] = &Seat{SeatNo: i, State: SeatEmpty}
	}
	return &State{Phase: PhaseWaiting, Seats: seats, Button: -1, SB: sb, BB: bb, ToAct: -1}
}

// SeatOf returns the seat a user occupies, or nil.
func (st *State) SeatOf(userID int64) *Seat {
	for _, s := range st.Seats {
		if s.State != SeatEmpty && s.UserID == userID {
			return s
		}
	}
	return nil
}

// Occupied counts non-empty seats.
func (st *State) Occupied() int {
	n := 0
	for _, s := range st.Seats {
		if s.State != SeatEmpty {
			n++
		}
	}
	return n
}

// SeatPlayer takes a seat (first free unless seatNo given). Chips arrive via
// the runner's buy-in before this call. Joins mid-hand wait for the next
// hand.
func (st *State) SeatPlayer(userID int64, name string, chips int64, seatNo int) (*Seat, error) {
	if st.SeatOf(userID) != nil {
		return nil, ErrSeatTaken
	}
	pick := func(i int) error {
		s := st.Seats[i]
		if s.State != SeatEmpty {
			return ErrSeatTaken
		}
		s.UserID, s.DisplayName, s.Stack = userID, name, chips
		s.State = SeatSitOut
		return nil
	}
	if seatNo >= 0 {
		if seatNo >= len(st.Seats) {
			return nil, ErrTableFull
		}
		if err := pick(seatNo); err != nil {
			return nil, err
		}
		return st.Seats[seatNo], nil
	}
	for i := range st.Seats {
		if st.Seats[i].State == SeatEmpty {
			if err := pick(i); err != nil {
				return nil, err
			}
			return st.Seats[i], nil
		}
	}
	return nil, ErrTableFull
}

// AddChips queues a rebuy/add-on; applied at hand start so chip counts never
// change mid-hand.
func (st *State) AddChips(userID, chips int64) error {
	s := st.SeatOf(userID)
	if s == nil {
		return ErrNotSeated
	}
	s.Rebuy += chips
	return nil
}

// LeaveSeat marks a player for cash-out. Out of a hand: immediate. In a
// hand: fold now, chips return when the hand settles.
func (st *State) LeaveSeat(userID int64) (*Seat, error) {
	s := st.SeatOf(userID)
	if s == nil {
		return nil, ErrNotSeated
	}
	if st.Phase == PhaseWaiting || s.State != SeatPlaying {
		return s, nil // caller clears the seat
	}
	// In the hand: check/fold out of turn so the seat frees at settlement.
	if st.ToAct == s.SeatNo {
		_ = st.applyTimeout(s)
	} else {
		s.Folded = true
		s.LastAction = ActFold
		st.logAction(s, "leave-fold")
		if st.countLive() == 1 {
			st.finishByFold()
		}
	}
	s.LeaveNext = true
	return s, nil
}

// StartHand begins the next hand. Requires two seated players with chips.
func (st *State) StartHand(s *fair.Stream) error {
	if st.Phase != PhaseWaiting && st.Phase != PhaseShowdown {
		return ErrHandInProgress
	}
	// Apply queued rebuys.
	for _, seat := range st.Seats {
		if seat.State != SeatEmpty && seat.Rebuy > 0 {
			seat.Stack += seat.Rebuy
			seat.Rebuy = 0
		}
	}
	eligible := 0
	for _, seat := range st.Seats {
		if seat.State != SeatEmpty && seat.Stack > 0 {
			eligible++
		}
	}
	if eligible < 2 {
		return ErrNeedPlayers
	}

	st.HandNo++
	st.Button = st.nextOccupied(st.Button)
	st.resetHand()

	// Shuffle and deal: two cards per playing seat clockwise from the button.
	deck := cards.NewDeck()
	cards.Shuffle(deck, s)
	hole := 0
	st.Deck = cards.CardsString(deck)
	st.Phase = PhasePreflop

	players := st.playingOrder() // clockwise from left of the button
	for _, seat := range players {
		seat.State = SeatPlaying
		seat.Cards = st.Deck[hole*4 : hole*4+4]
		hole++
	}
	rest := deck[len(players)*2:]
	st.BoardRest = cards.CardsString(rest[:5])
	st.Deck = cards.CardsString(rest[5:])

	st.postBlinds()
	return nil
}

// playingOrder returns seats that will be dealt in, clockwise starting left
// of the button. Only seats with chips participate.
func (st *State) playingOrder() []*Seat {
	var out []*Seat
	n := len(st.Seats)
	for i := 1; i <= n; i++ {
		seat := st.Seats[(st.Button+i)%n]
		if seat.State != SeatEmpty && seat.Stack > 0 {
			out = append(out, seat)
		}
	}
	return out
}

// nextOccupied finds the next seat with chips clockwise (button rotation).
func (st *State) nextOccupied(from int) int {
	n := len(st.Seats)
	for i := 1; i <= n; i++ {
		idx := (from + i + n) % n
		s := st.Seats[idx]
		if s.State != SeatEmpty && s.Stack > 0 {
			return idx
		}
	}
	// First hand: any occupied seat.
	for i, s := range st.Seats {
		if s.State != SeatEmpty && s.Stack > 0 {
			return i
		}
	}
	return 0
}

func (st *State) resetHand() {
	st.Board = ""
	st.Pot = 0
	st.CurrentBet = 0
	st.MinRaise = st.BB
	st.Results = nil
	st.ActionLog = nil
	for _, s := range st.Seats {
		if s.State == SeatEmpty {
			continue
		}
		s.Cards = ""
		s.Folded = false
		s.AllIn = false
		s.Bet = 0
		s.TotalBet = 0
		s.Acted = false
		s.Showed = false
		s.LastAction = ""
		if s.Stack <= 0 {
			s.State = SeatSitOut // busted: rides until rebuy or leave
		}
	}
}

// postBlinds rotates by player count. liveSeats is clockwise from left of
// the button, so in a full game players[0] is the small blind; heads-up the
// button posts the small blind (players[1]) and acts first preflop.
func (st *State) postBlinds() {
	players := st.liveSeats()
	var sbSeat, bbSeat *Seat
	if len(players) == 2 {
		sbSeat, bbSeat = players[1], players[0] // button posts SB
	} else {
		sbSeat, bbSeat = players[0], players[1]
	}
	st.post(sbSeat, st.SB)
	st.post(bbSeat, st.BB)
	st.CurrentBet = st.BB
	st.MinRaise = st.BB
	if len(players) == 2 {
		st.ToAct = sbSeat.SeatNo
	} else {
		st.ToAct = players[2].SeatNo // UTG
	}
	// Blinds may have put the designated actor (or everyone) all-in; settle
	// who can actually act, running the board out if nobody can.
	st.ToAct = st.firstCapableFrom(st.ToAct)
	if st.ToAct < 0 {
		st.closeStreet()
	}
}

// firstCapableFrom scans clockwise from idx (inclusive) for the first seat
// that still owes action. Returns -1 when the betting round is complete.
func (st *State) firstCapableFrom(idx int) int {
	n := len(st.Seats)
	for i := 0; i < n; i++ {
		s := st.Seats[(idx+i)%n]
		if s.State != SeatPlaying || s.Folded || s.AllIn {
			continue
		}
		if !s.Acted || s.Bet < st.CurrentBet {
			return (idx + i) % n
		}
	}
	return -1
}

// liveSeats returns non-folded players clockwise from left of the button.
func (st *State) liveSeats() []*Seat {
	n := len(st.Seats)
	var out []*Seat
	for i := 1; i <= n; i++ {
		s := st.Seats[(st.Button+i)%n]
		if s.State == SeatPlaying && !s.Folded {
			out = append(out, s)
		}
	}
	return out
}

// post charges a blind (or portion when short-stacked).
func (st *State) post(s *Seat, amount int64) {
	if amount > s.Stack {
		amount = s.Stack
	}
	s.Stack -= amount
	s.Bet += amount
	s.TotalBet += amount
	st.Pot += amount
	if s.Stack == 0 {
		s.AllIn = true
	}
	s.LastAction = "blind"
}

// countLive is the number of players still holding cards.
func (st *State) countLive() int {
	n := 0
	for _, s := range st.Seats {
		if s.State == SeatPlaying && !s.Folded {
			n++
		}
	}
	return n
}

// Action is one player's move. For bet/raise, Amount is the total street
// commitment (raise-to semantics).
type Action struct {
	UserID int64
	Kind   string
	Amount int64
}

// Act validates and applies a player action, then advances the hand.
func (st *State) Act(a Action) error {
	if st.Phase == PhaseWaiting || st.Phase == PhaseShowdown {
		return ErrNoHand
	}
	if st.ToAct < 0 {
		return ErrNoHand
	}
	seat := st.SeatOf(a.UserID)
	if seat == nil || seat.State != SeatPlaying || seat.Folded || seat.AllIn {
		return ErrNotYourTurn
	}
	if seat.SeatNo != st.ToAct {
		return ErrNotYourTurn
	}
	switch a.Kind {
	case ActFold:
		seat.Folded = true
		seat.LastAction = ActFold
		st.logAction(seat, ActFold)
	case ActCheck:
		if st.CurrentBet != seat.Bet {
			return ErrIllegalAction // must call, raise, or fold
		}
		seat.LastAction = ActCheck
		st.logAction(seat, ActCheck)
	case ActCall:
		need := st.CurrentBet - seat.Bet
		if need <= 0 {
			return ErrIllegalAction // nothing to call; check instead
		}
		pay := min64(need, seat.Stack)
		st.commit(seat, pay)
		seat.LastAction = ActCall
		st.logAction(seat, ActCall)
	case ActBet, ActRaise:
		if a.Amount <= seat.Bet {
			return ErrBadAmount
		}
		add := a.Amount - seat.Bet
		if add > seat.Stack {
			return ErrBadAmount // all-in short of the target is a call, not a raise-to
		}
		fullRaise := a.Amount >= st.CurrentBet+st.MinRaise
		isOpenBet := st.CurrentBet == 0
		if isOpenBet {
			if a.Amount < st.BB && add < seat.Stack {
				return ErrBadAmount // opening bet must be at least the big blind
			}
		} else if !fullRaise && add < seat.Stack {
			return ErrBadAmount // raise must be a full raise or all-in
		}
		st.commit(seat, add)
		seat.LastAction = a.Kind
		st.logAction(seat, a.Kind, a.Amount)
		if fullRaise || isOpenBet {
			// A full raise reopens action for everyone else.
			st.MinRaise = a.Amount - st.CurrentBet
			if st.MinRaise < st.BB {
				st.MinRaise = st.BB
			}
			for _, o := range st.Seats {
				if o != seat && o.State == SeatPlaying && !o.Folded && !o.AllIn {
					o.Acted = false
				}
			}
		}
		// CurrentBet tracks the high water even for short all-ins; callers
		// must still match it.
		if seat.Bet > st.CurrentBet {
			st.CurrentBet = seat.Bet
		}
	default:
		return ErrIllegalAction
	}
	seat.Acted = true
	if seat.Stack == 0 && !seat.Folded {
		seat.AllIn = true
	}
	st.advance()
	return nil
}

// commit moves chips from a seat's stack into the pot.
func (st *State) commit(s *Seat, amount int64) {
	if amount > s.Stack {
		amount = s.Stack
	}
	s.Stack -= amount
	s.Bet += amount
	s.TotalBet += amount
	st.Pot += amount
}

func (st *State) logAction(s *Seat, parts ...any) {
	var b strings.Builder
	fmt.Fprintf(&b, "u%d", s.UserID)
	for _, p := range parts {
		fmt.Fprintf(&b, ":%v", p)
	}
	st.ActionLog = append(st.ActionLog, b.String())
}

// advance moves the hand forward: next actor, street transition, or
// showdown. Called after every action.
func (st *State) advance() {
	if st.countLive() == 1 {
		st.finishByFold()
		return
	}
	if next := st.nextActor(); next >= 0 {
		st.ToAct = next
		return
	}
	st.closeStreet()
}

// nextActor finds the next seat that still owes action, scanning clockwise
// from the seat after the current actor. Returns -1 when the betting round
// is complete.
func (st *State) nextActor() int {
	n := len(st.Seats)
	return st.firstCapableFrom((st.ToAct + 1) % n)
}

// closeStreet ends the betting round and deals the next street, running out
// the board automatically when no more action is possible.
func (st *State) closeStreet() {
	for _, s := range st.Seats {
		if s.State == SeatPlaying {
			s.Bet = 0
			s.Acted = false
		}
	}
	st.CurrentBet = 0
	st.MinRaise = st.BB
	st.ToAct = -1

	canAct := 0
	for _, s := range st.Seats {
		if s.State == SeatPlaying && !s.Folded && !s.AllIn {
			canAct++
		}
	}
	runout := canAct <= 1 // everyone all-in: reveal the rest

	switch st.Phase {
	case PhasePreflop:
		st.Phase = PhaseFlop
		st.revealBoard(3)
	case PhaseFlop:
		st.Phase = PhaseTurn
		st.revealBoard(1)
	case PhaseTurn:
		st.Phase = PhaseRiver
		st.revealBoard(1)
	case PhaseRiver:
		st.showdown()
		return
	}
	if runout && st.Phase != PhaseShowdown {
		st.closeStreet() // keep dealing until the river settles it
		return
	}
	// Postflop action opens left of the button; everyone's Acted flag was
	// reset above so firstCapableFrom finds them.
	st.ToAct = st.firstCapableFrom((st.Button + 1) % len(st.Seats))
}

// revealBoard moves n cards from the hidden remainder onto the board.
func (st *State) revealBoard(n int) {
	if len(st.BoardRest) < n*2 {
		return
	}
	st.Board += st.BoardRest[:n*2]
	st.BoardRest = st.BoardRest[n*2:]
}

// finishByFold awards the matched pot to the last player holding cards.
func (st *State) finishByFold() {
	st.Phase = PhaseShowdown
	st.ToAct = -1
	st.refundUncalled()
	winner := st.liveSeats()[0]
	st.Results = []PlayerResult{{
		UserID: winner.UserID, DisplayName: winner.DisplayName,
		Cards: winner.Cards, WinAmount: st.Pot,
		Contributed: winner.TotalBet, Net: st.Pot - winner.TotalBet,
	}}
	winner.Stack += st.Pot
	for _, s := range st.Seats {
		if s.State == SeatPlaying {
			s.Showed = true // full transparency, per house rules
		}
	}
	st.settleHand()
}

// showdown evaluates every live hand and splits the side pots.
func (st *State) showdown() {
	st.Phase = PhaseShowdown
	st.ToAct = -1
	st.refundUncalled()
	board, _ := cards.ParseCards(st.Board)

	type live struct {
		seat *Seat
		eval cards.Eval
	}
	var hands []live
	for _, s := range st.liveSeats() {
		hole, _ := cards.ParseCards(s.Cards)
		hands = append(hands, live{seat: s, eval: cards.Evaluate(append(append([]cards.Card{}, hole...), board...))})
		s.Showed = true
	}

	win := make(map[int64]int64)    // userID -> chips won
	pots := st.buildPots()
	for _, pot := range pots {
		var best []live
		for _, h := range hands {
			if pot.eligible[h.seat.UserID] {
				if len(best) == 0 || h.eval.Better(best[0].eval) {
					best = []live{h}
				} else if !best[0].eval.Better(h.eval) {
					best = append(best, h)
				}
			}
		}
		if len(best) == 0 {
			continue
		}
		share := pot.amount / int64(len(best))
		rem := pot.amount - share*int64(len(best))
		for i, h := range best {
			amt := share
			if i == 0 && rem > 0 {
				amt += rem // odd chip to the first winner in position order
			}
			win[h.seat.UserID] += amt
		}
	}

	st.Results = make([]PlayerResult, 0, len(hands))
	for _, s := range st.Seats {
		if s.State != SeatPlaying {
			continue
		}
		var handName string
		for _, h := range hands {
			if h.seat == s {
				handName = h.eval.Name
			}
		}
		w := win[s.UserID]
		s.Stack += w
		st.Results = append(st.Results, PlayerResult{
			UserID: s.UserID, DisplayName: s.DisplayName, Cards: s.Cards,
			HandName: handName, WinAmount: w,
			Contributed: s.TotalBet, Net: w - s.TotalBet,
		})
	}
	st.settleHand()
}

type pot struct {
	amount   int64
	eligible map[int64]bool
}

// buildPots layers side pots at each distinct all-in contribution level.
// Folded chips belong to the pot but not the eligibility. The top layer with
// a single contributor returns that player's uncalled bet naturally.
func (st *State) buildPots() []pot {
	var levels []int64
	for _, s := range st.Seats {
		if s.State != SeatEmpty && s.TotalBet > 0 {
			levels = append(levels, s.TotalBet)
		}
	}
	if len(levels) == 0 {
		return nil
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	dedup := levels[:1]
	for _, l := range levels[1:] {
		if l != dedup[len(dedup)-1] {
			dedup = append(dedup, l)
		}
	}

	var pots []pot
	prev := int64(0)
	for _, level := range dedup {
		amount := int64(0)
		eligible := make(map[int64]bool)
		for _, s := range st.Seats {
			if s.State == SeatEmpty || s.TotalBet <= 0 {
				continue
			}
			// Chips this player puts into the [prev, level] layer: their
			// total clipped to the layer's bounds.
			amount += min64(s.TotalBet, level) - min64(s.TotalBet, prev)
			if !s.Folded && s.TotalBet >= level {
				eligible[s.UserID] = true
			}
		}
		if amount > 0 {
			pots = append(pots, pot{amount: amount, eligible: eligible})
		}
		prev = level
	}
	return pots
}

// settleHand finalizes the hand's chip movement. The phase stays Showdown —
// a terminal "hand over" state with Results populated — until the next
// StartHand moves the table back into play.
func (st *State) settleHand() {
	st.Pot = 0
	st.BoardRest = ""
	st.Deck = ""
	st.ToAct = -1
	for _, s := range st.Seats {
		if s.State == SeatEmpty {
			continue
		}
		if s.State == SeatPlaying {
			s.State = SeatSitOut
		}
		s.Bet = 0
		if s.Stack <= 0 && !s.LeaveNext && s.Rebuy <= 0 {
			// Busted stack stays seated as sit-out; the client offers rebuy.
		}
	}
}

// refundUncalled returns the un-matched excess of the largest contribution
// to its owner before pots are layered, so an uncalled raise never feeds
// the pot (and never displays as winnings). Folded chips are dead money:
// they count toward the call level but never come back.
func (st *State) refundUncalled() {
	var top *Seat
	var topTotal, second int64
	for _, s := range st.Seats {
		if s.State != SeatPlaying || s.TotalBet <= 0 {
			continue
		}
		if s.TotalBet > topTotal {
			second = topTotal
			topTotal = s.TotalBet
			if s.Folded {
				top = nil // a dead bet is never refunded
			} else {
				top = s
			}
		} else if s.TotalBet > second {
			second = s.TotalBet
		}
	}
	if top == nil || topTotal <= second {
		return
	}
	refund := topTotal - second
	top.Stack += refund
	top.TotalBet -= refund
	st.Pot -= refund
}

// CashOut returns the chips a leaving player takes and frees the seat.
// Called by the runner between hands.
func (st *State) CashOut(userID int64) (int64, error) {
	s := st.SeatOf(userID)
	if s == nil {
		return 0, ErrNotSeated
	}
	chips := s.Stack + s.Rebuy
	s.Stack, s.Rebuy = 0, 0
	s.State = SeatEmpty
	s.UserID, s.DisplayName = 0, ""
	s.Folded, s.AllIn, s.LeaveNext = false, false, false
	s.Cards, s.LastAction = "", ""
	return chips, nil
}

// LeaveChips reports chips a leave-marked player will cash out (runner calls
// CashOut after reading results).
func (st *State) LeavingSeats() []*Seat {
	var out []*Seat
	for _, s := range st.Seats {
		if s.State != SeatEmpty && s.LeaveNext {
			out = append(out, s)
		}
	}
	return out
}

// applyTimeout acts for an absent player: check when free, else fold.
func (st *State) applyTimeout(seat *Seat) error {
	if st.CurrentBet == seat.Bet {
		seat.LastAction = ActCheck
		seat.Acted = true
		st.logAction(seat, "timeout-check")
	} else {
		seat.Folded = true
		seat.LastAction = ActFold
		st.logAction(seat, "timeout-fold")
	}
	if st.countLive() == 1 {
		st.finishByFold()
		return nil
	}
	if next := st.nextActor(); next >= 0 {
		st.ToAct = next
	} else {
		st.closeStreet()
	}
	return nil
}

// Timeout drives the turn timer for whoever is to act.
func (st *State) Timeout() error {
	if st.ToAct < 0 || st.Phase == PhaseWaiting || st.Phase == PhaseShowdown {
		return ErrNoHand
	}
	return st.applyTimeout(st.Seats[st.ToAct])
}

// LegalActions reports what the player to act may do (for the action bar).
func (st *State) LegalActions(userID int64) map[string]any {
	out := map[string]any{"actions": []string{}}
	if st.ToAct < 0 {
		return out
	}
	seat := st.SeatOf(userID)
	if seat == nil || seat.SeatNo != st.ToAct {
		return out
	}
	acts := []string{ActFold}
	need := st.CurrentBet - seat.Bet
	if need <= 0 {
		acts = append(acts, ActCheck)
	} else {
		acts = append(acts, ActCall)
		if need > seat.Stack {
			out["callAmount"] = seat.Stack
		} else {
			out["callAmount"] = need
		}
	}
	if seat.Stack > need {
		if st.CurrentBet == 0 {
			acts = append(acts, ActBet) // opening bet
		} else {
			acts = append(acts, ActRaise)
		}
		minTo := st.CurrentBet + st.MinRaise
		if st.CurrentBet == 0 {
			minTo = st.BB
		}
		if minTo < st.BB {
			minTo = st.BB
		}
		if seat.Stack+seat.Bet < minTo {
			minTo = seat.Stack + seat.Bet // all-in short is allowed
		}
		out["minRaiseTo"] = minTo
		out["maxRaiseTo"] = seat.Bet + seat.Stack
	}
	out["actions"] = acts
	return out
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
