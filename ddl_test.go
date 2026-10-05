package main

import (
	"strings"
	"testing"
)

func TestBuildCreateRT(t *testing.T) {
	sql, err := BuildCreateTable(CreateTableSpec{
		Name: "products",
		Kind: "rt",
		Columns: []ColumnDef{
			{Name: "title", Type: "text", Indexed: true, Stored: true},
			{Name: "price", Type: "float"},
			{Name: "tags", Type: "multi"},
		},
		Options: []OptionPair{
			{Name: "morphology", Value: "stem_en"},
			{Name: "min_infix_len", Value: "2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE `products` (`title` text, `price` float, `tags` multi) morphology='stem_en' min_infix_len='2'"
	if sql != want {
		t.Fatalf("got %s", sql)
	}
}

func TestBuildCreateTextFlagsAndVector(t *testing.T) {
	sql, err := BuildCreateTable(CreateTableSpec{
		Name:        "docs",
		IfNotExists: true,
		Engine:      "columnar",
		Columns: []ColumnDef{
			{Name: "body", Type: "text", Indexed: true},
			{Name: "note", Type: "text", Stored: true},
			{Name: "title", Type: "text", Indexed: true, Stored: true, Attribute: true},
			{Name: "meta", Type: "json", SecondaryIndex: true},
			{Name: "vec", Type: "float_vector", KnnType: "hnsw", KnnDims: 4, HnswSimilarity: "cosine", Engine: "columnar"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE IF NOT EXISTS `docs` (`body` text indexed, `note` text stored, `title` text indexed stored attribute, `meta` json secondary_index='1', `vec` float_vector knn_type='hnsw' knn_dims='4' hnsw_similarity='cosine' engine='columnar') engine='columnar'"
	if sql != want {
		t.Fatalf("got %s", sql)
	}
}

func TestBuildCreatePercolateAndDistributed(t *testing.T) {
	pq, err := BuildCreateTable(CreateTableSpec{
		Name:    "alerts",
		Kind:    "percolate",
		Columns: []ColumnDef{{Name: "title", Type: "text", Indexed: true, Stored: true}, {Name: "meta", Type: "json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pq != "CREATE TABLE `alerts` (`title` text, `meta` json) type='pq'" {
		t.Fatalf("pq: %s", pq)
	}
	dist, err := BuildCreateTable(CreateTableSpec{
		Name: "dist",
		Kind: "distributed",
		Members: []DistMember{
			{Kind: "local", Value: "a,b"},
			{Kind: "agent", Value: "127.0.0.1:9312:remote|10.0.0.2:9312:remote"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE `dist` type='distributed' local='a,b' agent='127.0.0.1:9312:remote|10.0.0.2:9312:remote'"
	if dist != want {
		t.Fatalf("dist: %s", dist)
	}
}

func TestBuildCreateRejectsInjection(t *testing.T) {
	_, err := BuildCreateTable(CreateTableSpec{
		Name:    "products; DROP TABLE x",
		Columns: []ColumnDef{{Name: "title", Type: "text", Indexed: true, Stored: true}},
	})
	if err == nil {
		t.Fatal("expected bad table name")
	}
	_, err = BuildCreateTable(CreateTableSpec{
		Name:    "products",
		Columns: []ColumnDef{{Name: "title", Type: "text", Indexed: true, Stored: true}},
		Options: []OptionPair{{Name: "morphology", Value: "stem_en'; DROP TABLE x"}},
	})
	if err == nil {
		t.Fatal("expected bad option value")
	}
	stmts, err := BuildSchemaChanges("t", []SchemaChange{{
		Action:   "setting",
		Settings: []OptionPair{{Name: "charset_table", Value: "0..9, english, _"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if stmts[0] != "ALTER TABLE `t` charset_table='0..9, english, _'" {
		t.Fatalf("charset: %s", stmts[0])
	}
	sql, err := BuildCreateTable(CreateTableSpec{
		Name:    "products",
		Columns: []ColumnDef{{Name: "title", Type: "text", Indexed: true, Stored: true}},
		Options: []OptionPair{{Name: "morphology", Value: "o'brien"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "morphology='o''brien'") {
		t.Fatalf("quote: %s", sql)
	}
	_, err = BuildCreateTable(CreateTableSpec{
		Name:    "products",
		Columns: []ColumnDef{{Name: "id", Type: "bigint"}},
	})
	if err == nil {
		t.Fatal("expected id rejection")
	}
}

func TestBuildSchemaChangesOrder(t *testing.T) {
	stmts, err := BuildSchemaChanges("products", []SchemaChange{
		{Action: "rename", NewName: "shop"},
		{Action: "add", Column: ColumnDef{Name: "stock", Type: "int"}},
		{Action: "drop", Column: ColumnDef{Name: "price"}},
		{Action: "modify", Column: ColumnDef{Name: "qty"}},
		{Action: "setting", Settings: []OptionPair{{Name: "morphology", Value: "stem_en"}, {Name: "min_infix_len", Value: "2"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ALTER TABLE `products` ADD COLUMN `stock` int",
		"ALTER TABLE `products` DROP COLUMN `price`",
		"ALTER TABLE `products` MODIFY COLUMN `qty` bigint",
		"ALTER TABLE `products` morphology='stem_en', min_infix_len='2'",
		"ALTER TABLE `products` RENAME `shop`",
	}
	if strings.Join(stmts, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(stmts, "\n"))
	}
}

func TestBuildSchemaChangesRejects(t *testing.T) {
	if _, err := BuildSchemaChanges("t", nil); err == nil {
		t.Fatal("expected empty")
	}
	if _, err := BuildSchemaChanges("t", []SchemaChange{{Action: "drop", Column: ColumnDef{Name: "id"}}}); err == nil {
		t.Fatal("expected id drop rejection")
	}
	sql, err := buildCreateLike("copy", "products", true)
	if err != nil {
		t.Fatal(err)
	}
	if sql != "CREATE TABLE `copy` LIKE `products` WITH DATA" {
		t.Fatalf("like: %s", sql)
	}
}
