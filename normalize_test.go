package main

import (
	"encoding/json"
	"testing"
)

func TestNormalizeBodyArrayRows(t *testing.T) {
	body := []byte(`[{"id":1,"title":"hello","tags":[1,2],"meta":{"k":"v"}},{"id":2,"title":"world"}]`)
	res := normalizeBody(body)
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	wantCols := []string{"id", "title", "tags", "meta"}
	if len(res.Columns) != len(wantCols) {
		t.Fatalf("columns = %v, want %v", res.Columns, wantCols)
	}
	for i := range wantCols {
		if res.Columns[i] != wantCols[i] {
			t.Fatalf("columns = %v, want %v", res.Columns, wantCols)
		}
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if res.Rows[0][1] != "hello" {
		t.Fatalf("row0 title = %v", res.Rows[0][1])
	}
	// nested values must marshal back to ordered JSON
	b, _ := json.Marshal(res.Rows[0][2])
	if string(b) != "[1,2]" {
		t.Fatalf("tags marshalled as %s", b)
	}
	b, _ = json.Marshal(res.Rows[0][3])
	if string(b) != `{"k":"v"}` {
		t.Fatalf("meta marshalled as %s", b)
	}
}

func TestNormalizeBodyHitsShape(t *testing.T) {
	body := []byte(`{"took":3,"hits":{"total":12,"hits":[{"_id":"7","_score":1,"_source":{"title":"a","year":2020}},{"_id":"8","_score":2,"_source":{"title":"b","year":2021}}]}}`)
	res := normalizeBody(body)
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Total == nil || *res.Total != 12 {
		t.Fatalf("total = %v, want 12", res.Total)
	}
	if len(res.Columns) != 4 || res.Columns[0] != "id" || res.Columns[2] != "title" {
		t.Fatalf("columns = %v", res.Columns)
	}
	if len(res.Rows) != 2 || res.Rows[0][0] != "7" {
		t.Fatalf("rows = %v", res.Rows)
	}
}

func TestNormalizeBodyColumnsData(t *testing.T) {
	body := []byte(`{"columns":[{"Field":{"type":"string"}},{"Type":{"type":"string"}}],"data":[{"Field":"title","Type":"text"}]}`)
	res := normalizeBody(body)
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if len(res.Columns) != 2 || res.Columns[0] != "Field" || res.Columns[1] != "Type" {
		t.Fatalf("columns = %v", res.Columns)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] != "title" || res.Rows[0][1] != "text" {
		t.Fatalf("rows = %v", res.Rows)
	}
}

func TestNormalizeBodyErrorObjectAndText(t *testing.T) {
	res := normalizeBody([]byte(`{"error":"P01: syntax error, unexpected X"}`))
	if res.Error == "" {
		t.Fatal("expected error")
	}
	res = normalizeBody([]byte("ERROR 1064 (42000): sphinxql: syntax error near 'foo'"))
	if res.Error == "" {
		t.Fatal("expected plain-text error")
	}
}

func TestNormalizeBodyInsertAck(t *testing.T) {
	res := normalizeBody([]byte(`{"total":0,"error":"","warning":""}`))
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if len(res.Rows) != 1 || len(res.Columns) != 3 {
		t.Fatalf("expected single ack row, got columns=%v rows=%v", res.Columns, res.Rows)
	}
}
