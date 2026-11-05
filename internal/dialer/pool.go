package dialer

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	openapi "github.com/manticoresoftware/manticoresearch-go"
	"manticoresearch-gui/internal/model"
)

type clientEntry struct {
	client *http.Client
}

var (
	httpClients sync.Map // string (connection fingerprint) -> *clientEntry
)

// InvalidateClient drops the cached HTTP client for this connection.
// Live SSH sessions stay cached and are reused until they die.
func InvalidateClient(conn *model.Connection) {
	httpClients.Delete(Fingerprint(conn))
}

// Fingerprint produces a JSON string fingerprint of the connection configuration.
func Fingerprint(c *model.Connection) string {
	data, _ := json.Marshal(c)
	return string(data)
}

// GetHTTPClient returns a cached HTTP client for the connection. Hop chains
// (including cached SSH clients) are rebuilt transparently when they die.
func GetHTTPClient(conn *model.Connection) (*http.Client, error) {
	key := Fingerprint(conn)
	if v, ok := httpClients.Load(key); ok {
		return v.(*clientEntry).client, nil
	}

	transport := &http.Transport{
		MaxIdleConns:        8,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	primary := conn.PrimaryNode()
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
		d := &ClusterDialer{Conn: conn, Nodes: nodes}
		transport.DialContext = d.DialContext
	} else if len(conn.Hops) > 0 {
		d := &ChainDialer{Conn: conn, Target: target}
		transport.DialContext = d.DialContext
	}
	transport.TLSClientConfig = &tls.Config{ServerName: primary.Host}

	client := &http.Client{Transport: transport, Timeout: 120 * time.Second}
	actual, _ := httpClients.LoadOrStore(key, &clientEntry{client: client})
	return actual.(*clientEntry).client, nil
}

// BuildClient initializes a Manticore Search OpenAPI client using custom transport dialers.
func BuildClient(conn *model.Connection) (*openapi.APIClient, error) {
	httpClient, err := GetHTTPClient(conn)
	if err != nil {
		return nil, err
	}
	cfg := openapi.NewConfiguration()
	cfg.Servers = openapi.ServerConfigurations{
		{URL: conn.ServerURL(), Description: "user connection"},
	}
	cfg.HTTPClient = httpClient
	return openapi.NewAPIClient(cfg), nil
}

// CallCtx creates a context with timeout and basic auth headers configured for the connection.
func CallCtx(conn *model.Connection) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	if conn.Username != "" {
		ctx = context.WithValue(ctx, openapi.ContextBasicAuth, openapi.BasicAuth{
			UserName: conn.Username,
			Password: conn.Password,
		})
	}
	return ctx, cancel
}
