package disco

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/stun"
)

// miniSTUN is a pocket-sized STUN responder that replies to any Binding
// request with an XOR-MAPPED-ADDRESS equal to the packet's source address.
// Used for tests — not a real server.
type miniSTUN struct {
	conn    net.PacketConn
	delay   time.Duration
	stop    chan struct{}
	stopped chan struct{}
}

func newMiniSTUN(t *testing.T, delay time.Duration) *miniSTUN {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	m := &miniSTUN{
		conn:    conn,
		delay:   delay,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go m.loop()
	return m
}

func (m *miniSTUN) addr() string { return m.conn.LocalAddr().String() }

func (m *miniSTUN) Close() {
	close(m.stop)
	_ = m.conn.Close()
	<-m.stopped
}

func (m *miniSTUN) loop() {
	defer close(m.stopped)
	buf := make([]byte, 1500)
	for {
		_ = m.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, src, err := m.conn.ReadFrom(buf)
		select {
		case <-m.stop:
			return
		default:
		}
		if err != nil {
			continue
		}
		if !stun.IsMessage(buf[:n]) {
			continue
		}
		req := &stun.Message{Raw: append([]byte{}, buf[:n]...)}
		if err := req.Decode(); err != nil {
			continue
		}
		if m.delay > 0 {
			time.Sleep(m.delay)
		}
		srcUDP, _ := src.(*net.UDPAddr)
		resp, err := stun.Build(
			stun.BindingSuccess,
			stun.NewTransactionIDSetter(req.TransactionID),
			&stun.XORMappedAddress{IP: srcUDP.IP, Port: srcUDP.Port},
			stun.Fingerprint,
		)
		if err != nil {
			continue
		}
		_, _ = m.conn.WriteTo(resp.Raw, src)
	}
}

func TestDiscoverPublic_Succeeds(t *testing.T) {
	srv := newMiniSTUN(t, 0)
	defer srv.Close()

	client, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ep, err := DiscoverPublic(ctx, client, []string{srv.addr()})
	if err != nil {
		t.Fatalf("DiscoverPublic: %v", err)
	}
	if ep.Addr.Port() == 0 {
		t.Errorf("port was zero: %v", ep.Addr)
	}
	if !strings.HasPrefix(ep.Addr.Addr().String(), "127.") {
		t.Errorf("expected loopback address, got %v", ep.Addr)
	}
	if ep.Via != srv.addr() {
		t.Errorf("Via = %q, want %q", ep.Via, srv.addr())
	}
}

func TestDiscoverPublic_Race(t *testing.T) {
	slow := newMiniSTUN(t, 500*time.Millisecond)
	defer slow.Close()
	fast := newMiniSTUN(t, 0)
	defer fast.Close()

	client, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ep, err := DiscoverPublic(ctx, client, []string{slow.addr(), fast.addr()})
	if err != nil {
		t.Fatalf("DiscoverPublic: %v", err)
	}
	if ep.Via != fast.addr() {
		t.Errorf("fast server should have won; got via=%q (fast=%q slow=%q)",
			ep.Via, fast.addr(), slow.addr())
	}
}

func TestDiscoverPublic_AllDown(t *testing.T) {
	// Two TCP listeners on loopback give us host:port pairs that nothing
	// answers UDP at; ResolveUDPAddr will succeed but no STUN responder is
	// listening, so reads time out.
	conn1, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	conn2, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	a1 := conn1.LocalAddr().String()
	a2 := conn2.LocalAddr().String()
	_ = conn1.Close()
	_ = conn2.Close()

	client, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = DiscoverPublic(ctx, client, []string{a1, a2})
	if err == nil {
		t.Fatal("expected error when no servers respond")
	}
}

func TestDiscoverPublic_Canceled(t *testing.T) {
	srv := newMiniSTUN(t, 500*time.Millisecond)
	defer srv.Close()

	client, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := DiscoverPublic(ctx, client, []string{srv.addr()})
		if err == nil {
			t.Errorf("expected cancellation error")
		}
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()
}
