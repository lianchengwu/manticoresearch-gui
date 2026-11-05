package dialer

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"manticoresearch-gui/internal/model"
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

	// greeting
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if s.auth {
		conn.Write([]byte{0x05, 0x02})
		uhead := make([]byte, 2)
		if _, err := io.ReadFull(conn, uhead); err != nil {
			return
		}
		u := make([]byte, uhead[1])
		io.ReadFull(conn, u)
		phead := make([]byte, 1)
		io.ReadFull(conn, phead)
		p := make([]byte, phead[0])
		io.ReadFull(conn, p)
		if string(u) != "user" || string(p) != "pass" {
			conn.Write([]byte{0x01, 0x01})
			return
		}
		conn.Write([]byte{0x01, 0x00})
	} else {
		conn.Write([]byte{0x05, 0x00})
	}

	// request
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return
	}
	var targetHost string
	switch req[3] {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		io.ReadFull(conn, ip)
		targetHost = net.IP(ip).String()
	case 0x03: // domain
		l := make([]byte, 1)
		io.ReadFull(conn, l)
		dom := make([]byte, l[0])
		io.ReadFull(conn, dom)
		targetHost = string(dom)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		io.ReadFull(conn, ip)
		targetHost = net.IP(ip).String()
	}
	portB := make([]byte, 2)
	io.ReadFull(conn, portB)
	port := int(portB[0])<<8 | int(portB[1])
	target := net.JoinHostPort(targetHost, fmt.Sprint(port))

	s.mu.Lock()
	s.reqs = append(s.reqs, socksReq{ver: req[0], cmd: req[1], atyp: req[3], target: target})
	s.mu.Unlock()

	upstream, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	// success reply
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})
	_ = conn.SetDeadline(time.Time{})

	go io.Copy(upstream, conn)
	io.Copy(conn, upstream)
}

// ---------------------------------------------------------------------------
// minimal HTTP CONNECT proxy

type httpProxy struct {
	t      *testing.T
	ln     net.Listener
	mu     sync.Mutex
	auth   bool
	target string
}

func newHTTPProxy(t *testing.T, auth bool) *httpProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &httpProxy{t: t, ln: ln, auth: auth}
	go p.serve()
	t.Cleanup(func() { ln.Close() })
	return p
}

func (p *httpProxy) addr() string { return p.ln.Addr().String() }

func (p *httpProxy) serve() {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.handle(conn)
	}
}

func (p *httpProxy) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		conn.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\n\r\n"))
		return
	}
	if p.auth {
		hdr := req.Header.Get("Proxy-Authorization")
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
		if hdr != expected {
			conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
			return
		}
	}
	p.mu.Lock()
	p.target = req.RequestURI
	p.mu.Unlock()

	upstream, err := net.DialTimeout("tcp", req.RequestURI, 2*time.Second)
	if err != nil {
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer upstream.Close()

	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	_ = conn.SetDeadline(time.Time{})

	go io.Copy(upstream, conn)
	io.Copy(conn, upstream)
}

// ---------------------------------------------------------------------------
// tests

func hostPort(addr string) (string, int) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, 0
	}
	h := addr[:i]
	p, _ := parsePort(addr[i+1:])
	return h, p
}

func TestDirectDialTCP(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		c, _ := echo.Accept()
		if c != nil {
			io.Copy(c, c)
			c.Close()
		}
	}()

	conn, err := DialTCP(t.Context(), echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	io.ReadFull(conn, buf)
	if string(buf) != "ping" {
		t.Fatalf("expected ping, got %s", buf)
	}
}

