package dialer

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"manticoresearch-gui/internal/model"
)

// sshClients caches live SSH clients per hop so repeated requests reuse the
// authenticated session instead of re-handshaking per connection.
var sshClients sync.Map // string (hop fingerprint) -> *ssh.Client

func sshHopKey(h *model.Hop) string {
	data, _ := json.Marshal(struct {
		Host, User, Pass, KeyPath, Passphrase string
		Port                                  int
	}{h.Host, h.Username, h.Password, h.KeyPath, h.KeyPassphrase, h.Port})
	return string(data)
}

func invalidateSSHHop(h *model.Hop) {
	if v, ok := sshClients.Load(sshHopKey(h)); ok {
		v.(*ssh.Client).Close()
		sshClients.Delete(sshHopKey(h))
	}
}

func sshAuthMethods(h *model.Hop) ([]ssh.AuthMethod, error) {
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
func sshHopTunnel(h *model.Hop, conn net.Conn, next string) (net.Conn, io.Closer, error) {
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

	addr := hopAddr(h)
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

// ---------------------------------------------------------------------------
// Known hosts / TOFU

var (
	configDirOverride   string
	configDirOverrideMu sync.RWMutex
	knownHosts          = struct {
		mu   sync.Mutex
		path string
	}{}
)

// SetConfigDir redirects ConfigDir. Empty restores the user config directory.
// connections.json and ssh_known_hosts.json both follow this path.
func SetConfigDir(dir string) {
	configDirOverrideMu.Lock()
	defer configDirOverrideMu.Unlock()
	configDirOverride = dir
	knownHosts.mu.Lock()
	knownHosts.path = ""
	knownHosts.mu.Unlock()
}

// ConfigDir is the directory for connections.json and ssh_known_hosts.json.
func ConfigDir() string {
	configDirOverrideMu.RLock()
	override := configDirOverride
	configDirOverrideMu.RUnlock()
	return resolveConfigDir(override, os.UserConfigDir, os.UserHomeDir)
}

func resolveConfigDir(override string, userConfig, home func() (string, error)) string {
	if override != "" {
		return override
	}
	base, err := userConfig()
	if err != nil {
		h, _ := home()
		base = filepath.Join(h, ".config")
	}
	dir := filepath.Join(base, "manticoresearch-gui")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func knownHostsPathLocked() string {
	if knownHosts.path == "" {
		knownHosts.path = filepath.Join(ConfigDir(), "ssh_known_hosts.json")
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
