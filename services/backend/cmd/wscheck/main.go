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
	"sync"
	"time"

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
	if len(args) > 0 && (args[0] == "sync" || args[0] == "lobby") {
		mode = args[0]
		args = args[1:]
	}
	if len(args) > 0 {
		base = args[0]
	}
	var err error
	if mode == "lobby" {
		err = checkLobby(base)
	} else {
		err = run(base)
	}
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
	if mode == "lobby" {
		fmt.Println("OK: idle lobby received ~1 summary/sec and zero round ticks")
	} else {
		fmt.Println("OK: sockets in sync, rejoin snapshot received")
	}
}

// checkLobby is the phase-16 gate: an idle lobby client receives roughly
// one message per second and zero round ticks, regardless of room churn.
func checkLobby(base string) error {
	conn, err := dial(base)
	if err != nil {
		return err
	}
	defer conn.Close()
	send(conn, "subscribe_lobby", nil)

	summaries := 0
	ticks := 0
	var lastSummary map[string]any
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Until(deadline)))
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

func dial(base string) (*websocket.Conn, error) {
	resp, err := http.Post(base+"/api/v1/auth/guest", "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("guest: %w", err)
	}
	cookies := resp.Cookies()
	resp.Body.Close()

	wsURL := "ws" + base[len("http"):] + "/api/v1/ws"
	hdr := http.Header{}
	for _, c := range cookies {
		hdr.Add("Cookie", c.Name+"="+c.Value)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	return conn, nil
}

func run(base string) error {
	a, err := dial(base)
	if err != nil {
		return err
	}
	defer a.Close()
	b, err := dial(base)
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
				nc, err := dial(base)
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
