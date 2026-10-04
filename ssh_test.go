package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type stubAddr string

func (s stubAddr) Network() string { return "tcp" }
func (s stubAddr) String() string  { return string(s) }

func resetKnownHosts(t *testing.T) {
	t.Helper()
	configDirOverride = t.TempDir()
	knownHosts.mu.Lock()
	knownHosts.path = ""
	knownHosts.mu.Unlock()
	t.Cleanup(func() {
		knownHosts.mu.Lock()
		knownHosts.path = ""
		knownHosts.mu.Unlock()
		configDirOverride = ""
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
		t.Fatal("TOFU re-check deadlocked")
	}

	go func() {
		done <- ClearSSHHostKey("127.0.0.1", 22)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ClearSSHHostKey deadlocked")
	}
}

func TestTOFURejectsChangedKey(t *testing.T) {
	resetKnownHosts(t)
	a, b := testSigner(t), testSigner(t)
	cb := tofuHostKeyCallback("10.0.0.1:22")
	if err := cb("", stubAddr("10.0.0.1:22"), a.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := cb("", stubAddr("10.0.0.1:22"), b.PublicKey()); err == nil {
		t.Fatal("expected host key mismatch")
	}
}

type sshTestServer struct {
	ln     net.Listener
	cfg    *ssh.ServerConfig
	closed chan struct{}
}

func startSSHServer(t *testing.T) *sshTestServer {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "test" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(testSigner(t))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &sshTestServer{ln: ln, cfg: cfg, closed: make(chan struct{})}
	t.Cleanup(func() {
		ln.Close()
		select {
		case <-s.closed:
		case <-time.After(2 * time.Second):
		}
	})
	go s.serve()
	return s
}

func (s *sshTestServer) addr() string { return s.ln.Addr().String() }

func (s *sshTestServer) serve() {
	defer close(s.closed)
	for {
		tcp, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(tcp)
	}
}

func (s *sshTestServer) handle(tcp net.Conn) {
	conn, chans, reqs, err := ssh.NewServerConn(tcp, s.cfg)
	if err != nil {
		tcp.Close()
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			ch.Reject(ssh.UnknownChannelType, ch.ChannelType())
			continue
		}
		var extra struct {
			Host     string
			Port     uint32
			OrigHost string
			OrigPort uint32
		}
		if err := ssh.Unmarshal(ch.ExtraData(), &extra); err != nil {
			ch.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		backend, err := net.DialTimeout("tcp", net.JoinHostPort(extra.Host, fmt.Sprint(extra.Port)), 3*time.Second)
		if err != nil {
			ch.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		channel, reqs, err := ch.Accept()
		if err != nil {
			backend.Close()
			continue
		}
		go ssh.DiscardRequests(reqs)
		go func() {
			defer channel.Close()
			defer backend.Close()
			go func() { _, _ = io.Copy(channel, backend); channel.CloseWrite() }()
			_, _ = io.Copy(backend, channel)
		}()
	}
}

func TestSSHHopTestConnection(t *testing.T) {
	resetKnownHosts(t)
	m := newMockManticore(t, "", "")
	sshSrv := startSSHServer(t)

	c := *newTestConn(m)
	c.Hops = []Hop{{
		Type:     "ssh",
		Host:     hostOf(sshSrv.addr()),
		Port:     portOf(sshSrv.addr()),
		Username: "test",
		AuthType: "password",
		Password: "secret",
	}}
	r, err := ConnectionService{}.TestConnection(c)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Version != "9.2.14" {
		t.Fatalf("result = %+v", r)
	}
}
