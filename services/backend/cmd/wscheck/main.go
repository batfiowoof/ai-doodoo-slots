// Command wscheck verifies the crash-room realtime contract against a live
// gameserver: two connections observe identical tick sequences for a full
// round, and a reconnecting client lands on a fresh snapshot. Usage:
//
//	go run ./cmd/wscheck [baseURL]   (default http://localhost:8082)
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/gorilla/websocket"
)

type message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func main() {
	mode := "sync"
	base := "http://localhost:8082"
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "sync" || args[0] == "lobby" || args[0] == "holdem") {
		mode = args[0]
		args = args[1:]
	}
	if len(args) > 0 {
		base = args[0]
	}
	var err error
	switch mode {
	case "lobby":
		err = checkLobby(base)
	case "holdem":
		err = checkHoldem(base)
	default:
		err = run(base)
	}
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
	switch mode {
	case "lobby":
		fmt.Println("OK: idle lobby received ~1 summary/sec and zero round ticks")
	case "holdem":
		fmt.Println("OK: holdem table observed identical state on both sockets, hand settled, reconnect got fresh state")
	default:
		fmt.Println("OK: sockets in sync, rejoin snapshot received")
	}
}

// checkLobby is the phase-16 gate: an idle lobby client receives roughly
// one message per second and zero round ticks, regardless of room churn.
func checkLobby(base string) error {
	conn, _, err := dial(base)
	if err != nil {
		return err
	}
	defer conn.Close()
	send(conn, "subscribe_lobby", nil)

	summaries := 0
	ticks := 0
	var lastSummary map[string]any
	deadline := clock.Real{}.Now().Add(6 * time.Second)
	for (clock.Real{}).Now().Before(deadline) {
		_ = conn.SetReadDeadline((clock.Real{}).Now().Add(time.Until(deadline)))
		var m message
		if err := conn.ReadJSON(&m); err != nil {
			break // read deadline reached: window over
		}
		fmt.Println("MSG:", m.Type)
		switch m.Type {
		case "lobby_summary":
			summaries++
			_ = json.Unmarshal(m.Payload, &lastSummary)
		case "round_tick":
			ticks++
		}
	}
	if summaries < 4 {
		return fmt.Errorf("only %d lobby summaries in ~6s", summaries)
	}
	if ticks != 0 {
		return fmt.Errorf("lobby received %d round ticks — ticks must never fan out to the lobby", ticks)
	}
	raw, _ := json.Marshal(lastSummary)
	if len(raw) > 300 {
		raw = raw[:300]
	}
	fmt.Printf("summaries=%d ticks=0 last=%s\n", summaries, raw)
	return nil
}

func dial(base string) (*websocket.Conn, int64, error) {
	resp, err := http.Post(base+"/api/v1/auth/guest", "application/json", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("guest: %w", err)
	}
	var guest struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&guest)
	cookies := resp.Cookies()
	resp.Body.Close()

	wsURL := "ws" + base[len("http"):] + "/api/v1/ws"
	hdr := http.Header{}
	for _, c := range cookies {
		hdr.Add("Cookie", c.Name+"="+c.Value)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		return nil, 0, fmt.Errorf("dial: %w", err)
	}
	return conn, guest.User.ID, nil
}

