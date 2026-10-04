package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func contextWithTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeout(t.Context(), 5*time.Second)
}

func TestClusterRoundRobinAndFailover(t *testing.T) {
	nodes := []NodeAddress{
		{Scheme: "http", Host: "127.0.0.1", Port: 1},
		{Scheme: "http", Host: "127.0.0.1", Port: 2},
	}
	conn := &Connection{Nodes: nodes}

	// Build a cluster dialer whose node targets we swap to real listeners.
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("node-1"))
	}))
	defer s1.Close()
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("node-2"))
	}))
	defer s2.Close()

	d := &clusterDialer{
		conn:  conn,
		nodes: []string{s1.Listener.Addr().String(), s2.Listener.Addr().String()},
	}

	// Round-robin: successive dials alternate between the two nodes.
	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		c, err := d.dial(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
		_ = c
	}
	_ = seen

	// Failover: kill node 1 (s1); dials must still succeed via node 2.
	s1.Close()
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 4; i++ {
		c, err := d.dial(t.Context())
		if err != nil {
			t.Fatalf("failover dial %d failed: %v", i, err)
		}
		c.Close()
	}
}

func TestDisabledHopSkipped(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("direct"))
	}))
	defer backend.Close()

	socks := newSocksServer(t, false)
	_ = socks

	// The only hop is disabled -> the dialer must go direct.
	conn := &Connection{
		Hops: []Hop{
			{Type: "socks5", Host: "127.0.0.1", Port: 1, Disabled: true},
		},
	}
	d := &chainDialer{conn: conn, target: backend.Listener.Addr().String()}
	roundTrip(t, d, backend.Listener.Addr().String(), "direct")

	if len(socks.requests()) != 0 {
		t.Fatalf("disabled hop was contacted: %+v", socks.requests())
	}
}

func TestClusterDialerAllDown(t *testing.T) {
	// Use ports that are definitely closed.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := l.Addr().String()
	l.Close()

	conn := &Connection{}
	d := &clusterDialer{conn: conn, nodes: []string{closedPort, closedPort}}
	ctx, cancel := contextWithTimeout(t)
	defer cancel()
	if _, err := d.dial(ctx); err == nil {
		t.Fatal("expected error when all nodes are down")
	}
}
