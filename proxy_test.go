package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// minimal SOCKS5 proxy (supports optional username/password auth)

type socksReq struct {
	ver, cmd, atyp byte
	target         string
}

type socksServer struct {
	t    *testing.T
	ln   net.Listener
	mu   sync.Mutex
	reqs []socksReq
	auth bool // require username/password
}

func newSocksServer(t *testing.T, auth bool) *socksServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &socksServer{t: t, ln: ln, auth: auth}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *socksServer) addr() string { return s.ln.Addr().String() }

func (s *socksServer) requests() []socksReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]socksReq(nil), s.reqs...)
}

func (s *socksServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *socksServer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	head := make([]byte, 3)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	if s.auth {
		if head[2] != 0x02 {
			conn.Write([]byte{0x05, 0xFF})
			return
		}
		conn.Write([]byte{0x05, 0x02})
		ulenB := make([]byte, 2) // ver + ulen
		if _, err := io.ReadFull(conn, ulenB); err != nil {
			return
		}
		user := make([]byte, ulenB[1])
		if _, err := io.ReadFull(conn, user); err != nil {
			return
		}
		plenB := make([]byte, 1)
		if _, err := io.ReadFull(conn, plenB); err != nil {
			return
		}
		pass := make([]byte, plenB[0])
		if _, err := io.ReadFull(conn, pass); err != nil {
			return
		}
		if string(user) != "alice" || string(pass) != "wonder" {
			conn.Write([]byte{0x01, 0x01})
			return
		}
		conn.Write([]byte{0x01, 0x00})
	} else {
		conn.Write([]byte{0x05, 0x00})
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return
	}
	var host string
	switch req[3] {
	case 0x01:
		b := make([]byte, 6)
		if _, err := io.ReadFull(conn, b); err != nil {
			return
		}
		host = net.IPv4(b[0], b[1], b[2], b[3]).String() + ":" + fmt.Sprint(int(b[4])<<8|int(b[5]))
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return
		}
		b := make([]byte, int(l[0])+2)
		if _, err := io.ReadFull(conn, b); err != nil {
			return
		}
		host = string(b[:l[0]]) + ":" + fmt.Sprint(int(b[l[0]])<<8|int(b[l[0]+1]))
	default:
		return
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, socksReq{ver: req[0], cmd: req[1], atyp: req[3], target: host})
	s.mu.Unlock()

	upstream, err := net.Dial("tcp", host)
	if err != nil {
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	_ = conn.SetDeadline(time.Time{})
	go io.Copy(upstream, conn)
	io.Copy(conn, upstream)
}

// ---------------------------------------------------------------------------
// minimal HTTP CONNECT proxy: relays to the CONNECT target like a real proxy

func httpConnectProxy(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				fields := strings.Fields(line)
				if len(fields) < 2 {
					return
				}
				for {
					l, err := br.ReadString('\n')
					if err != nil || l == "\r\n" || l == "\n" {
						break
					}
				}
				up, err := net.Dial("tcp", fields[1])
				if err != nil {
					c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					return
				}
				defer up.Close()
				c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
				go io.Copy(up, &bufferedConn{Conn: c, r: br})
				io.Copy(c, up)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

// ---------------------------------------------------------------------------

type hpT struct{ host, port string }

func hostPort(addr string) hpT {
	i := strings.LastIndex(addr, ":")
	return hpT{host: addr[:i], port: addr[i+1:]}
}

func hostOf(addr string) string { return hostPort(addr).host }
func portOf(addr string) int    { p, _ := parsePort(hostPort(addr).port); return p }

func roundTrip(t *testing.T, d *chainDialer, backendAddr string, want string) {
	t.Helper()
	c, err := d.dial(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	data, _ := io.ReadAll(c)
	if !strings.Contains(string(data), want) {
		t.Fatalf("response = %q, want substring %q", string(data), want)
	}
}

func TestSingleHopHTTPProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("via-http-proxy"))
	}))
	defer backend.Close()

	addr := httpConnectProxy(t)
	conn := &Connection{
		Hops: []Hop{{Type: "http", Host: hostOf(addr), Port: portOf(addr)}},
	}
	d := &chainDialer{conn: conn, target: backend.Listener.Addr().String()}
	roundTrip(t, d, backend.Listener.Addr().String(), "via-http-proxy")
}

