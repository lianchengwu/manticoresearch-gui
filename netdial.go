package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	openapi "github.com/manticoresoftware/manticoresearch-go"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"
)

// This file implements the network layer: an ordered chain of hops
// (HTTP proxy / SOCKS5 proxy / SSH tunnel), applied left to right
// (本机 → hop1 → hop2 → … → Manticore). Any mix and any order is allowed;
// disabled hops are skipped. A cluster connection additionally rotates the
// final Manticore node (round-robin with fail-over).

const dialTimeout = 15 * time.Second

// ---------------------------------------------------------------------------
// dialing helpers

func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	var nd net.Dialer
	nd.Timeout = dialTimeout
	return nd.DialContext(ctx, "tcp", addr)
}

func (h *Hop) addr() string {
	port := h.Port
	if port == 0 {
		if h.Type == "ssh" {
			port = 22
		} else {
			port = 1080
		}
	}
	return net.JoinHostPort(h.Host, fmt.Sprint(port))
}

// nextAddr returns the address this hop tunnels to. Empty remote config on
// the hop means automatic: the next enabled hop, or the final target.
func nextAddr(hops []Hop, i int, final string) string {
	if hops[i].RemoteHost != "" {
		port := hops[i].RemotePort
		if port == 0 {
			port = 9308
		}
		return net.JoinHostPort(hops[i].RemoteHost, fmt.Sprint(port))
	}
	for j := i + 1; j < len(hops); j++ {
		if !hops[j].Disabled {
			return hops[j].addr()
		}
	}
	return final
}

// tunnelConn takes a conn that has reached hop i and tunnels it to the next
// chain address. Returns the tunneled conn (never nil on nil error) and a
// closer for resources that must outlive the connection (SSH client).
func tunnelConn(hops []Hop, i int, conn net.Conn, final string) (net.Conn, io.Closer, error) {
	h := &hops[i]
	next := nextAddr(hops, i, final)
	switch strings.ToLower(h.Type) {
	case "ssh":
		return sshHopTunnel(h, conn, next)
	case "http":
		c, err := httpConnectHandshake(h, conn, next)
		return c, nil, err
	default:
		c, err := socks5Handshake(h, conn, next)
		return c, nil, err
	}
}

// chainDialer builds conns to a target address through the connection's
// enabled hop chain.
type chainDialer struct {
	conn   *Connection
	target string // final host:port (a Manticore node or an SSH-in-chain server)
}

func (d *chainDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported network %q", network)
	}
	return d.dial(ctx)
}

func (d *chainDialer) dial(ctx context.Context) (net.Conn, error) {
	hops := make([]Hop, 0, len(d.conn.Hops))
	for _, h := range d.conn.Hops {
		if !h.Disabled {
			hops = append(hops, h)
		}
	}
	if len(hops) == 0 {
		return dialTCP(ctx, d.target)
	}

	var conn net.Conn
	var err error
	var closers []io.Closer
	defer func() {
		if err != nil {
			if conn != nil {
				conn.Close()
			}
			for _, c := range closers {
				c.Close()
			}
		}
	}()

	// First hop: plain TCP to its address (later hops are reached through
	// the tunnel built so far).
	conn, err = dialTCP(ctx, hops[0].addr())
	if err != nil {
		return nil, fmt.Errorf("connect to %s %s: %w", hops[0].Type, hops[0].addr(), err)
	}
	for i := range hops {
		var closer io.Closer
		var c net.Conn
		c, closer, err = tunnelConn(hops, i, conn, d.target)
		if closer != nil {
			closers = append(closers, closer)
		}
		if err != nil {
			return nil, fmt.Errorf("via %s %s: %w", hops[i].Type, hops[i].addr(), err)
		}
		conn = c
	}
	return conn, nil
}

// ---------------------------------------------------------------------------
// SOCKS5 (RFC 1928)

func socks5Handshake(h *Hop, conn net.Conn, dest string) (net.Conn, error) {
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	method := byte(0x00) // no auth
	if h.Username != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, 0x01, method}); err != nil {
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	if resp[0] != 0x05 {
		return nil, fmt.Errorf("not a SOCKS5 proxy")
	}
	switch resp[1] {
	case 0x00:
	case 0x02:
		if h.Username == "" {
			return nil, fmt.Errorf("proxy requires username/password auth")
		}
		auth := append([]byte{0x01, byte(len(h.Username))}, h.Username...)
		auth = append(auth, byte(len(h.Password)))
		auth = append(auth, h.Password...)
		if _, err := conn.Write(auth); err != nil {
			return nil, err
		}
		arep := make([]byte, 2)
		if _, err := io.ReadFull(conn, arep); err != nil {
			return nil, err
		}
		if arep[1] != 0x00 {
			return nil, fmt.Errorf("proxy auth failed")
		}
	default:
		return nil, fmt.Errorf("proxy rejected auth method 0x%02x", resp[1])
	}

	host, portStr, err := net.SplitHostPort(dest)
	if err != nil {
		return nil, err
	}
	port, err := parsePort(portStr)
	if err != nil {
		return nil, err
	}
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 0x01)
			req = append(req, v4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return nil, err
	}
	if head[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 connect failed: %s", socks5Error(head[1]))
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	default:
		skip = -1 // domain: first byte is length
	}
	if skip < 0 {
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return nil, err
		}
		skip = int(l[0])
	}
	tail := make([]byte, skip+2)
	if _, err := io.ReadFull(conn, tail); err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func socks5Error(code byte) string {
	switch code {
	case 0x01:
		return "general failure"
	case 0x02:
		return "connection not allowed"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	}
	return fmt.Sprintf("reply code %d", code)
}

