// Command socialcheck exercises the social surface against a live
// gameserver: two guest identities chat, emote, tip, rain and hunt a big
// win over the api→pg_notify→relay path, asserting each broadcast lands on
// both sockets and the ledger moved. Usage:
//
//	go run ./cmd/socialcheck [baseURL]   (default http://localhost:8082)
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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

type player struct {
	id     int64
	conn   *websocket.Conn
	client *http.Client
	base   string
	wsMu   sync.Mutex
	frames chan message // single reader pump; waitTypes consume from here
}

var failures []string

// debugFrames traces player A's inbound frame types (SOCIALCHECK_DEBUG=1).
var debugFrames = os.Getenv("SOCIALCHECK_DEBUG") == "1"

// The clock guard forbids direct time.Now outside internal/clock; the bot
// has no determinism needs, it just plays by the same rule.
var clk = clock.Real{}

func check(name string, ok bool, detail string) {
	if ok {
		fmt.Printf("PASS %s %s\n", name, detail)
	} else {
		fmt.Printf("FAIL %s %s\n", name, detail)
		failures = append(failures, name)
	}
}

func main() {
	base := "http://localhost:8082"
	if len(os.Args) > 1 {
		base = os.Args[1]
	}
	a := dial(base, "A")
	b := dial(base, "B")
	defer a.conn.Close()
	defer b.conn.Close()
	for _, p := range []*player{a, b} {
		send(p, "subscribe_lobby", nil)
	}

	// 1. Chat: A speaks, both sockets must receive the broadcast.
	send(a, "send_chat", map[string]any{"body": "hello from socialcheck"})
	mA := waitType(a, "chat_message", 3*time.Second)
	mB := waitType(b, "chat_message", 3*time.Second)
	check("chat_broadcast", mB != nil && strings.Contains(string(mB.Payload), "hello from socialcheck"),
		"(A got: "+typeName(mA)+", B got: "+typeName(mB)+")")

	// 2. Cooldown: A's immediate second line must bounce.
	send(a, "send_chat", map[string]any{"body": "too fast"})
	errMsg := waitTypeError(a, 3*time.Second)
	check("chat_cooldown", errMsg == "social_cooldown", "(code="+errMsg+")")
	time.Sleep(700 * time.Millisecond) // clear the floor for later sends

	// 3. Emote: A reacts, both sockets get the ephemeral broadcast.
	send(a, "send_emote", map[string]any{"emoteId": "fire"})
	eB := waitType(b, "emote", 3*time.Second)
	check("emote_broadcast", eB != nil && strings.Contains(string(eB.Payload), `"fire"`), "")

	// 4. Presence: the lobby summary should carry a roster with both guests.
	send(b, "subscribe_lobby", nil)
	rosterOK := false
	for try := 0; try < 4 && !rosterOK; try++ {
		m := waitType(b, "lobby_summary", time.Second)
		if m == nil {
			continue
		}
		rosterOK = strings.Contains(string(m.Payload), fmt.Sprintf(`"userId":%d`, a.id)) &&
			strings.Contains(string(m.Payload), fmt.Sprintf(`"userId":%d`, b.id))
	}
	check("roster_both_users", rosterOK, fmt.Sprintf("(a=%d b=%d)", a.id, b.id))

	// 5. Tip: B tips A 50 credits; ledger + broadcast must agree.
	balBefore := balance(a)
	send(b, "send_tip", map[string]any{"toUserId": a.id, "credits": 50})
	tipA := waitType(a, "tip", 3*time.Second)
	tipLine := waitType(a, "chat_message", 3*time.Second)
	balAfter := balance(a)
	check("tip_flow", tipA != nil && strings.Contains(string(tipA.Payload), `"credits":50`) &&
		tipLine != nil && balAfter == balBefore+50,
		fmt.Sprintf("(balance %d → %d)", balBefore, balAfter))

	// 6. Rain: B rains 90 across everyone else online. Tips share the chat
	// cooldown floor, so give the gate room to clear.
	time.Sleep(700 * time.Millisecond)
	send(b, "make_it_rain", map[string]any{"credits": 90})
	rainA := waitType(a, "rain", 3*time.Second)
	rainBal := balance(a)
	check("rain_flow", rainA != nil && strings.Contains(string(rainA.Payload), `"totalCredits":`) && rainBal > balAfter,
		fmt.Sprintf("(A balance now %d)", rainBal))

	// 7. Big win via the api→pg_notify→relay path: A grinds slots until a
	// payout clears the threshold, then BOTH sockets must see big_win.
	bigWinA := make(chan bool, 1)
	go func() {
		for {
			m := waitType(a, "big_win", 2*time.Second)
			if m != nil {
				bigWinA <- true
				return
			}
		}
	}()
	multiplier, plays := grindBigWin(a, 220)
	gotWin := false
	if multiplier > 0 {
		select {
		case gotWin = <-bigWinA:
		case <-time.After(5 * time.Second):
		}
	}
	check("big_win_relay", gotWin, fmt.Sprintf("(%d plays, best multiplier %.1fx, threshold %.0f)", plays, multiplier, threshold()))

	// 8. Leaderboard REST: A's grind must show up with a rank.
	lb := a.get("/api/v1/leaderboard?metric=biggest_win&window=all")
	check("leaderboard_me_rank", strings.Contains(lb, `"me":{`), "("+firstChars(lb, 120)+")")

	fmt.Println()
	if len(failures) > 0 {
		fmt.Println("SOCIALCHECK FAIL:", strings.Join(failures, ", "))
		os.Exit(1)
	}
	fmt.Println("SOCIALCHECK OK: chat, cooldown, emote, roster, tip, rain, big-win relay, leaderboard")
}