func run(base string) error {
	a, _, err := dial(base)
	if err != nil {
		return err
	}
	defer a.Close()
	b, _, err := dial(base)
	if err != nil {
		return err
	}
	defer b.Close()

	for _, c := range []*websocket.Conn{a, b} {
		send(c, "join_room", map[string]string{"slug": "crash-1"})
	}

	// Collect ticks from both for one full round (~13-25s).
	stop := 200 // 40s at 200ms budget
	var ticksA, ticksB []float64
	var sawResult, sawRejoin bool
	counts := map[string]int{}
	errs := []string{}
	var countsMu sync.Mutex

	// Reader for A; on round_result it reconnects (fresh connection inside
	// the same goroutine) and expects a snapshot — the rejoin gate.
	go func() {
		conn := a
		for stop > 0 {
			stop--
			var m message
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			countsMu.Lock()
			counts[m.Type]++
			if m.Type == "error" {
				errs = append(errs, string(m.Payload))
			}
			countsMu.Unlock()
			switch m.Type {
			case "round_tick":
				var p struct {
					Multiplier float64 `json:"multiplier"`
				}
				if json.Unmarshal(m.Payload, &p) == nil {
					ticksA = append(ticksA, p.Multiplier)
				}
			case "round_result":
				sawResult = true
				conn.Close()
				nc, _, err := dial(base)
				if err != nil {
					return
				}
				conn = nc
				send(conn, "join_room", map[string]string{"slug": "crash-1"})
				sawRejoin = true
			case "room_snapshot":
				sawRejoin = sawRejoin && true
			}
		}
	}()
	go func() {
		for stop > 0 {
			stop--
			var m message
			if err := b.ReadJSON(&m); err != nil {
				return
			}
			countsMu.Lock()
			counts[m.Type]++
			if m.Type == "error" {
				errs = append(errs, string(m.Payload))
			}
			countsMu.Unlock()
			if m.Type == "round_tick" {
				var p struct {
					Multiplier float64 `json:"multiplier"`
				}
				if json.Unmarshal(m.Payload, &p) == nil {
					ticksB = append(ticksB, p.Multiplier)
				}
			}
		}
	}()

	// Also place a bet from each socket during betting_open.
	time.Sleep(500 * time.Millisecond)
	send(a, "place_bet", map[string]any{"credits": 5, "autoCashout": 1.5, "idempotencyKey": "wscheck-a"})
	send(b, "place_bet", map[string]any{"credits": 5, "autoCashout": 1.5, "idempotencyKey": "wscheck-b"})

	for stop > 0 {
		stop--
		if sawResult && sawRejoin && len(ticksA) > 0 && len(ticksB) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !sawResult {
		countsMu.Lock()
		h := fmt.Sprint(counts)
		countsMu.Unlock()
		return fmt.Errorf("no round_result observed; messages: %s, errors: %v", h, errs)
	}
	if !sawRejoin {
		return fmt.Errorf("reconnect after result failed")
	}
	// Both sockets must have observed the identical tick sequence.
	n := min(len(ticksA), len(ticksB))
	if n == 0 {
		return fmt.Errorf("no ticks observed")
	}
	for i := 0; i < n; i++ {
		if ticksA[i] != ticksB[i] {
			return fmt.Errorf("tick %d diverged: %v vs %v", i, ticksA[i], ticksB[i])
		}
	}
	countsMu.Lock()
	fmt.Printf(`ticks compared: %d (a=%d b=%d), messages: %v, errors: %v\n`, n, len(ticksA), len(ticksB), counts, errs)
	countsMu.Unlock()
	return nil
}

func send(c *websocket.Conn, typ string, payload any) {
	raw, _ := json.Marshal(map[string]any{"type": typ, "payload": payload})
	_ = c.WriteMessage(websocket.TextMessage, raw)
}

// checkHoldem is the poker gate: two players join a holdem room, buy in
// over game_action, play out one full hand (timeout-driven), observe
// identical table_state broadcasts from the moment both are seated, and a
// reconnecting player receives a fresh personalized state with hole cards.
func checkHoldem(base string) error {
	const room = "holdem-1"
	a, uidA, err := dial(base)
	if err != nil {
		return err
	}
	defer a.Close()
	b, uidB, err := dial(base)
	if err != nil {
		return err
	}
	defer b.Close()

	for _, c := range []*websocket.Conn{a, b} {
		send(c, "join_room", map[string]string{"slug": room})
	}

	var mu sync.Mutex
	var statesA, statesB []string
	var handResults int
	var sawStart bool
	var sawStrangerMasked, sawOwnCards bool
	var errs []string
	connA := a
	done := make(chan struct{})
	var reconnect func() error

	// Reader A: drives the reconnect gate on the first hand_result. The
	// connection swaps on reconnect; each read takes the current one.
	go func() {
		defer close(done)
		deadline := clock.Real{}.Now().Add(90 * time.Second)
		for (clock.Real{}).Now().Before(deadline) {
			mu.Lock()
			conn := connA
			mu.Unlock()
			_ = conn.SetReadDeadline((clock.Real{}).Now().Add(time.Until(deadline)))
			var m message
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			mu.Lock()
			switch m.Type {
			case "table_state":
				statesA = append(statesA, string(m.Payload))
			case "hand_started":
				sawStart = true
			case "hand_result":
				handResults++
			case "error":
				errs = append(errs, string(m.Payload))
			case "game_ack":
				// The reconnected socket is a stranger at the table: its
				// state pull must show zero hole cards.
				var p struct {
					Phase string `json:"phase"`
					Seats []struct {
						Cards string `json:"cards"`
					} `json:"seats"`
				}
				if json.Unmarshal(m.Payload, &p) == nil {
					any := false
					for _, seat := range p.Seats {
						if seat.Cards != "" {
							any = true
						}
					}
					if !any {
						sawStrangerMasked = true
					}
				}
			}
			mu.Unlock()
			if m.Type == "hand_result" && reconnect != nil {
				if err := reconnect(); err != nil {
					return
				}
				// B is still seated: pull its personalized state.
				send(b, "game_action", map[string]any{"action": "state"})
			}
			mu.Lock()
			finished := handResults >= 1 && sawStrangerMasked && sawOwnCards
			mu.Unlock()
			if finished {
				return
			}
		}
	}()
	// Reader B.
	go func() {
		deadline := clock.Real{}.Now().Add(90 * time.Second)
		for (clock.Real{}).Now().Before(deadline) {
			_ = b.SetReadDeadline((clock.Real{}).Now().Add(time.Until(deadline)))
			var m message
			if err := b.ReadJSON(&m); err != nil {
				return
			}
			mu.Lock()
			switch m.Type {
			case "table_state":
				statesB = append(statesB, string(m.Payload))
			case "game_ack":
				// A seated player's own pull must include their hole cards.
				var p struct {
					Seats []struct {
						UserID int64  `json:"userId"`
						Cards  string `json:"cards"`
					} `json:"seats"`
				}
				if json.Unmarshal(m.Payload, &p) == nil {
					for _, seat := range p.Seats {
						if seat.UserID == uidB && len(seat.Cards) == 4 {
							sawOwnCards = true
						}
					}
				}
			case "error":
				errs = append(errs, string(m.Payload))
			}
			mu.Unlock()
		}
	}()

	// Reconnect A with a fresh socket + join + personalized state pull. The
	// new guest identity is NOT seated; the state pull must come back with
	// zero hole cards — A's original seat shows cards only to its owner, so
	// the ack proves per-user masking both ways (own cards visible to the
	// owner, hidden to strangers).
	reconnect = func() error {
		mu.Lock()
		old := connA
		mu.Unlock()
		old.Close()
		nc, _, err := dial(base)
		if err != nil {
			return err
		}
		send(nc, "join_room", map[string]string{"slug": room})
		time.Sleep(200 * time.Millisecond)
		send(nc, "game_action", map[string]any{"action": "state"})
		mu.Lock()
		connA = nc
		mu.Unlock()
		return nil
	}

	// Readers are up: buy in (holdem-1 BB 10 → min buy-in 200).
	time.Sleep(300 * time.Millisecond)
	send(a, "game_action", map[string]any{"action": "buy_in", "amount": 200, "idempotencyKey": fmt.Sprintf("wscheck-%d", uidA)})
	send(b, "game_action", map[string]any{"action": "buy_in", "amount": 200, "idempotencyKey": fmt.Sprintf("wscheck-%d", uidB)})

	select {
	case <-done:
	case <-time.After(95 * time.Second):
	}

	mu.Lock()
	defer mu.Unlock()
	if len(errs) > 0 {
		return fmt.Errorf("socket errors: %v", errs)
	}
	if !sawStart {
		return fmt.Errorf("no hand started within the window")
	}
	if handResults < 1 {
		return fmt.Errorf("no hand_result observed (states a=%d b=%d)", len(statesA), len(statesB))
	}
	if !sawStrangerMasked {
		return fmt.Errorf("stranger state pull was not fully masked")
	}
	if !sawOwnCards {
		return fmt.Errorf("seated player never received own hole cards")
	}

	// Both sockets must have observed identical table_state broadcasts from
	// the first state in which both players were seated (earlier states
	// predate the second buy-in by design).
	both := func(state string) bool {
		return strings.Contains(state, fmt.Sprintf(`"userId":%d`, uidA)) &&
			strings.Contains(state, fmt.Sprintf(`"userId":%d`, uidB))
	}
	var syncedA, syncedB []string
	for _, st := range statesA {
		if both(st) {
			syncedA = append(syncedA, st)
		}
	}
	for _, st := range statesB {
		if both(st) {
			syncedB = append(syncedB, st)
		}
	}
	n := min(len(syncedA), len(syncedB))
	if n < 2 {
		return fmt.Errorf("too few shared table_states to compare (a=%d b=%d)", len(syncedA), len(syncedB))
	}
	for i := 0; i < n; i++ {
		if syncedA[i] != syncedB[i] {
			return fmt.Errorf("table_state %d diverged between sockets; A=%s B=%s", i, syncedA[i], syncedB[i])
		}
	}

	// Leave cleanly (cash out the seated player; the stranger is a no-op).
	send(b, "game_action", map[string]any{"action": "leave"})
	time.Sleep(300 * time.Millisecond)

	fmt.Printf("shared states compared: %d (a=%d b=%d), hand_results=%d\n", n, len(syncedA), len(syncedB), handResults)
	return nil
}
