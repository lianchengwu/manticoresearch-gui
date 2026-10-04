package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// onode is a decoded JSON node that preserves object key order, so the data
// grid shows columns in the order the server sent them.
type onode struct {
	isObj  bool
	keys   []string
	vals   map[string]*onode
	arr    []*onode
	scalar any
}

func (n *onode) get(key string) *onode {
	if n == nil || !n.isObj {
		return nil
	}
	return n.vals[key]
}

func (n *onode) isEmptyObj() bool {
	return n != nil && n.isObj && len(n.keys) == 0
}

// MarshalJSON re-encodes the node preserving key order; nested objects and
// arrays inside query rows stay intact.
func (n *onode) MarshalJSON() ([]byte, error) {
	if n == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	switch {
	case n.isObj:
		buf.WriteByte('{')
		for i, k := range n.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := json.Marshal(n.vals[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case n.arr != nil:
		buf.WriteByte('[')
		for i, el := range n.arr {
			if i > 0 {
				buf.WriteByte(',')
			}
			eb, err := json.Marshal(el)
			if err != nil {
				return nil, err
			}
			buf.Write(eb)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(n.scalar)
	}
}

func (n *onode) str() (string, bool) {
	if n == nil || n.isObj || n.arr != nil || n.scalar == nil {
		return "", false
	}
	switch v := n.scalar.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		return fmt.Sprintf("%v", v), true
	}
	return "", false
}

func parseOrdered(body []byte) (*onode, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	n, err := decodeNode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return n, nil
}

func decodeNode(dec *json.Decoder) (*onode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeFromToken(dec, tok)
}

func decodeFromToken(dec *json.Decoder, tok json.Token) (*onode, error) {
	if t, ok := tok.(json.Delim); ok {
		switch t {
		case '{':
			n := &onode{isObj: true, vals: map[string]*onode{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				v, err := decodeNode(dec)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, key)
				n.vals[key] = v
			}
			if _, err := dec.Token(); err != nil { // closing }
				return nil, err
			}
			return n, nil
		case '[':
			n := &onode{}
			for dec.More() {
				v, err := decodeNode(dec)
				if err != nil {
					return nil, err
				}
				n.arr = append(n.arr, v)
			}
			if _, err := dec.Token(); err != nil { // closing ]
				return nil, err
			}
			return n, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", tok)
	}
	if tok == nil {
		return &onode{}, nil
	}
	return &onode{scalar: tok}, nil
}

// normalizeBody converts a raw Manticore JSON response (any of its several
// shapes) into the uniform QueryResult.
func normalizeBody(body []byte) *QueryResult {
	res := &QueryResult{}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		res.Message = "OK"
		return res
	}
	// The server may answer multiple statements with newline-delimited JSON.
	if json.Valid(trimmed) {
		if n, err := parseOrdered(trimmed); err == nil {
			return nodeToResult(n)
		}
	}
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !json.Valid(line) {
			continue
		}
		if n, err := parseOrdered(line); err == nil {
			partial := nodeToResult(n)
			if res.Columns == nil {
				res.Columns, res.Rows = partial.Columns, partial.Rows
			} else if sameColumns(res.Columns, partial.Columns) {
				res.Rows = append(res.Rows, partial.Rows...)
			} else {
				res.Message = strings.TrimSpace(res.Message + " " + partial.Message)
			}
			if partial.Error != "" {
				res.Error = partial.Error
			}
			continue
		}
	}
	if res.Columns == nil && res.Error == "" && res.Message == "" {
		res.Error = firstLineText(trimmed)
	}
	return res
}

func sameColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func firstLineText(body []byte) string {
	text := string(body)
	if i := strings.IndexByte(text, '\n'); i > 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

func nodeToResult(n *onode) *QueryResult {
	res := &QueryResult{}
	switch {
	case n == nil:
		res.Message = "OK"
	case n.arr != nil:
		fillTableFromArray(res, n)
	case n.isObj:
		fillTableFromObject(res, n)
	default:
		if s, ok := n.str(); ok {
			res.Message = s
		} else {
			res.Message = "OK"
		}
	}
	return res
}

func fillTableFromArray(res *QueryResult, n *onode) {
	if len(n.arr) == 0 {
		res.Message = "OK (0 rows)"
		return
	}
	objs := true
	for _, el := range n.arr {
		if !el.isObj {
			objs = false
			break
		}
	}
	if !objs {
		res.Columns = []string{"value"}
		for _, el := range n.arr {
			res.Rows = append(res.Rows, []any{el.scalar})
		}
		return
	}
	fillRowTable(res, n.arr)
	if len(res.Rows) == 0 {
		res.Message = "OK (0 rows)"
	}
}

// fillTableFromObject dispatches on the known response shapes.
func fillTableFromObject(res *QueryResult, n *onode) {
	// Search API shape: {"hits": {"hits": [...], "total": N}, "took": ...}
	if h := n.get("hits"); h != nil && h.isObj {
		if inner := h.get("hits"); inner != nil && inner.arr != nil {
			fromSearchHits(res, inner.arr)
			if t := h.get("total"); t != nil {
				if s, ok := t.str(); ok {
					var total int64
					fmt.Sscan(s, &total)
					res.Total = &total
				}
			}
			if took := n.get("took"); took != nil {
				if s, ok := took.str(); ok {
					res.TookMs, _ = strconvFloat(s)
				}
			}
			return
		}
	}
	// SQL object shape: {"columns": [...], "data": [...], ...}
	if c := n.get("columns"); c != nil && c.arr != nil {
		if d := n.get("data"); d != nil && d.arr != nil {
			var cols []string
			for _, ce := range c.arr {
				if ce.isObj && len(ce.keys) > 0 {
					cols = append(cols, ce.keys[0])
				}
			}
			res.Columns = cols
			for _, row := range d.arr {
				if !row.isObj {
					continue
				}
				vals := make([]any, len(cols))
				for i, col := range cols {
					if v := row.get(col); v != nil {
						vals[i] = valueOf(v)
					}
				}
				res.Rows = append(res.Rows, vals)
			}
			return
		}
	}
	// Error object: {"error": "...", ...}
	if e := n.get("error"); e != nil {
		if s, ok := e.str(); ok && s != "" {
			res.Error = s
			return
		}
	}
	// Generic scalar-only object: show it as a single row.
	if len(n.keys) > 0 {
		allScalar := true
		for _, k := range n.keys {
			v := n.vals[k]
			if v.isObj || v.arr != nil {
				allScalar = false
				break
			}
		}
		if allScalar {
			res.Columns = n.keys
			vals := make([]any, len(n.keys))
			for i, k := range n.keys {
				vals[i] = valueOf(n.vals[k])
			}
			res.Rows = append(res.Rows, vals)
			res.Message = "OK"
			return
		}
	}
	res.Message = "OK"
}

// fromSearchHits builds a grid from Search API hits: id, _score, then the
// document source fields in server order.
func fromSearchHits(res *QueryResult, hits []*onode) {
	cols := []string{"id", "_score"}
	for _, h := range hits {
		src := h.get("_source")
		if src == nil || !src.isObj {
			continue
		}
		for _, k := range src.keys {
			if !contains(cols, k) {
				cols = append(cols, k)
			}
		}
	}
	res.Columns = cols
	for _, h := range hits {
		vals := make([]any, len(cols))
		if id := h.get("_id"); id != nil {
			if s, ok := id.str(); ok {
				vals[0] = s
			} else {
				vals[0] = id.scalar
			}
		}
		if sc := h.get("_score"); sc != nil {
			vals[1] = valueOf(sc)
		}
		if src := h.get("_source"); src != nil && src.isObj {
			for i := 2; i < len(cols); i++ {
				if v := src.get(cols[i]); v != nil {
					vals[i] = valueOf(v)
				}
			}
		}
		res.Rows = append(res.Rows, vals)
	}
}

func fillRowTable(res *QueryResult, objs []*onode) {
	var cols []string
	for _, o := range objs {
		for _, k := range o.keys {
			if !contains(cols, k) {
				cols = append(cols, k)
			}
		}
	}
	res.Columns = cols
	for _, o := range objs {
		vals := make([]any, len(cols))
		for i, c := range cols {
			if v := o.get(c); v != nil {
				vals[i] = valueOf(v)
			}
		}
		res.Rows = append(res.Rows, vals)
	}
}

func valueOf(n *onode) any {
	if n == nil {
		return nil
	}
	if n.isObj || n.arr != nil {
		return n // marshals back to ordered JSON
	}
	return n.scalar
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func strconvFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscan(s, &f)
	return f, err
}