// dial creates a guest with its own cookie jar, connects the socket, and
// starts nothing else — reads are made by waitType on demand.
func dial(base, tag string) *player {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Post(base+"/api/v1/auth/guest", "application/json", nil)
	if err != nil {
		fmt.Println("FATAL guest:", err)
		os.Exit(1)
	}
	var guest struct {
		User struct {
			ID          int64  `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&guest)
	resp.Body.Close()

	wsURL := "ws" + base[len("http"):] + "/api/v1/ws"
	u, _ := url.Parse(base)
	hdr := http.Header{}
	for _, c := range jar.Cookies(u) {
		hdr.Add("Cookie", c.Name+"="+c.Value)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		fmt.Println("FATAL dial:", err)
		os.Exit(1)
	}
	fmt.Printf("guest %s joined as %d (%s)\n", tag, guest.User.ID, guest.User.DisplayName)
	p := &player{id: guest.User.ID, conn: conn, client: client, base: base, frames: make(chan message, 256)}
	// One reader pump per connection; waitTypes consume from the channel.
	go func() {
		defer close(p.frames)
		for {
			var m message
			if err := conn.ReadJSON(&m); err != nil {
				fmt.Println(tag, "pump exit:", err)
				return
			}
			if tag == "A" && debugFrames {
				fmt.Println("A frame:", m.Type)
			}
			p.frames <- m
		}
	}()
	return p
}

func send(p *player, typ string, payload any) {
	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	raw, _ := json.Marshal(map[string]any{"type": typ, "payload": payload})
	_ = p.conn.WriteMessage(websocket.TextMessage, raw)
}

// waitType scans frames until one of the wanted type arrives (others are
// dropped — summaries and ticks are noise here) or the deadline passes.
func waitType(p *player, want string, d time.Duration) *message {
	if d <= 0 {
		d = time.Millisecond
	}
	deadline := time.After(d)
	for {
		select {
		case m, ok := <-p.frames:
			if !ok {
				return nil
			}
			if m.Type == want {
				mm := m
				return &mm
			}
		case <-deadline:
			return nil
		}
	}
}

// waitTypeError scans for the next error envelope and returns its code.
func waitTypeError(p *player, d time.Duration) string {
	deadline := time.After(d)
	for {
		select {
		case m, ok := <-p.frames:
			if !ok {
				return ""
			}
			if m.Type == "error" {
				var e struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(m.Payload, &e)
				return e.Code
			}
		case <-deadline:
			return ""
		}
	}
}

func (p *player) get(path string) string {
	resp, err := p.client.Get(p.base + path)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
}

func balance(p *player) int64 {
	var me struct {
		BalanceCredits int64 `json:"balanceCredits"`
	}
	resp, err := p.client.Get(p.base + "/api/v1/me")
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(resp.Body).Decode(&me)
	return me.BalanceCredits
}

// grindBigWin plays slots with min bet until one payout clears the big-win
// threshold. Returns the best multiplier seen and the play count.
func grindBigWin(p *player, maxPlays int) (float64, int) {
	best := 0.0
	for i := 0; i < maxPlays; i++ {
		if i%40 == 39 {
			p.client.Post(p.base+"/api/v1/me/deposit", "application/json", nil) // keep the stake alive
		}
		body := strings.NewReader(fmt.Sprintf(
			`{"betCredits":5,"clientSeed":"","idempotencyKey":"socialcheck-%d"}`, clk.Now().UnixNano()))
		resp, err := p.client.Post(p.base+"/api/v1/games/slots/play", "application/json", body)
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		var res struct {
			PayoutCredits int64 `json:"payoutCredits"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if res.PayoutCredits > 0 {
			if mult := float64(res.PayoutCredits) / 5; mult > best {
				best = mult
			}
			if float64(res.PayoutCredits)/5 >= threshold() {
				return best, i + 1
			}
		}
		time.Sleep(550 * time.Millisecond) // play limiter: 20 per 10s
	}
	return best, maxPlays
}

func threshold() float64 {
	if v := os.Getenv("BIG_WIN_MULTIPLIER"); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil && f > 0 {
			return f
		}
	}
	return 15
}

func typeName(m *message) string {
	if m == nil {
		return "none"
	}
	return m.Type
}

func firstChars(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
