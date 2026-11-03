package handler

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/fasthttp/websocket"
	"github.com/google/uuid"
)

// TestLobbyLoad28And56 runs fake clients against the real WebSocket stack.
// 28 clients fill Sảnh 1. 56 overflow into Sảnh 2. Numbers are one process,
// so CPU and heap include both the server and the clients.
func TestLobbyLoad28And56(t *testing.T) {
	var lines []string
	for _, n := range []int{28, 56} {
		n := n
		t.Run(fmt.Sprintf("%d", n), func(t *testing.T) {
			line := runLobbyLoad(t, n, 3*time.Second)
			t.Log(line)
			lines = append(lines, line)
		})
	}
	_ = os.WriteFile("/tmp/lobby-load.txt", []byte(joinLines(lines)), 0o644)
}

func runLobbyLoad(t *testing.T, n int, steady time.Duration) string {
	t.Helper()
	users := make([]*domain.User, n)
	for i := range users {
		users[i] = &domain.User{
			ID:       uuid.New(),
			Email:    fmt.Sprintf("admin-%d@example.com", i),
			IsActive: true,
		}
	}
	wsBase, _, tok, hub := startLobbyServer(t, true, users...)

	clients := make([]*loadClient, n)
	var readers sync.WaitGroup
	for i, user := range users {
		access, err := tok.SignAccess(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		conn := dialLobby(t, wsBase, map[string][]string{"Authorization": {"Bearer " + access}})
		c := &loadClient{conn: conn}
		clients[i] = c
		readers.Add(1)
		go func() {
			defer readers.Done()
			c.readLoop()
		}()
		c.write([]byte(fmt.Sprintf(
			`{"v":1,"channel":"lobby","op":"join","nickname":"Ban %d","outfit":"outfit_pajamas","hair":"hair_sleep_buns","accessory":"accessory_star_clip","x":0.40,"y":0.60,"facing":"right"}`,
			i,
		)))
	}
	waitHellos(t, clients, 8*time.Second)

	lobbies, _, present := hub.Sizes()
	wantLobbies := 1
	if n > 28 {
		wantLobbies = (n + 27) / 28
	}
	if present != n || lobbies != wantLobbies {
		t.Fatalf("after join present=%d lobbies=%d, want present=%d lobbies=%d", present, lobbies, n, wantLobbies)
	}
	baseIn, baseOut, baseMsgs := clientTotals(clients)
	baseHub := hub.Counters()

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	cpuStart := processCPU()
	wallStart := time.Now()
	var peak atomic.Uint64
	peak.Store(before.HeapAlloc)
	stopSample := make(chan struct{})
	go sampleHeap(&peak, stopSample)

	var movers sync.WaitGroup
	stopMove := make(chan struct{})
	for _, c := range clients {
		movers.Add(1)
		go func(c *loadClient) {
			defer movers.Done()
			tick := time.NewTicker(600 * time.Millisecond)
			defer tick.Stop()
			x := 0.25
			for {
				select {
				case <-stopMove:
					return
				case <-tick.C:
					x += 0.07
					if x > 0.9 {
						x = 0.25
					}
					c.write([]byte(fmt.Sprintf(
						`{"v":1,"channel":"lobby","op":"move-to","x":%.2f,"y":0.62,"facing":"left","action":"walk"}`,
						x,
					)))
				}
			}
		}(c)
	}
	time.Sleep(steady)
	close(stopMove)
	movers.Wait()
	time.Sleep(400 * time.Millisecond)
	close(stopSample)

	wall := time.Since(wallStart)
	cpu := processCPU() - cpuStart
	var inBytes, outBytes, inMsgs int
	for _, c := range clients {
		c.mu.Lock()
		inBytes += c.inBytes
		outBytes += c.outBytes
		inMsgs += c.inMsgs
		c.mu.Unlock()
		_ = c.conn.Close()
	}
	readers.Wait()

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	counters := hub.Counters()
	cpuPct := 0.0
	sec := wall.Seconds()
	if sec <= 0 {
		sec = 1
	}
	if wall > 0 {
		cpuPct = float64(cpu) / float64(wall) * 100
	}
	// Steady window excludes the join burst. Bytes are JSON payloads, not WebSocket framing.
	steadyOut := outBytes - baseOut
	steadyIn := inBytes - baseIn
	return fmt.Sprintf(
		"n=%d lobbies=%d present=%d wall=%s cpu=%s cpu_pct=%.1f heap_before=%d heap_peak=%d heap_after=%d steady_client_out=%d steady_client_in=%d steady_out_Bps=%.0f steady_in_Bps=%.0f steady_msgs_in=%d hub_msgs_in=%d hub_msgs_out=%d hub_bytes_in=%d hub_bytes_out=%d rate_limited=%d gomaxprocs=%d",
		n, lobbies, present, wall.Round(time.Millisecond), cpu.Round(time.Millisecond), cpuPct,
		before.HeapAlloc, peak.Load(), after.HeapAlloc,
		steadyOut, steadyIn, float64(steadyOut)/sec, float64(steadyIn)/sec, inMsgs-baseMsgs,
		counters.In-baseHub.In, counters.Out-baseHub.Out, counters.InBytes-baseHub.InBytes, counters.OutBytes-baseHub.OutBytes,
		counters.RateLimited-baseHub.RateLimited,
		runtime.GOMAXPROCS(0),
	)
}

func clientTotals(clients []*loadClient) (inBytes, outBytes, inMsgs int) {
	for _, c := range clients {
		c.mu.Lock()
		inBytes += c.inBytes
		outBytes += c.outBytes
		inMsgs += c.inMsgs
		c.mu.Unlock()
	}
	return inBytes, outBytes, inMsgs
}

func waitHellos(t *testing.T, clients []*loadClient, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ready := 0
		for _, c := range clients {
			c.mu.Lock()
			ok := c.hello
			c.mu.Unlock()
			if ok {
				ready++
			}
		}
		if ready == len(clients) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	ready := 0
	for _, c := range clients {
		c.mu.Lock()
		if c.hello {
			ready++
		}
		c.mu.Unlock()
	}
	t.Fatalf("hellos %d/%d", ready, len(clients))
}

func sampleHeap(peak *atomic.Uint64, stop <-chan struct{}) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			for {
				old := peak.Load()
				if ms.HeapAlloc <= old || peak.CompareAndSwap(old, ms.HeapAlloc) {
					break
				}
			}
		}
	}
}

func processCPU() time.Duration {
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		return 0
	}
	return time.Duration(r.Utime.Nano()) + time.Duration(r.Stime.Nano())
}

type loadClient struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	inBytes  int
	outBytes int
	inMsgs   int
	hello    bool
}

func (c *loadClient) write(b []byte) {
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		return
	}
	c.mu.Lock()
	c.outBytes += len(b)
	c.mu.Unlock()
}

func (c *loadClient) readLoop() {
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		c.mu.Lock()
		c.inBytes += len(msg)
		c.inMsgs++
		if !c.hello && bytes.Contains(msg, []byte(`"op":"hello"`)) {
			c.hello = true
		}
		c.mu.Unlock()
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}
