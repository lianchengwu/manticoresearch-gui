package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	configDirOverride = t.TempDir()
	saved, err := connStore.upsert(*c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connStore.remove(saved.ID) })
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
