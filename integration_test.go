package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"manticoresearch-gui/internal/dialer"
)

// mockManticore mimics the Manticore Search HTTP endpoints the SDK uses.
type mockManticore struct {
	t         *testing.T
	srv       *httptest.Server
	sqlBodies []string
	authUser  string
	authPass  string
	unauth    bool
}

func newMockManticore(t *testing.T, authUser, authPass string) *mockManticore {
	m := &mockManticore{t: t, authUser: authUser, authPass: authPass}
	m.srv = httptest.NewServer(http.HandlerFunc(m.route))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockManticore) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if m.authUser == "" {
		return true
	}
	u, p, ok := r.BasicAuth()
	if !ok || u != m.authUser || p != m.authPass {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
		return false
	}
	return true
}

func (m *mockManticore) route(w http.ResponseWriter, r *http.Request) {
	if !m.checkAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	body, _ := io.ReadAll(r.Body)
	switch r.URL.Path {
	case "/sql":
		m.sqlBodies = append(m.sqlBodies, string(body))
		m.sql(w, string(body))
	case "/search":
		m.search(w, body)
	case "/insert":
		m.insert(w, body)
	case "/replace":
		m.replace(w, body)
	case "/update":
		m.update(w, body)
	case "/delete":
		m.delete(w, body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (m *mockManticore) sql(w http.ResponseWriter, q string) {
	switch {
	case strings.HasPrefix(q, "SELECT version()"):
		fmt.Fprint(w, `[{"version()":"9.2.14"}]`)
	case strings.HasPrefix(q, "SHOW TABLES"):
		fmt.Fprint(w, `[{"columns":[{"Table":{"type":"string"}},{"Type":{"type":"string"}}],"data":[{"Table":"products","Type":"rt"},{"Table":"alerts","Type":"percolate"}],"total":2,"error":"","warning":""}]`)
	case strings.HasPrefix(q, "DESCRIBE"):
		fmt.Fprint(w, `{"columns":[{"Field":{"type":"string"}},{"Type":{"type":"string"}},{"Properties":{"type":"string"}}],"data":[{"Field":"title","Type":"text","Properties":"indexed"},{"Field":"price","Type":"float","Properties":""}]}`)
	case strings.Contains(q, "syntax error"), strings.Contains(q, "FAILCOL"):
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"error":"P01: syntax error, unexpected identifier near 'BOGUS'"}`)
	default:
		fmt.Fprint(w, `{"total":0,"error":"","warning":""}`)
	}
}

func (m *mockManticore) search(w http.ResponseWriter, body []byte) {
	var req map[string]any
	json.Unmarshal(body, &req)
	if req["table"] != "products" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"no such table"}`)
		return
	}
	offset := 0
	if o, ok := req["offset"].(float64); ok {
		offset = int(o)
	}
	// literal JSON so field order matches what a real server sends
	// (encoding a Go map would silently sort keys alphabetically)
	fmt.Fprintf(w, `{"took":2.5,"timed_out":false,"hits":{"total":42,"total_relation":"eq","hits":[
		{"_id":"101","_score":1,"_source":{"title":"first doc","price":9.99,"tags":["a","b"]}},
		{"_id":"102","_score":0.9,"_source":{"title":"page-%d","price":12.5}}
	]}}`, offset)
}

func (m *mockManticore) insert(w http.ResponseWriter, body []byte) {
	var req map[string]any
	json.Unmarshal(body, &req)
	if _, hasDoc := req["doc"]; !hasDoc {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"doc required"}`)
		return
	}
	w.Write([]byte(`{"table":"products","_id":555,"created":true,"result":"created","status":201}`))
}

func (m *mockManticore) replace(w http.ResponseWriter, _ []byte) {
	w.Write([]byte(`{"table":"products","_id":101,"replaced":true,"result":"updated","status":200}`))
}

func (m *mockManticore) update(w http.ResponseWriter, _ []byte) {
	w.Write([]byte(`{"table":"products","_id":101,"updated":1}`))
}

func (m *mockManticore) delete(w http.ResponseWriter, _ []byte) {
	w.Write([]byte(`{"table":"products","deleted":1}`))
}

// ---------------------------------------------------------------------------

func newTestConn(m *mockManticore) *Connection {
	addr := strings.TrimPrefix(m.srv.URL, "http://")
	i := strings.LastIndex(addr, ":")
	return &Connection{Host: addr[:i], Port: mustPort(addr[i+1:]), Scheme: "http"}
}

func mustPort(s string) int {
	var p int
	fmt.Sscan(s, &p)
	return p
}

// registerConn persists the connection in an isolated config dir and returns
// the assigned id.
func registerConn(t *testing.T, c *Connection) string {
	t.Helper()
	dir := t.TempDir()
	dialer.SetConfigDir(dir)
	saved, err := connStore.upsert(*c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dialer.SetConfigDir(dir)
		_ = connStore.remove(saved.ID)
		dialer.SetConfigDir("")
	})
	return saved.ID
}

func TestSQLSelectVersion(t *testing.T) {
	m := newMockManticore(t, "", "")
	conn := newTestConn(m)
	id := registerConn(t, conn)
	res, err := QueryService{}.ExecuteSQL(id, "SELECT version()")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatal(res.Error)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] != "9.2.14" {
		t.Fatalf("rows = %v", res.Rows)
	}
}

func TestSQLShowTablesToList(t *testing.T) {
	m := newMockManticore(t, "", "")
	conn := newTestConn(m)
	id := registerConn(t, conn)
	tables, err := TableService{}.ListTables(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 || tables[0].Name != "products" || tables[0].Type != "rt" || tables[1].Type != "percolate" {
		t.Fatalf("tables = %+v", tables)
	}
}

func TestSQLSyntaxError(t *testing.T) {
	m := newMockManticore(t, "", "")
	conn := newTestConn(m)
	res, err := QueryService{}.ExecuteSQL(registerConn(t, conn), "BOGUS syntax error query")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == "" || !strings.Contains(res.Error, "syntax error") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestBrowseDocumentsViaSearchAPI(t *testing.T) {
	m := newMockManticore(t, "", "")
	conn := newTestConn(m)
	id := registerConn(t, conn)
	res, err := TableService{}.BrowseDocuments(id, BrowseOptions{
		Table: "products", Page: 1, PageSize: 50, Query: "doc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatal(res.Error)
	}
	if res.Total == nil || *res.Total != 42 {
		t.Fatalf("total = %v", res.Total)
	}
	wantCols := []string{"id", "_score", "title", "price", "tags"}
	if len(res.Columns) != len(wantCols) {
		t.Fatalf("columns = %v", res.Columns)
	}
	for i, c := range wantCols {
		if res.Columns[i] != c {
			t.Fatalf("columns = %v", res.Columns)
		}
	}
	if res.Rows[0][0] != "101" || res.Rows[0][2] != "first doc" {
		t.Fatalf("row0 = %v", res.Rows[0])
	}
	if res.Rows[1][2] != "page-50" {
		t.Fatalf("offset not applied: %v", res.Rows[1])
	}
}

func TestDescribeTable(t *testing.T) {
	m := newMockManticore(t, "", "")
	conn := newTestConn(m)
	id := registerConn(t, conn)
	res, err := TableService{}.DescribeTable(id, "products")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatal(res.Error)
	}
	if len(res.Columns) != 3 || res.Columns[0] != "Field" {
		t.Fatalf("columns = %v", res.Columns)
	}
	if len(res.Rows) != 2 || res.Rows[0][0] != "title" {
		t.Fatalf("rows = %v", res.Rows)
	}
}
func TestCreateTableSendsBuiltSQL(t *testing.T) {
	m := newMockManticore(t, "", "")
	id := registerConn(t, newTestConn(m))
	res, err := TableService{}.CreateTable(id, CreateTableSpec{
		Name:    "demo",
		Columns: []ColumnDef{{Name: "title", Type: "text", Indexed: true, Stored: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatal(res.Error)
	}
	if len(m.sqlBodies) == 0 || !strings.Contains(m.sqlBodies[len(m.sqlBodies)-1], "CREATE TABLE `demo` (`title` text)") {
		t.Fatalf("sql bodies: %#v", m.sqlBodies)
	}
	applied, err := TableService{}.ApplySchema(id, "demo", []SchemaChange{
		{Action: "add", Column: ColumnDef{Name: "price", Type: "float"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied.Error != "" || applied.Applied != 1 {
		t.Fatalf("%+v", applied)
	}
	if !strings.Contains(m.sqlBodies[len(m.sqlBodies)-1], "ALTER TABLE `demo` ADD COLUMN `price` float") {
		t.Fatalf("sql bodies: %#v", m.sqlBodies)
	}
	opt, err := TableService{}.OptimizeTable(id, "demo")
	if err != nil || opt.Error != "" {
		t.Fatalf("optimize err=%v res=%+v", err, opt)
	}
	if !strings.Contains(m.sqlBodies[len(m.sqlBodies)-1], "OPTIMIZE TABLE `demo`") {
		t.Fatalf("sql bodies: %#v", m.sqlBodies)
	}
	partial, err := TableService{}.ApplySchema(id, "demo", []SchemaChange{
		{Action: "add", Column: ColumnDef{Name: "okcol", Type: "int"}},
		{Action: "add", Column: ColumnDef{Name: "FAILCOL", Type: "int"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Applied != 1 || partial.Error == "" || !strings.Contains(partial.FailedSQL, "FAILCOL") {
		t.Fatalf("partial apply: %+v", partial)
	}
}

func TestDocumentCRUD(t *testing.T) {
	m := newMockManticore(t, "", "")
	conn := newTestConn(m)
	id := registerConn(t, conn)
	svc := TableService{}

	ins, err := svc.InsertDocument(id, "products", "", map[string]any{"title": "x", "price": 1.5})
	if err != nil || ins == nil || !strings.Contains(ins.Message, "已创建") {
		t.Fatalf("insert: %v, %v", err, ins)
	}

	rep, err := svc.ReplaceDocument(id, "products", "101", map[string]any{"title": "y"})
	if err != nil || rep == nil {
		t.Fatalf("replace: %v, %v", err, rep)
	}

	upd, err := svc.UpdateDocument(id, "products", "101", map[string]any{"price": 2.0})
	if err != nil || !strings.Contains(upd.Message, "1") {
		t.Fatalf("update: %v, %v", err, upd)
	}

	del, err := svc.DeleteDocument(id, "products", "101")
	if err != nil || !strings.Contains(del.Message, "1") {
		t.Fatalf("delete: %v, %v", err, del)
	}

	// insert error propagates
	if _, err := svc.InsertDocument(id, "products", "", nil); err == nil {
		t.Fatal("expected error for nil doc")
	}
}

func TestBasicAuthReachMock(t *testing.T) {
	m := newMockManticore(t, "manti", "secret")
	conn := newTestConn(m)
	conn.Username = "manti"
	conn.Password = "secret"
	id := registerConn(t, conn)

	res, err := QueryService{}.ExecuteSQL(id, "SELECT version()")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatal(res.Error)
	}

	// now with wrong password
	conn2 := *conn
	conn2.Password = "wrong"
	id2 := registerConn(t, &conn2)
	res2, err := QueryService{}.ExecuteSQL(id2, "SELECT version()")
	if err != nil || res2.Error == "" {
		t.Fatalf("expected unauthorized error, got err=%v res=%+v", err, res2)
	}
}

// ---------------------------------------------------------------------------
// Proxy & SSH TestConnection integration tests

func hostOf(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	return h
}

func portOf(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
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

func httpConnectProxy(t *testing.T) string {
	t.Helper()
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
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				up, err := net.Dial("tcp", req.RequestURI)
				if err != nil {
					c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					return
				}
				defer up.Close()
				c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go io.Copy(up, c)
				io.Copy(c, up)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

type mockSocksServer struct {
	ln net.Listener
}

func newMockSocks(t *testing.T) *mockSocksServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &mockSocksServer{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				head := make([]byte, 2)
				if _, err := io.ReadFull(c, head); err != nil {
					return
				}
				methods := make([]byte, head[1])
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				c.Write([]byte{0x05, 0x00}) // no auth

				req := make([]byte, 4)
				if _, err := io.ReadFull(c, req); err != nil {
					return
				}
				var host string
				switch req[3] {
				case 0x01: // IPv4
					ip := make([]byte, 4)
					io.ReadFull(c, ip)
					host = net.IP(ip).String()
				case 0x03: // domain
					l := make([]byte, 1)
					io.ReadFull(c, l)
					dom := make([]byte, l[0])
					io.ReadFull(c, dom)
					host = string(dom)
				default:
					return
				}
				portB := make([]byte, 2)
				io.ReadFull(c, portB)
				port := int(portB[0])<<8 | int(portB[1])
				target := net.JoinHostPort(host, fmt.Sprint(port))

				up, err := net.Dial("tcp", target)
				if err != nil {
					c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				c.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(conn)
		}
	}()
	return s
}

func (s *mockSocksServer) addr() string { return s.ln.Addr().String() }

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
	socks := newMockSocks(t)
	testConnectionThroughHop(t, Hop{Type: "socks5", Host: hostOf(socks.addr()), Port: portOf(socks.addr())})
}

func TestHTTPThenSocksTestConnection(t *testing.T) {
	m := newMockManticore(t, "", "")
	c := *newTestConn(m)
	socks := newMockSocks(t)
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

func TestSSHHopTestConnection(t *testing.T) {
	dialer.SetConfigDir(t.TempDir())
	t.Cleanup(func() { dialer.SetConfigDir("") })
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

func TestConnectionsFollowConfigDir(t *testing.T) {
	dir := t.TempDir()
	dialer.SetConfigDir(dir)
	t.Cleanup(func() { dialer.SetConfigDir("") })

	saved, err := connStore.upsert(Connection{Host: "127.0.0.1", Port: 9308, Name: "cfg"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dialer.SetConfigDir(dir)
		_ = connStore.remove(saved.ID)
	})
	if _, err := os.Stat(filepath.Join(dir, "connections.json")); err != nil {
		t.Fatal(err)
	}
}