func httpConnectHandshake(h *Hop, conn net.Conn, dest string) (net.Conn, error) {
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	req := "CONNECT " + dest + " HTTP/1.1\r\nHost: " + dest + "\r\n"
	if h.Username != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(h.Username + ":" + h.Password))
		req += "Proxy-Authorization: Basic " + cred + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(status)
	if len(fields) < 2 || fields[1] != "200" {
		return nil, fmt.Errorf("HTTP proxy CONNECT failed: %s", strings.TrimSpace(status))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return &bufferedConn{Conn: conn, r: br}, nil
}

// bufferedConn keeps bytes already buffered by the handshake reader.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }

func parsePort(s string) (int, error) {
	var p int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid port %q", s)
		}
		p = p*10 + int(c-'0')
		if p > 65535 {
			return 0, fmt.Errorf("invalid port %q", s)
		}
	}
	if p == 0 {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// SSH hop

// sshClients caches live SSH clients per hop so repeated requests reuse the
// authenticated session instead of re-handshaking per connection.
var sshClients sync.Map // string (hop fingerprint) -> *ssh.Client

func sshHopKey(h *Hop) string {
	data, _ := json.Marshal(struct {
		Host, User, Pass, KeyPath, Passphrase string
		Port                                  int
	}{h.Host, h.Username, h.Password, h.KeyPath, h.KeyPassphrase, h.Port})
	return string(data)
}

func invalidateSSHHop(h *Hop) {
	if v, ok := sshClients.Load(sshHopKey(h)); ok {
		v.(*ssh.Client).Close()
		sshClients.Delete(sshHopKey(h))
	}
}

func sshAuthMethods(h *Hop) ([]ssh.AuthMethod, error) {
	switch h.AuthType {
	case "key":
		if h.KeyPath == "" {
			return nil, fmt.Errorf("SSH 私钥路径为空")
		}
		pem, err := os.ReadFile(expandHome(h.KeyPath))
		if err != nil {
			return nil, err
		}
		var signer ssh.Signer
		if h.KeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(h.KeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(pem)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return []ssh.AuthMethod{ssh.Password(h.Password)}, nil
	}
}

// sshHopTunnel performs the SSH handshake over conn and opens a channel to
// next. The ssh.Client is cached per hop and reused for later requests.
func sshHopTunnel(h *Hop, conn net.Conn, next string) (net.Conn, io.Closer, error) {
	key := sshHopKey(h)
	if v, ok := sshClients.Load(key); ok {
		client := v.(*ssh.Client)
		ch, err := client.Dial("tcp", next)
		if err == nil {
			return ch, client, nil
		}
		// stale client: drop and re-handshake below
		client.Close()
		sshClients.Delete(key)
	}

	addr := h.addr()
	user := h.Username
	if user == "" {
		user = "root"
	}
	auth, err := sshAuthMethods(h)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	sshCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: tofuHostKeyCallback(addr),
		Timeout:         dialTimeout,
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	client := ssh.NewClient(c, chans, reqs)
	actual, _ := sshClients.LoadOrStore(key, client)
	live := actual.(*ssh.Client)
	if live == client {
		// drop the cache entry when the session dies
		go func() {
			_ = client.Wait()
			sshClients.Delete(key)
		}()
	}
	ch, err := live.Dial("tcp", next)
	if err != nil {
		return nil, live, fmt.Errorf("ssh tunnel to %s: %w", next, err)
	}
	return ch, live, nil
}

// knownHosts implements trust-on-first-use for SSH host keys.
var knownHosts = struct {
	mu   sync.Mutex
	path string
}{}

func knownHostsPathLocked() string {
	if knownHosts.path == "" {
		knownHosts.path = filepath.Join(appConfigDir(), "ssh_known_hosts.json")
	}
	return knownHosts.path
}

func loadKnownHostsLocked() map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(knownHostsPathLocked())
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func saveKnownHostsLocked(m map[string]string) {
	data, _ := json.MarshalIndent(m, "", "  ")
	path := knownHostsPathLocked()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, data, 0o600)
}

// ClearSSHHostKey removes a saved SSH host key fingerprint.
func ClearSSHHostKey(host string, port int) error {
	knownHosts.mu.Lock()
	defer knownHosts.mu.Unlock()
	m := loadKnownHostsLocked()
	delete(m, net.JoinHostPort(host, fmt.Sprint(port)))
	saveKnownHostsLocked(m)
	return nil
}

func tofuHostKeyCallback(addr string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		knownHosts.mu.Lock()
		defer knownHosts.mu.Unlock()
		fp := ssh.FingerprintSHA256(key)
		m := loadKnownHostsLocked()
		if old, ok := m[addr]; ok {
			if old != fp {
				return fmt.Errorf(
					"SSH host key for %s has changed!\nsaved: %s\ncurrent: %s\nIf this is expected, clear the saved host key in the connection settings.",
					addr, old, fp)
			}
			return nil
		}
		m[addr] = fp
		saveKnownHostsLocked(m)
		return nil
	}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// Cluster dialer: spreads requests across nodes (round-robin) and fails over
// to the next node when dialing one fails.

type clusterDialer struct {
	conn  *Connection
	nodes []string // host:port list, len > 0
	next  atomic.Uint32
}

// dial connects to the next healthy node, bypassing the transport layer.
func (d *clusterDialer) dial(ctx context.Context) (net.Conn, error) {
	return d.DialContext(ctx, "tcp", "")
}

func (d *clusterDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported network %q", network)
	}
	start := int(d.next.Add(1)-1) % len(d.nodes)
	var lastErr error
	for i := range d.nodes {
		node := d.nodes[(start+i)%len(d.nodes)]
		if len(d.conn.Hops) == 0 {
			c, err := dialTCP(ctx, node)
			if err == nil {
				return c, nil
			}
			lastErr = err
			continue
		}
		cd := &chainDialer{conn: d.conn, target: node}
		c, err := cd.dial(ctx)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("cluster: all %d nodes unreachable, last error: %w", len(d.nodes), lastErr)
}

// ---------------------------------------------------------------------------
// HTTP client (cached per connection)

type clientEntry struct {
	client *http.Client
}

var (
	httpClients sync.Map // string (connection fingerprint) -> *clientEntry
)

func invalidateClient(key string) {
	httpClients.Delete(key)
}

// getHTTPClient returns a cached HTTP client for the connection. Hop chains
// (including cached SSH clients) are rebuilt transparently when they die.
func getHTTPClient(conn *Connection) (*http.Client, error) {
	key := fingerprint(conn)
	if v, ok := httpClients.Load(key); ok {
		return v.(*clientEntry).client, nil
	}

	transport := &http.Transport{
		MaxIdleConns:        8,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	primary := conn.PrimaryNode()
	scheme := primary.Scheme
	if scheme == "" {
		scheme = "http"
	}
	target := net.JoinHostPort(primary.Host, fmt.Sprint(primary.Port))
	if len(conn.Nodes) > 1 {
		nodes := make([]string, 0, len(conn.Nodes))
		for _, n := range conn.Nodes {
			port := n.Port
			if port == 0 {
				port = 9308
			}
			nodes = append(nodes, net.JoinHostPort(n.Host, fmt.Sprint(port)))
		}
		d := &clusterDialer{conn: conn, nodes: nodes}
		transport.DialContext = d.DialContext
	} else if len(conn.Hops) > 0 {
		d := &chainDialer{conn: conn, target: target}
		transport.DialContext = d.DialContext
	}
	transport.TLSClientConfig = &tls.Config{ServerName: primary.Host}

	client := &http.Client{Transport: transport, Timeout: 120 * time.Second}
	actual, _ := httpClients.LoadOrStore(key, &clientEntry{client: client})
	return actual.(*clientEntry).client, nil
}

func fingerprint(c *Connection) string {
	data, _ := json.Marshal(c)
	return string(data)
}

// ---------------------------------------------------------------------------
// SDK client factory

func buildClient(conn *Connection) (*openapi.APIClient, error) {
	httpClient, err := getHTTPClient(conn)
	if err != nil {
		return nil, err
	}
	cfg := openapi.NewConfiguration()
	cfg.Servers = openapi.ServerConfigurations{
		{URL: serverURL(conn), Description: "user connection"},
	}
	cfg.HTTPClient = httpClient
	return openapi.NewAPIClient(cfg), nil
}

func callCtx(conn *Connection) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	if conn.Username != "" {
		ctx = context.WithValue(ctx, openapi.ContextBasicAuth, openapi.BasicAuth{
			UserName: conn.Username,
			Password: conn.Password,
		})
	}
	return ctx, cancel
}

// pickKeyFile opens a native file dialog to select an SSH private key.
func pickKeyFile() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("application not ready")
	}
	return app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle("选择 SSH 私钥").
		PromptForSingleSelection()
}
