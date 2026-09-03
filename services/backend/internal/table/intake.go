package table

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ai-doodoo-slots/services/backend/internal/game/poker"
	"github.com/ai-doodoo-slots/services/backend/internal/ws"
)

// Intake is the authorized path for socket game actions at a poker table.
// It validates shape and status (re-checked server-side, never trusting the
// socket), then serializes into the room's runner. Implements ws.RoomHandler.
type Intake struct {
	runner *Runner
	logger *slog.Logger
}

// NewIntake wires the intake to its table runner.
func NewIntake(r *Runner, logger *slog.Logger) *Intake {
	return &Intake{runner: r, logger: logger}
}

// gameActionPayload is the inbound socket shape for game_action messages.
// SeatNo is optional (nil = first free seat).
type gameActionPayload struct {
	Action         string `json:"action"`
	Amount         int64  `json:"amount"`
	SeatNo         *int   `json:"seatNo"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// HandleGameAction runs one authorized game action. The ack payload is the
// authoritative table state (personalized: the caller sees their own hole
// cards).
func (i *Intake) HandleGameAction(id ws.Identity, payload json.RawMessage) (map[string]any, error) {
	var p gameActionPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.Action == "" {
		return nil, coded("bad_request", fmt.Errorf("malformed game action"))
	}
	// Per-message authorization: a connection authenticated before a ban
	// must not reach money.
	if id.Status != "active" {
		return nil, coded("status_forbids_betting", ErrForbiddenStatus)
	}

	seatNo := -1
	if p.SeatNo != nil {
		seatNo = *p.SeatNo
	}
	req := request{userID: id.UserID, name: id.DisplayName, seatNo: seatNo, idemKey: p.IdempotencyKey}
	switch p.Action {
	case "buy_in":
		if p.IdempotencyKey == "" {
			return nil, coded("bad_request", fmt.Errorf("buy-in needs an idempotency key"))
		}
		req.kind = "buy_in"
		req.amount = p.Amount
	case "rebuy":
		if p.IdempotencyKey == "" {
			return nil, coded("bad_request", fmt.Errorf("rebuy needs an idempotency key"))
		}
		req.kind = "rebuy"
		req.amount = p.Amount
	case "leave":
		req.kind = "leave"
	case "state":
		req.kind = "state"
	case poker.ActFold, poker.ActCheck, poker.ActCall:
		req.kind = "act"
		req.action = p.Action
	case poker.ActBet, poker.ActRaise:
		if p.Amount <= 0 {
			return nil, coded("invalid_amount", ErrInvalidAmount)
		}
		req.kind = "act"
		req.action = p.Action
		req.amount = p.Amount
	default:
		return nil, coded("bad_request", fmt.Errorf("unknown action %q", p.Action))
	}

	// The socket caller's context is not plumbed through the RoomHandler
	// interface; the Submit timeout bounds each round trip.
	return i.runner.Submit(context.Background(), req)
}
