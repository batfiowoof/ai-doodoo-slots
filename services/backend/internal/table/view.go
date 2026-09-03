package table

import (
	"github.com/ai-doodoo-slots/services/backend/internal/game/poker"
)

// viewFor renders the room's state for one viewer (0 = masked, for
// broadcasts and spectators). Hole cards appear only for the viewer's own
// seat or seats revealed at showdown.
func (r *Runner) viewFor(userID int64) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.viewLocked(r.state, userID)
}

// viewLocked renders the state; caller holds the lock.
func (r *Runner) viewLocked(st *poker.State, userID int64) map[string]any {
	if st == nil {
		return map[string]any{"phase": poker.PhaseWaiting}
	}
	seats := make([]map[string]any, 0, len(st.Seats))
	for _, s := range st.Seats {
		if s.State == poker.SeatEmpty {
			continue
		}
		cards := ""
		if s.Showed || (userID != 0 && s.UserID == userID && s.State == poker.SeatPlaying && !s.Folded) {
			cards = s.Cards
		}
		seats = append(seats, map[string]any{
			"seatNo": s.SeatNo, "userId": s.UserID, "displayName": s.DisplayName,
			"state": s.State, "stack": s.Stack + s.Rebuy, "bet": s.Bet,
			"totalBet": s.TotalBet, "folded": s.Folded, "allIn": s.AllIn,
			"lastAction": s.LastAction, "cards": cards,
		})
	}
	view := map[string]any{
		"gameId": poker.GameID,
		"phase":  st.Phase, "handNo": st.HandNo,
		"button": st.Button, "sb": st.SB, "bb": st.BB,
		"board": splitCards(st.Board), "pot": st.Pot,
		"currentBet": st.CurrentBet, "minRaise": st.MinRaise,
		"toAct": st.ToAct, "seats": seats,
	}
	if st.Phase == poker.PhaseShowdown && st.Results != nil {
		results := make([]map[string]any, 0, len(st.Results))
		for _, res := range st.Results {
			results = append(results, map[string]any{
				"userId": res.UserID, "displayName": res.DisplayName,
				"cards": res.Cards, "handName": res.HandName,
				"winAmount": res.WinAmount, "contributed": res.Contributed, "net": res.Net,
			})
		}
		view["results"] = results
		view["actions"] = st.ActionLog
	}
	if st.ToAct >= 0 && userID != 0 {
		view["legal"] = st.LegalActions(userID)
	}
	return view
}

// recordLocked builds the full-reveal hand record and the settled-player
// list for persistence; caller holds the lock.
func (r *Runner) recordLocked(st *poker.State) (HandRecord, []SettledPlayer) {
	rec := HandRecord{
		HandNo:   st.HandNo,
		Board:    splitCards(st.Board),
		Seats:    make([]SeatRecord, 0, len(st.Seats)),
		Results:  make([]PlayerResultRecord, 0, len(st.Results)),
		Actions:  append([]string(nil), st.ActionLog...),
	}
	won := make(map[int64]int64)
	settled := make([]SettledPlayer, 0, len(st.Results))
	for _, res := range st.Results {
		won[res.UserID] = res.WinAmount
		rec.Results = append(rec.Results, PlayerResultRecord{
			UserID: res.UserID, DisplayName: res.DisplayName, Cards: res.Cards,
			HandName: res.HandName, WinAmount: res.WinAmount,
			Contributed: res.Contributed, Net: res.Net,
		})
	}
	for _, s := range st.Seats {
		if s.State == poker.SeatEmpty || s.TotalBet <= 0 {
			continue
		}
		rec.Seats = append(rec.Seats, SeatRecord{
			SeatNo: s.SeatNo, UserID: s.UserID, DisplayName: s.DisplayName,
			Cards: s.Cards, Folded: s.Folded, Showed: s.Showed,
			Contributed: s.TotalBet, WinAmount: won[s.UserID], StackAfter: s.Stack,
		})
		settled = append(settled, SettledPlayer{
			UserID: s.UserID, Contributed: s.TotalBet, WinAmount: won[s.UserID],
		})
	}
	return rec, settled
}

func splitCards(list string) []string {
	if list == "" {
		return []string{}
	}
	out := make([]string, 0, len(list)/2)
	for i := 0; i+1 < len(list); i += 2 {
		out = append(out, list[i:i+2])
	}
	return out
}
