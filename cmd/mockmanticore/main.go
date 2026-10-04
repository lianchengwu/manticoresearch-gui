// Command mockmanticore runs a tiny in-memory Manticore Search HTTP mock
// for development and UI testing without a real server.
//
// Usage: go run ./cmd/mockmanticore [port]
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
)

type doc struct {
	ID    uint64         `json:"id"`
	Attrs map[string]any `json:"attrs"`
}

type server struct {
	mu   sync.Mutex
	docs map[uint64]map[string]any
	next uint64
}

func main() {
	port := 9308
	if len(os.Args) > 1 {
		p, err := strconv.Atoi(os.Args[1])
		if err == nil {
			port = p
		}
	}
	s := &server{docs: map[uint64]map[string]any{}, next: 100}
	for i := 1; i <= 8; i++ {
		s.docs[uint64(i)] = map[string]any{
			"title": fmt.Sprintf("Sample product #%d", i),
			"price": float64(i) * 9.99,
			"tags":  []string{"new", fmt.Sprintf("cat-%d", i%3)},
		}
	}
	s.next = 9

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		switch r.URL.Path {
		case "/sql":
			s.sql(w, string(body))
		case "/search":
			s.search(w)
		case "/insert", "/replace":
			s.writeDoc(w, body)
		case "/update":
			s.update(w)
		case "/delete":
			fmt.Fprint(w, `{"table":"products","deleted":1}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	fmt.Printf("mock manticore listening on http://127.0.0.1:%d\n", port)
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (s *server) sql(w http.ResponseWriter, q string) {
	switch {
	case contains(q, "version()"):
		fmt.Fprint(w, `[{"version()":"9.2.14-mock"}]`)
	case q == "SHOW TABLES":
		fmt.Fprint(w, `[{"Index":"products","Type":"rt"},{"Index":"alerts","Type":"percolate"}]`)
	case len(q) >= 8 && q[:8] == "DESCRIBE":
		fmt.Fprint(w, `{"columns":[{"Field":{"type":"string"}},{"Type":{"type":"string"}},{"Properties":{"type":"string"}}],"data":[`+
			`{"Field":"id","Type":"bigint","Properties":""},`+
			`{"Field":"title","Type":"text","Properties":"indexed stored"},`+
			`{"Field":"price","Type":"float","Properties":""},`+
			`{"Field":"tags","Type":"multi","Properties":"indexed"}`+
			`]}`)
	case len(q) >= 17 && q[:17] == "SHOW CREATE TABLE":
		fmt.Fprint(w, `[{"Table":"products","Create Table":"CREATE TABLE products (\n  title text,\n  price float,\n  tags multi\n) min_prefix_len='1'"}]`)
	case len(q) >= 6 && q[:6] == "SELECT":
		s.mu.Lock()
		rows := make([]string, 0, len(s.docs))
		for id, d := range s.docs {
			b, _ := json.Marshal(map[string]any{
				"id": id, "title": d["title"], "price": d["price"], "tags": d["tags"],
			})
			rows = append(rows, string(b))
		}
		s.mu.Unlock()
		fmt.Fprintf(w, "[%s]", join(rows))
	default:
		fmt.Fprint(w, `{"total":0,"error":"","warning":""}`)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func (s *server) search(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hits := make([]string, 0, len(s.docs))
	for id, d := range s.docs {
		b, _ := json.Marshal(map[string]any{
			"_id":    fmt.Sprint(id),
			"_score": 1,
			"_source": map[string]any{
				"id": id, "title": d["title"], "price": d["price"], "tags": d["tags"],
			},
		})
		hits = append(hits, string(b))
	}
	fmt.Fprintf(w, `{"took":1,"hits":{"total":%d,"hits":[%s]}}`, len(hits), join(hits))
}

func (s *server) writeDoc(w http.ResponseWriter, body []byte) {
	var req struct {
		Table string         `json:"table"`
		ID    *uint64        `json:"id"`
		Doc   map[string]any `json:"doc"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Doc == nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad request"}`)
		return
	}
	s.mu.Lock()
	id := s.next
	if req.ID != nil {
		id = *req.ID
	}
	s.docs[id] = req.Doc
	s.next++
	s.mu.Unlock()
	fmt.Fprintf(w, `{"table":"products","_id":%d,"created":true,"result":"created","status":201}`, id)
}

func (s *server) update(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprint(w, `{"table":"products","updated":1}`)
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
