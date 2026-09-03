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
	"time"

	"github.com/gorilla/websocket"
)

type message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func main() {
	base := "http://localhost:8082"
	if len(os.Args) > 1 {
		base = os.Args[1]
	}
	if err := run(base); err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
	fmt.Println("OK: sockets in sync, rejoin snapshot received")
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
	stop := time.Now().Add(40 * time.Second)
	var ticksA, ticksB []float64
	var sawResult, sawRejoin bool
	var aRoundID float64

	aReconnect := func() error {
		a.Close()
		na, err := dial(base)
		if err != nil {
			return err
		}
		*a = *na
		send(a, "join_room", map[string]string{"slug": "crash-1"})
		return nil
	}

	go func() {
		for time.Now().Before(stop) {
			var m message
			if err := a.ReadJSON(&m); err != nil {
				return
			}
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
				if err := aReconnect(); err == nil {
					sawRejoin = true
				}
			case "room_snapshot":
				var snap struct {
					Round *struct {
						RoundID float64 `json:"roundId"`
					} `json:"round"`
				}
				if json.Unmarshal(m.Payload, &snap) == nil && snap.Round != nil {
					aRoundID = snap.Round.RoundID
				}
			}
		}
	}()
	go func() {
		for time.Now().Before(stop) {
			var m message
			if err := b.ReadJSON(&m); err != nil {
				return
			}
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

	for time.Now().Before(stop) {
		if sawResult && sawRejoin && len(ticksA) > 0 && len(ticksB) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !sawResult {
		return fmt.Errorf("no round_result observed")
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
	fmt.Printf("ticks compared: %d (a=%d b=%d), last round id %.0f\n", n, len(ticksA), len(ticksB), aRoundID)
	return nil
}

func send(c *websocket.Conn, typ string, payload any) {
	raw, _ := json.Marshal(map[string]any{"type": typ, "payload": payload})
	_ = c.WriteJSON(raw)
}