func TestSOCKS5HandshakeNoAuth(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	socks := newSocksServer(t, false)
	sh, sp := hostPort(socks.addr())

	c := &model.Connection{
		Host: "localhost",
		Port: 9308,
		Hops: []model.Hop{
			{Type: "socks5", Host: sh, Port: sp},
		},
	}
	cd := &ChainDialer{Conn: c, Target: backend.Listener.Addr().String()}
	conn, err := cd.Dial(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: backend\r\nConnection: close\r\n\r\n")
	resp, _ := io.ReadAll(conn)
	if !strings.Contains(string(resp), "hello from backend") {
		t.Fatalf("expected backend body, got %q", resp)
	}
}

func TestSOCKS5HandshakeUserPass(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	socks := newSocksServer(t, true)
	sh, sp := hostPort(socks.addr())

	// good auth
	c := &model.Connection{
		Hops: []model.Hop{
			{Type: "socks5", Host: sh, Port: sp, Username: "user", Password: "pass"},
		},
	}
	cd := &ChainDialer{Conn: c, Target: backend.Listener.Addr().String()}
	conn, err := cd.Dial(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	// bad auth
	cBad := &model.Connection{
		Hops: []model.Hop{
			{Type: "socks5", Host: sh, Port: sp, Username: "user", Password: "wrong"},
		},
	}
	cdBad := &ChainDialer{Conn: cBad, Target: backend.Listener.Addr().String()}
	if _, err := cdBad.Dial(t.Context()); err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestHTTPConnectHandshake(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxied"))
	}))
	defer backend.Close()

	hp := newHTTPProxy(t, false)
	hh, hport := hostPort(hp.addr())

	c := &model.Connection{
		Hops: []model.Hop{
			{Type: "http", Host: hh, Port: hport},
		},
	}
	cd := &ChainDialer{Conn: c, Target: backend.Listener.Addr().String()}
	conn, err := cd.Dial(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: backend\r\nConnection: close\r\n\r\n")
	resp, _ := io.ReadAll(conn)
	if !strings.Contains(string(resp), "proxied") {
		t.Fatalf("expected proxied body, got %q", resp)
	}
}

func TestMultiHopChain(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("end-of-chain"))
	}))
	defer backend.Close()

	s1 := newSocksServer(t, false)
	hp := newHTTPProxy(t, false)
	s2 := newSocksServer(t, false)

	h1, p1 := hostPort(s1.addr())
	h2, p2 := hostPort(hp.addr())
	h3, p3 := hostPort(s2.addr())

	c := &model.Connection{
		Hops: []model.Hop{
			{Type: "socks5", Host: h1, Port: p1},
			{Type: "http", Host: h2, Port: p2},
			{Type: "socks5", Host: h3, Port: p3},
		},
	}
	cd := &ChainDialer{Conn: c, Target: backend.Listener.Addr().String()}
	conn, err := cd.Dial(t.Context())
	if err != nil {
		t.Fatalf("chain dial failed: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: backend\r\nConnection: close\r\n\r\n")
	resp, _ := io.ReadAll(conn)
	if !strings.Contains(string(resp), "end-of-chain") {
		t.Fatalf("unexpected body: %q", resp)
	}
}

func TestTwoHopChainHTTPThenSocks(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("via-chain"))
	}))
	defer backend.Close()

	socks := newSocksServer(t, false)
	hp := newHTTPProxy(t, false)
	hh, hpPort := hostPort(hp.addr())
	sh, sp := hostPort(socks.addr())
	target := backend.Listener.Addr().String()

	c := &model.Connection{
		Hops: []model.Hop{
			{Type: "http", Host: hh, Port: hpPort},
			{Type: "socks5", Host: sh, Port: sp},
		},
	}
	conn, err := (&ChainDialer{Conn: c, Target: target}).Dial(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	data, _ := io.ReadAll(conn)
	if !strings.Contains(string(data), "via-chain") {
		t.Fatalf("response = %q", data)
	}
	reqs := socks.requests()
	if len(reqs) != 1 || reqs[0].target != target {
		t.Fatalf("socks requests = %+v, want target %s", reqs, target)
	}
}

// ---------------------------------------------------------------------------
// SSH tests

type stubAddr string

func (s stubAddr) Network() string { return "tcp" }
func (s stubAddr) String() string  { return string(s) }

func resetKnownHosts(t *testing.T) {
	t.Helper()
	SetConfigDir(t.TempDir())
	t.Cleanup(func() {
		SetConfigDir("")
	})
}

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestTOFUHostKeyNoDeadlock(t *testing.T) {
	resetKnownHosts(t)
	signer := testSigner(t)
	cb := tofuHostKeyCallback("127.0.0.1:22")

	done := make(chan error, 2)
	go func() {
		done <- cb("127.0.0.1:22", stubAddr("1.2.3.4:22"), signer.PublicKey())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TOFU host key callback deadlocked")
	}

	go func() {
		done <- cb("127.0.0.1:22", stubAddr("1.2.3.4:22"), signer.PublicKey())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TOFU second callback deadlocked")
	}
}

func TestTOFUFirstUseAcceptsAndPersists(t *testing.T) {
	resetKnownHosts(t)
	signer := testSigner(t)
	cb := tofuHostKeyCallback("remote:22")
	if err := cb("remote:22", stubAddr("1.2.3.4:22"), signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m := loadKnownHostsLocked()
	if m["remote:22"] != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("expected fingerprint saved, got %q", m["remote:22"])
	}
}

func TestTOFUMismatchRejected(t *testing.T) {
	resetKnownHosts(t)
	s1 := testSigner(t)
	s2 := testSigner(t)
	cb := tofuHostKeyCallback("remote:22")
	_ = cb("remote:22", stubAddr("1.2.3.4:22"), s1.PublicKey())

	err := cb("remote:22", stubAddr("1.2.3.4:22"), s2.PublicKey())
	if err == nil || !strings.Contains(err.Error(), "has changed") {
		t.Fatalf("expected mismatch rejection, got %v", err)
	}
}

