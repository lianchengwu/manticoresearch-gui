package dialer

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"manticoresearch-gui/internal/model"
)

const dialTimeout = 15 * time.Second

// DialTCP connects directly to the target TCP address with a default timeout.
func DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	var nd net.Dialer
	nd.Timeout = dialTimeout
	return nd.DialContext(ctx, "tcp", addr)
}

func hopAddr(h *model.Hop) string {
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
func nextAddr(hops []model.Hop, i int, final string) string {
	if hops[i].RemoteHost != "" {
		port := hops[i].RemotePort
		if port == 0 {
			port = 9308
		}
		return net.JoinHostPort(hops[i].RemoteHost, fmt.Sprint(port))
	}
	for j := i + 1; j < len(hops); j++ {
		if !hops[j].Disabled {
			return hopAddr(&hops[j])
		}
	}
	return final
}

// tunnelConn takes a conn that has reached hop i and tunnels it to the next
// chain address. Returns the tunneled conn (never nil on nil error) and a
// closer for resources that must outlive the connection (SSH client).
func tunnelConn(hops []model.Hop, i int, conn net.Conn, final string) (net.Conn, io.Closer, error) {
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

// ChainDialer builds conns to a target address through the connection's
// enabled hop chain.
type ChainDialer struct {
	Conn   *model.Connection
	Target string // final host:port (a Manticore node or an SSH-in-chain server)
}

func (d *ChainDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported network %q", network)
	}
	return d.Dial(ctx)
}

func (d *ChainDialer) Dial(ctx context.Context) (net.Conn, error) {
	hops := make([]model.Hop, 0, len(d.Conn.Hops))
	for _, h := range d.Conn.Hops {
		if !h.Disabled {
			hops = append(hops, h)
		}
	}
	if len(hops) == 0 {
		return DialTCP(ctx, d.Target)
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
	conn, err = DialTCP(ctx, hopAddr(&hops[0]))
	if err != nil {
		return nil, fmt.Errorf("connect to %s %s: %w", hops[0].Type, hopAddr(&hops[0]), err)
	}
	for i := range hops {
		var closer io.Closer
		var c net.Conn
		c, closer, err = tunnelConn(hops, i, conn, d.Target)
		if closer != nil {
			closers = append(closers, closer)
		}
		if err != nil {
			return nil, fmt.Errorf("via %s %s: %w", hops[i].Type, hopAddr(&hops[i]), err)
		}
		conn = c
	}
	return conn, nil
}

// ---------------------------------------------------------------------------
// SOCKS5 (RFC 1928)

func socks5Handshake(h *model.Hop, conn net.Conn, dest string) (net.Conn, error) {
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

// ---------------------------------------------------------------------------
// HTTP CONNECT proxy

func httpConnectHandshake(h *model.Hop, conn net.Conn, dest string) (net.Conn, error) {
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
// Cluster dialer: spreads requests across nodes (round-robin) and fails over
// to the next node when dialing one fails.

type ClusterDialer struct {
	Conn  *model.Connection
	Nodes []string // host:port list, len > 0
	next  atomic.Uint32
}

func (d *ClusterDialer) Dial(ctx context.Context) (net.Conn, error) {
	return d.DialContext(ctx, "tcp", "")
}

func (d *ClusterDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported network %q", network)
	}
	start := int(d.next.Add(1)-1) % len(d.Nodes)
	var lastErr error
	for i := range d.Nodes {
		node := d.Nodes[(start+i)%len(d.Nodes)]
		if len(d.Conn.Hops) == 0 {
			c, err := DialTCP(ctx, node)
			if err == nil {
				return c, nil
			}
			lastErr = err
			continue
		}
		cd := &ChainDialer{Conn: d.Conn, Target: node}
		c, err := cd.Dial(ctx)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("cluster: all %d nodes unreachable, last error: %w", len(d.Nodes), lastErr)
}