func TestTwoHopChainHTTPThenSocks(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("via-chain"))
	}))
	defer backend.Close()

	socks := newSocksServer(t, false)
	httpAddr := httpConnectProxy(t)

	conn := &Connection{
		Hops: []Hop{
			{Type: "http", Host: hostOf(httpAddr), Port: portOf(httpAddr)},
			{Type: "socks5", Host: hostOf(socks.addr()), Port: portOf(socks.addr())},
		},
	}
	d := &chainDialer{conn: conn, target: backend.Listener.Addr().String()}
	roundTrip(t, d, backend.Listener.Addr().String(), "via-chain")

	reqs := socks.requests()
	if len(reqs) != 1 || reqs[0].target != backend.Listener.Addr().String() {
		t.Fatalf("socks requests = %+v", reqs)
	}
}

func TestSocks5Auth(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("auth-ok"))
	}))
	defer backend.Close()

	socks := newSocksServer(t, true)

	// wrong credentials must fail
	bad := &Connection{
		Hops: []Hop{
			{Type: "socks5", Host: hostOf(socks.addr()), Port: portOf(socks.addr()), Username: "alice", Password: "nope"},
		},
	}
	if _, err := (&chainDialer{conn: bad, target: backend.Listener.Addr().String()}).dial(t.Context()); err == nil {
		t.Fatal("expected auth failure")
	}

	good := &Connection{
		Hops: []Hop{
			{Type: "socks5", Host: hostOf(socks.addr()), Port: portOf(socks.addr()), Username: "alice", Password: "wonder"},
		},
	}
	d := &chainDialer{conn: good, target: backend.Listener.Addr().String()}
	roundTrip(t, d, backend.Listener.Addr().String(), "auth-ok")
}

func testConnectionThroughHop(t *testing.T, hop Hop) {
	t.Helper()
	m := newMockManticore(t, "", "")
	c := *newTestConn(m)
	c.Hops = []Hop{hop}
	done := make(chan error, 1)
	go func() {
		r, err := ConnectionService{}.TestConnection(c)
		if err != nil {
			done <- err
			return
		}
		if r == nil || r.Version != "9.2.14" {
			done <- fmt.Errorf("result = %+v", r)
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TestConnection hung")
	}
}

func TestHTTPProxyTestConnection(t *testing.T) {
	addr := httpConnectProxy(t)
	testConnectionThroughHop(t, Hop{Type: "http", Host: hostOf(addr), Port: portOf(addr)})
}

func TestSocks5TestConnection(t *testing.T) {
	socks := newSocksServer(t, false)
	testConnectionThroughHop(t, Hop{Type: "socks5", Host: hostOf(socks.addr()), Port: portOf(socks.addr())})
}

func TestHTTPThenSocksTestConnection(t *testing.T) {
	m := newMockManticore(t, "", "")
	c := *newTestConn(m)
	socks := newSocksServer(t, false)
	httpAddr := httpConnectProxy(t)
	c.Hops = []Hop{
		{Type: "http", Host: hostOf(httpAddr), Port: portOf(httpAddr)},
		{Type: "socks5", Host: hostOf(socks.addr()), Port: portOf(socks.addr())},
	}
	done := make(chan error, 1)
	go func() {
		r, err := ConnectionService{}.TestConnection(c)
		if err != nil {
			done <- err
			return
		}
		if r == nil || r.Version != "9.2.14" {
			done <- fmt.Errorf("result = %+v", r)
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TestConnection hung")
	}
}

func blackholeProxy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					_ = c.SetDeadline(time.Now().Add(30 * time.Second))
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func assertHandshakeTimesOut(t *testing.T, hopType, addr string) {
	t.Helper()
	conn := &Connection{
		Hops: []Hop{{Type: hopType, Host: hostOf(addr), Port: portOf(addr)}},
	}
	d := &chainDialer{conn: conn, target: "127.0.0.1:9"}
	start := time.Now()
	_, err := d.dial(t.Context())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected handshake to fail")
	}
	if elapsed < dialTimeout-2*time.Second || elapsed > dialTimeout+3*time.Second {
		t.Fatalf("%s blackhole elapsed %v, want ~%v (err=%v)", hopType, elapsed, dialTimeout, err)
	}
}

func TestBlackholeHTTPConnectTimesOut(t *testing.T) {
	t.Parallel()
	assertHandshakeTimesOut(t, "http", blackholeProxy(t))
}

func TestBlackholeSocks5TimesOut(t *testing.T) {
	t.Parallel()
	assertHandshakeTimesOut(t, "socks5", blackholeProxy(t))
}