func TestClearSSHHostKey(t *testing.T) {
	resetKnownHosts(t)
	s1 := testSigner(t)
	s2 := testSigner(t)
	cb := tofuHostKeyCallback("remote:2222")
	_ = cb("remote:2222", stubAddr("1.2.3.4:2222"), s1.PublicKey())

	if err := ClearSSHHostKey("remote", 2222); err != nil {
		t.Fatal(err)
	}
	if err := cb("remote:2222", stubAddr("1.2.3.4:2222"), s2.PublicKey()); err != nil {
		t.Fatalf("after clearing key, new key must be accepted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cluster tests

func TestClusterRoundRobinAndFailover(t *testing.T) {
	nodes := []model.NodeAddress{
		{Scheme: "http", Host: "127.0.0.1", Port: 1},
		{Scheme: "http", Host: "127.0.0.1", Port: 2},
	}
	conn := &model.Connection{Nodes: nodes}

	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("node-1"))
	}))
	defer s1.Close()
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("node-2"))
	}))
	defer s2.Close()

	d := &ClusterDialer{
		Conn:  conn,
		Nodes: []string{s1.Listener.Addr().String(), s2.Listener.Addr().String()},
	}

	for range 6 {
		c, err := d.Dial(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}

	// Failover: kill node 1 (s1); dials must still succeed via node 2.
	s1.Close()
	time.Sleep(50 * time.Millisecond)
	for i := range 4 {
		c, err := d.Dial(t.Context())
		if err != nil {
			t.Fatalf("failover dial %d failed: %v", i, err)
		}
		c.Close()
	}
}

func TestDisabledHopSkipped(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend-reached"))
	}))
	defer backend.Close()

	socks := newSocksServer(t, false)
	sh, sp := hostPort(socks.addr())

	c := &model.Connection{
		Hops: []model.Hop{
			{Type: "socks5", Host: "192.0.2.1", Port: 9999, Disabled: true},
			{Type: "socks5", Host: sh, Port: sp, Disabled: false},
			{Type: "http", Host: "192.0.2.2", Port: 8888, Disabled: true},
		},
	}
	cd := &ChainDialer{Conn: c, Target: backend.Listener.Addr().String()}
	conn, err := cd.Dial(t.Context())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: backend\r\nConnection: close\r\n\r\n")
	resp, _ := io.ReadAll(conn)
	if !strings.Contains(string(resp), "backend-reached") {
		t.Fatalf("expected backend body, got %q", resp)
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
	h, p := hostPort(addr)
	conn := &model.Connection{
		Hops: []model.Hop{{Type: hopType, Host: h, Port: p}},
	}
	d := &ChainDialer{Conn: conn, Target: "127.0.0.1:9"}
	start := time.Now()
	_, err := d.Dial(t.Context())
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

func TestClusterDialerAllDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	ln.Close()

	d := &ClusterDialer{Conn: &model.Connection{}, Nodes: []string{closed, closed}}
	if _, err := d.Dial(t.Context()); err == nil {
		t.Fatal("expected error when all nodes are down")
	}
}

func TestInvalidateClientDropsHTTPOnly(t *testing.T) {
	h := model.Hop{Type: "ssh", Host: "127.0.0.1", Port: 22, Username: "u", Password: "p"}
	conn := &model.Connection{Host: "db", Port: 9308, Hops: []model.Hop{h}}
	key := sshHopKey(&h)
	sentinel := "live-ssh"
	sshClients.Store(key, sentinel)
	t.Cleanup(func() { sshClients.Delete(key) })

	fp := Fingerprint(conn)
	httpClients.Store(fp, &clientEntry{client: &http.Client{}})
	t.Cleanup(func() { httpClients.Delete(fp) })

	InvalidateClient(conn)

	if _, ok := httpClients.Load(fp); ok {
		t.Fatal("HTTP client cache must be dropped")
	}
	v, ok := sshClients.Load(key)
	if !ok || v != sentinel {
		t.Fatal("SSH client must stay cached across HTTP invalidation")
	}
}

func TestConfigDirOverrideAndFallback(t *testing.T) {
	dir := t.TempDir()
	SetConfigDir(dir)
	t.Cleanup(func() { SetConfigDir("") })
	if got := ConfigDir(); got != dir {
		t.Fatalf("ConfigDir = %q, want %q", got, dir)
	}
	knownHosts.mu.Lock()
	gotHosts := knownHostsPathLocked()
	knownHosts.mu.Unlock()
	if want := filepath.Join(dir, "ssh_known_hosts.json"); gotHosts != want {
		t.Fatalf("known hosts = %q, want %q", gotHosts, want)
	}

	home := t.TempDir()
	got := resolveConfigDir("", func() (string, error) {
		return "", fmt.Errorf("no user config")
	}, func() (string, error) {
		return home, nil
	})
	want := filepath.Join(home, ".config", "manticoresearch-gui")
	if got != want {
		t.Fatalf("fallback = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}
