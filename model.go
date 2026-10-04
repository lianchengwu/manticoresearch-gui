package main

import "fmt"

// Connection describes a saved Manticore Search server connection.
type Connection struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Scheme   string        `json:"scheme"` // http | https
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	Username string        `json:"username"` // Manticore basic auth (optional)
	Password string        `json:"password"`
	Nodes    []NodeAddress `json:"nodes,omitempty"` // non-empty = cluster mode
	Hops     []Hop         `json:"hops,omitempty"`  // ordered network chain
	Net      *legacyNet    `json:"net,omitempty"`   // pre-chain configs, migrated on load
}

// Hop is one node of the network chain, applied left to right:
// 本机 → hop1 → hop2 → … → Manticore. Any mix of proxy and SSH types is
// allowed and the order is freely adjustable.
type Hop struct {
	Type     string `json:"type"` // http | socks5 | ssh
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`

	// SSH only
	AuthType      string `json:"authType,omitempty"` // password | key
	KeyPath       string `json:"keyPath,omitempty"`
	KeyPassphrase string `json:"keyPassphrase,omitempty"`
	// RemoteHost/RemotePort is where this hop tunnels to, as seen from the
	// hop itself. Empty = automatic (the next hop, or Manticore for the last).
	RemoteHost string `json:"remoteHost,omitempty"`
	RemotePort int    `json:"remotePort,omitempty"`
}

// legacyNet is the pre-chain network config; it is converted to Hops when a
// saved connection is loaded.
type legacyNet struct {
	Mode    string     `json:"mode"`
	Proxies []ProxyHop `json:"proxies,omitempty"`
	SSH     *legacySSH `json:"ssh,omitempty"`
}

type ProxyHop struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type legacySSH struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	AuthType      string `json:"authType"`
	Password      string `json:"password"`
	KeyPath       string `json:"keyPath"`
	KeyPassphrase string `json:"keyPassphrase"`
	RemoteHost    string `json:"remoteHost"`
	RemotePort    int    `json:"remotePort"`
}

// migrateNet converts a legacy net config into the unified hop chain.
func (c *Connection) migrateNet() {
	if c.Net == nil || len(c.Hops) > 0 {
		return
	}
	net := c.Net
	c.Net = nil
	switch net.Mode {
	case "proxy":
		for _, p := range net.Proxies {
			c.Hops = append(c.Hops, Hop{
				Type: p.Type, Host: p.Host, Port: p.Port,
				Username: p.Username, Password: p.Password, Disabled: p.Disabled,
			})
		}
	case "ssh":
		if net.SSH != nil {
			s := net.SSH
			c.Hops = append(c.Hops, Hop{
				Type: "ssh", Host: s.Host, Port: s.Port,
				Username: s.Username, Password: s.Password,
				AuthType: s.AuthType, KeyPath: s.KeyPath, KeyPassphrase: s.KeyPassphrase,
				RemoteHost: s.RemoteHost, RemotePort: s.RemotePort,
			})
		}
	}
}

// NodeAddress is one Manticore node of a cluster connection.
type NodeAddress struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

// PrimaryNode returns the node the SDK client URL points at.
func (c *Connection) PrimaryNode() NodeAddress {
	if len(c.Nodes) > 0 {
		return c.Nodes[0]
	}
	return NodeAddress{Scheme: c.Scheme, Host: c.Host, Port: c.Port}
}

// QueryResult is the normalized shape of any query, rendered by the data grid.
type QueryResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Total   *int64   `json:"total,omitempty"`
	TookMs  float64  `json:"tookMs"`
	Message string   `json:"message,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// TableInfo is one entry of the sidebar table list.
type TableInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Docs string `json:"docs,omitempty"`
}

// BrowseOptions controls the table data browser.
type BrowseOptions struct {
	Table    string `json:"table"`
	Query    string `json:"query"` // empty = match all
	Page     int    `json:"page"`  // 0-based
	PageSize int    `json:"pageSize"`
	SortBy   string `json:"sortBy"`
	SortAsc  bool   `json:"sortAsc"`
}

// MutationResult reports a document write.
type MutationResult struct {
	Message string `json:"message"`
}

func serverURL(c *Connection) string {
	node := c.PrimaryNode()
	scheme := node.Scheme
	if scheme == "" {
		scheme = "http"
	}
	port := node.Port
	if port == 0 {
		port = 9308
	}
	return fmt.Sprintf("%s://%s:%d", scheme, node.Host, port)
}
