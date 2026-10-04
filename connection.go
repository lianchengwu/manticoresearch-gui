package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// configDirOverride lets tests redirect the config directory.
var configDirOverride string

func appConfigDir() string {
	if configDirOverride != "" {
		return configDirOverride
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "manticoresearch-gui")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// ---------------------------------------------------------------------------
// Connection store

type connStoreT struct {
	mu    sync.Mutex
	conns []Connection
}

var connStore = &connStoreT{}

func (s *connStoreT) path() string {
	return filepath.Join(appConfigDir(), "connections.json")
}

func (s *connStoreT) all() ([]Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	out := make([]Connection, len(s.conns))
	copy(out, s.conns)
	return out, nil
}

func (s *connStoreT) load() error {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			s.conns = nil
			return nil
		}
		return err
	}
	s.conns = nil
	if err := json.Unmarshal(data, &s.conns); err != nil {
		return err
	}
	for i := range s.conns {
		s.conns[i].migrateNet()
	}
	return nil
}

func (s *connStoreT) persist() error {
	data, err := json.MarshalIndent(s.conns, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(), data, 0o600)
}

func (s *connStoreT) upsert(c Connection) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return c, err
	}
	c.Host = strings.TrimSpace(c.Host)
	c.Name = strings.TrimSpace(c.Name)
	if c.Scheme == "" {
		c.Scheme = "http"
	}
	if c.Port == 0 {
		c.Port = 9308
	}
	if c.Name == "" {
		c.Name = fmt.Sprintf("%s:%d", c.Host, c.Port)
	}
	if c.ID == "" {
		id := make([]byte, 8)
		if _, err := rand.Read(id); err != nil {
			return c, err
		}
		c.ID = hex.EncodeToString(id)
		s.conns = append(s.conns, c)
	} else {
		found := false
		for i := range s.conns {
			if s.conns[i].ID == c.ID {
				// network config changed: drop cached HTTP clients
				invalidateClient(fingerprint(&s.conns[i]))
				s.conns[i] = c
				found = true
				break
			}
		}
		if !found {
			return c, fmt.Errorf("connection %s not found", c.ID)
		}
	}
	return c, s.persist()
}

func (s *connStoreT) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	out := s.conns[:0]
	for _, c := range s.conns {
		if c.ID != id {
			out = append(out, c)
		} else {
			invalidateClient(fingerprint(&c))
		}
	}
	s.conns = out
	return s.persist()
}

func (s *connStoreT) get(id string) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	for i := range s.conns {
		if s.conns[i].ID == id {
			c := s.conns[i]
			return &c, nil
		}
	}
	return nil, fmt.Errorf("connection %q not found", id)
}

// ---------------------------------------------------------------------------
// ConnectionService (bound to the frontend)

type ConnectionService struct{}

// TestResult reports a successful connection test.
type TestResult struct {
	Version string  `json:"version"`
	Via     string  `json:"via"`
	TookMs  float64 `json:"tookMs"`
}

func (ConnectionService) ListConnections() ([]Connection, error) {
	return connStore.all()
}

func (ConnectionService) SaveConnection(c Connection) (Connection, error) {
	if c.Host == "" && len(c.Nodes) == 0 {
		return c, fmt.Errorf("主机地址不能为空")
	}
	for i := range c.Hops {
		h := &c.Hops[i]
		h.Host = strings.TrimSpace(h.Host)
		h.Type = strings.ToLower(h.Type)
		if h.Host == "" {
			return c, fmt.Errorf("网络链路节点 %d 缺少主机地址", i+1)
		}
		if h.Type == "ssh" && h.AuthType == "" {
			h.AuthType = "password"
		}
	}
	for i := range c.Nodes {
		c.Nodes[i].Host = strings.TrimSpace(c.Nodes[i].Host)
		if c.Nodes[i].Scheme == "" {
			c.Nodes[i].Scheme = "http"
		}
		if c.Nodes[i].Port == 0 {
			c.Nodes[i].Port = 9308
		}
		if c.Nodes[i].Host == "" {
			return c, fmt.Errorf("集群节点 %d 缺少主机地址", i+1)
		}
	}
	return connStore.upsert(c)
}

func (ConnectionService) DeleteConnection(id string) error {
	return connStore.remove(id)
}

// TestConnection verifies the full network path and returns the server version.
func (ConnectionService) TestConnection(c Connection) (*TestResult, error) {
	if c.ID != "" {
		if saved, err := connStore.get(c.ID); err == nil {
			// Allow testing with unsaved edits, but fall back to stored
			// secrets when the form leaves them blank.
			if c.Password == "" {
				c.Password = saved.Password
			}
			// fall back to stored SSH secrets for hops the form left blank
			for i := range c.Hops {
				if c.Hops[i].Type != "ssh" {
					continue
				}
				for _, sh := range saved.Hops {
					if sh.Type == "ssh" && sh.Host == c.Hops[i].Host && sh.Port == c.Hops[i].Port {
						if c.Hops[i].Password == "" {
							c.Hops[i].Password = sh.Password
						}
						if c.Hops[i].KeyPassphrase == "" {
							c.Hops[i].KeyPassphrase = sh.KeyPassphrase
						}
					}
				}
				if c.Hops[i].KeyPath != "" {
					c.Hops[i].KeyPath = expandHome(c.Hops[i].KeyPath)
				}
			}
		}
	}
	client, err := buildClient(&c)
	if err != nil {
		return nil, err
	}
	ctx, cancel := callCtx(&c)
	defer cancel()
	start := time.Now()
	resp, _, err := client.UtilsAPI.Sql(ctx).Body("SELECT version()").Execute()
	took := time.Since(start).Seconds() * 1000
	if err != nil {
		return nil, describeError(err)
	}
	version := ""
	if resp != nil && resp.ArrayOfMapmapOfStringAny != nil && len(*resp.ArrayOfMapmapOfStringAny) > 0 {
		row := (*resp.ArrayOfMapmapOfStringAny)[0]
		for _, v := range row {
			version = fmt.Sprintf("%v", v)
			break
		}
	}
	return &TestResult{
		Version: version,
		Via:     viaDescription(&c),
		TookMs:  took,
	}, nil
}

// ClearSSHHostKey removes the stored host key fingerprint for an SSH server.
func (ConnectionService) ClearSSHHostKey(host string, port int) error {
	return ClearSSHHostKey(host, port)
}

func (ConnectionService) PickKeyFile() (string, error) {
	return pickKeyFile()
}

func viaDescription(c *Connection) string {
	cluster := ""
	if len(c.Nodes) > 1 {
		cluster = fmt.Sprintf("集群 %d 节点 · ", len(c.Nodes))
	}
	active := 0
	parts := []string{"本机"}
	for _, h := range c.Hops {
		if h.Disabled {
			continue
		}
		active++
		parts = append(parts, h.Type+"://"+h.Host)
	}
	if active == 0 {
		return cluster + "直连"
	}
	parts = append(parts, "Manticore")
	return cluster + fmt.Sprintf("%d 跳 (%s)", active, strings.Join(parts, " → "))
}

func describeError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// Generated SDK errors embed the raw response body; keep it readable.
	if i := strings.Index(msg, "body:"); i > 0 {
		tail := strings.TrimSpace(msg[i+5:])
		if tail != "" {
			return fmt.Errorf("%s", tail)
		}
	}
	return fmt.Errorf("%s", msg)
}
