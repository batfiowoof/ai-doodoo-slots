package httpapi

import (
	"net/http"
	"strconv"

	"github.com/ai-doodoo-slots/services/backend/internal/social"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
)

// handleChatHistory serves the newest chat slice for dock mounting
// (authentication required; guests included).
func (s *Server) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	if s.currentUser(r) == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	limit := int32(50)
	if lv := r.URL.Query().Get("limit"); lv != "" {
		v, err := strconv.ParseInt(lv, 10, 32)
		if err != nil || v < 1 || v > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be 1-100")
			return
		}
		limit = int32(v)
	}
	rows, err := store.New(s.pool).ListRecentChatMessages(r.Context(), limit)
	if err != nil {
		s.logger.Error("chat history", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	// Oldest-first so the client list renders top-down.
	msgs := make([]map[string]any, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		msgs = append(msgs, social.HistoryPayload(rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// leaderboard windows map onto interval literals; all-time uses a span no
// bet can outlive.
var leaderboardWindows = map[string]string{
	"daily":  "24 hours",
	"weekly": "7 days",
	"all":    "1000 years",
}

// handleLeaderboard serves the top-20 aggregate for one metric and window
// plus the caller's own rank, computed from the same rows.
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if s.currentUser(r) == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "biggest_win"
	}
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "weekly"
	}
	span, ok := leaderboardWindows[window]
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "window must be daily, weekly or all")
		return
	}
	q := store.New(s.pool)
	type entry struct {
		userID        int64
		displayName   string
		avatarPreset  string
		avatarVersion int64
		value         int64
	}
	var rows []entry
	switch metric {
	case "biggest_win":
		raw, err := q.LeaderboardBiggestWin(r.Context(), span)
		if err != nil {
			s.logger.Error("leaderboard", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		for _, x := range raw {
			rows = append(rows, entry{x.UserID, x.DisplayName, x.AvatarPreset.String, x.AvatarVersion, x.Value})
		}
	case "net_profit":
		raw, err := q.LeaderboardNetProfit(r.Context(), span)
		if err != nil {
			s.logger.Error("leaderboard", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		for _, x := range raw {
			rows = append(rows, entry{x.UserID, x.DisplayName, x.AvatarPreset.String, x.AvatarVersion, x.Value})
		}
	case "wagered":
		raw, err := q.LeaderboardWagered(r.Context(), span)
		if err != nil {
			s.logger.Error("leaderboard", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		for _, x := range raw {
			rows = append(rows, entry{x.UserID, x.DisplayName, x.AvatarPreset.String, x.AvatarVersion, x.Value})
		}
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "metric must be biggest_win, net_profit or wagered")
		return
	}

	const topN = 20
	entries := make([]map[string]any, 0, topN)
	for i := 0; i < len(rows) && i < topN; i++ {
		x := rows[i]
		entries = append(entries, map[string]any{
			"rank":          i + 1,
			"userId":        x.userID,
			"displayName":   x.displayName,
			"avatarPreset":  x.avatarPreset,
			"avatarVersion": x.avatarVersion,
			"value":         x.value,
		})
	}

	// Own rank comes from the full aggregate, not the truncated top slice.
	var me map[string]any
	if su := s.currentUser(r); su != nil {
		for i, x := range rows {
			if x.userID == su.UserID {
				me = map[string]any{"rank": i + 1, "value": x.value}
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metric":  metric,
		"window":  window,
		"entries": entries,
		"me":      me,
	})
}

// playerStats decorates the public profile with the leaderboard-adjacent
// numbers the player card shows.
func (s *Server) playerStats(w http.ResponseWriter, r *http.Request, userID int64) (map[string]any, bool) {
	biggest, err := store.New(s.pool).GetUserBiggestWin(r.Context(), userID)
	if err != nil {
		s.logger.Error("player biggest win", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return nil, false
	}
	return map[string]any{"biggestWin": biggest}, true
}
