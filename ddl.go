package main

import (
	"fmt"
	"strings"
	"unicode"
)

// ColumnDef is one column in CREATE TABLE or ALTER TABLE ADD COLUMN.
type ColumnDef struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Indexed        bool   `json:"indexed"`
	Stored         bool   `json:"stored"`
	Attribute      bool   `json:"attribute"`
	Engine         string `json:"engine"` // "", "columnar", "rowwise"
	SecondaryIndex bool   `json:"secondaryIndex"`
	KnnType        string `json:"knnType"`
	KnnDims        int    `json:"knnDims"`
	HnswSimilarity string `json:"hnswSimilarity"`
}

// OptionPair is one table setting, rendered as name='value'.
type OptionPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DistMember is one local= or agent= clause of a distributed table.
type DistMember struct {
	Kind  string `json:"kind"` // local | agent
	Value string `json:"value"`
}

// CreateTableSpec is the visual designer's create request.
type CreateTableSpec struct {
	Name        string       `json:"name"`
	IfNotExists bool         `json:"ifNotExists"`
	Kind        string       `json:"kind"` // rt | pq | distributed
	Columns     []ColumnDef  `json:"columns"`
	Options     []OptionPair `json:"options"`
	Members     []DistMember `json:"members"`
	Engine      string       `json:"engine"` // table-level columnar
}

// SchemaChange is one pending ALTER against an existing table.
// Action is add, drop, modify, setting, or rename.
type SchemaChange struct {
	Action   string       `json:"action"`
	Column   ColumnDef    `json:"column"`
	NewName  string       `json:"newName"`
	Settings []OptionPair `json:"settings"`
}

// SQLPreview is a rendered statement that was not executed.
type SQLPreview struct {
	SQL   string   `json:"sql"`
	SQLs  []string `json:"sqls"`
	Error string   `json:"error"`
}

// SchemaApplyResult is the outcome of a batch of ALTER statements.
// Applied counts statements that succeeded before the first failure.
type SchemaApplyResult struct {
	Applied   int    `json:"applied"`
	Total     int    `json:"total"`
	Error     string `json:"error"`
	FailedSQL string `json:"failedSql"`
	Message   string `json:"message"`
}

var columnTypeSQL = map[string]string{
	"text":         "text",
	"string":       "string",
	"int":          "int",
	"integer":      "int",
	"uint":         "int",
	"bigint":       "bigint",
	"float":        "float",
	"bool":         "bool",
	"boolean":      "bool",
	"json":         "json",
	"multi":        "multi",
	"mva":          "multi",
	"multi64":      "multi64",
	"mva64":        "multi64",
	"timestamp":    "timestamp",
	"float_vector": "float_vector",
}

func validIdent(name string) bool {
	if name == "@timestamp" || name == "@version" {
		return true
	}
	if name == "" || len(name) > 200 {
		return false
	}
	for i, r := range name {
		if r < 0x20 || strings.ContainsRune("`'\";\\ ", r) {
			return false
		}
		if i == 0 {
			if !identStart(r) {
				return false
			}
			continue
		}
		if !identCont(r) {
			return false
		}
	}
	return true
}

func identStart(r rune) bool {
	return r == '_' || isASCIILetter(r) || (r > 0x7f && unicode.IsLetter(r))
}

func identCont(r rune) bool {
	return r == '_' || isASCIILetter(r) || (r >= '0' && r <= '9') || (r > 0x7f && (unicode.IsLetter(r) || unicode.IsNumber(r)))
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func checkIdent(name, what string) (string, error) {
	name = strings.TrimSpace(name)
	if !validIdent(name) {
		return "", fmt.Errorf("非法%s %q", what, name)
	}
	return name, nil
}

func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func validOptionValue(s string) bool {
	if s == "" || len(s) > 8000 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == ';' {
			return false
		}
	}
	return true
}

func validAgent(s string) bool {
	if s == "" || len(s) > 1024 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == '\'' || r == '`' || r == ';' || r == '\\' {
			return false
		}
	}
	return true
}

func validLocalList(s string) bool {
	parts := strings.Split(s, ",")
	if len(parts) == 0 || strings.TrimSpace(s) == "" {
		return false
	}
	for _, p := range parts {
		if !validIdent(strings.TrimSpace(p)) {
			return false
		}
	}
	return true
}

func columnSQL(c ColumnDef) (string, error) {
	name, err := checkIdent(c.Name, "列名")
	if err != nil {
		return "", err
	}
	if strings.EqualFold(name, "id") {
		return "", fmt.Errorf("不必声明 id，Manticore 会自动创建")
	}
	typ, ok := columnTypeSQL[strings.ToLower(strings.TrimSpace(c.Type))]
	if !ok {
		return "", fmt.Errorf("不支持的列类型 %q", c.Type)
	}
	var b strings.Builder
	b.WriteString(quoteIdent(name))
	b.WriteByte(' ')
	b.WriteString(typ)
	if typ == "text" {
		if !c.Indexed && !c.Stored && !c.Attribute {
			return "", fmt.Errorf("列 %s 的 text 至少要索引、存储或属性之一", name)
		}
		// Bare `text` is indexed+stored. Emit flags only when that default changes.
		if !c.Indexed || !c.Stored || c.Attribute {
			if c.Indexed {
				b.WriteString(" indexed")
			}
			if c.Stored {
				b.WriteString(" stored")
			}
			if c.Attribute {
				b.WriteString(" attribute")
			}
		}
	}
	if typ == "json" && c.SecondaryIndex {
		b.WriteString(" secondary_index='1'")
	}
	if typ == "float_vector" && (c.KnnType != "" || c.KnnDims > 0 || c.HnswSimilarity != "") {
		if c.KnnDims <= 0 {
			return "", fmt.Errorf("列 %s 设置了 KNN 但缺少 knn_dims", name)
		}
		knnType := strings.ToLower(strings.TrimSpace(c.KnnType))
		if knnType == "" {
			knnType = "hnsw"
		}
		if knnType != "hnsw" {
			return "", fmt.Errorf("不支持的 knn_type %q", c.KnnType)
		}
		sim := strings.ToLower(strings.TrimSpace(c.HnswSimilarity))
		if sim != "" && sim != "cosine" && sim != "l2" && sim != "ip" {
			return "", fmt.Errorf("不支持的 hnsw_similarity %q", c.HnswSimilarity)
		}
		b.WriteString(" knn_type='hnsw'")
		fmt.Fprintf(&b, " knn_dims='%d'", c.KnnDims)
		if sim != "" {
			b.WriteString(" hnsw_similarity='")
			b.WriteString(sim)
			b.WriteByte('\'')
		}
	}
	switch strings.ToLower(strings.TrimSpace(c.Engine)) {
	case "":
	case "columnar", "rowwise":
		b.WriteString(" engine='")
		b.WriteString(strings.ToLower(strings.TrimSpace(c.Engine)))
		b.WriteByte('\'')
	default:
		return "", fmt.Errorf("非法存储引擎 %q", c.Engine)
	}
	return b.String(), nil
}

func optionParts(opts []OptionPair) ([]string, error) {
	var parts []string
	seen := map[string]bool{}
	for _, o := range opts {
		name := strings.ToLower(strings.TrimSpace(o.Name))
		val := strings.TrimSpace(o.Value)
		if name == "" && val == "" {
			continue
		}
		if name == "" || val == "" {
			return nil, fmt.Errorf("选项名和值必须同时填写")
		}
		if !validIdent(name) {
			return nil, fmt.Errorf("非法选项名 %q", o.Name)
		}
		if !validOptionValue(val) {
			return nil, fmt.Errorf("选项 %s 的值含有非法字符", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		parts = append(parts, name+"="+quoteSQLString(val))
	}
	return parts, nil
}

// BuildCreateTable renders a CREATE TABLE statement. It does not execute it.
func BuildCreateTable(spec CreateTableSpec) (string, error) {
	name, err := checkIdent(spec.Name, "表名")
	if err != nil {
		return "", err
	}
	kind := strings.ToLower(strings.TrimSpace(spec.Kind))
	if kind == "" || kind == "rt" || kind == "realtime" {
		kind = "rt"
	}
	if kind == "percolate" {
		kind = "pq"
	}
	var b strings.Builder
	b.WriteString("CREATE TABLE ")
	if spec.IfNotExists {
		b.WriteString("IF NOT EXISTS ")
	}
	b.WriteString(quoteIdent(name))
	switch kind {
	case "rt", "pq":
		if len(spec.Columns) == 0 {
			return "", fmt.Errorf("至少需要一个字段")
		}
		seen := map[string]bool{}
		b.WriteString(" (")
		for i, col := range spec.Columns {
			cs, err := columnSQL(col)
			if err != nil {
				return "", err
			}
			key := strings.ToLower(strings.TrimSpace(col.Name))
			if seen[key] {
				return "", fmt.Errorf("重复的列名 %s", strings.TrimSpace(col.Name))
			}
			seen[key] = true
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(cs)
		}
		b.WriteByte(')')
		var opts []OptionPair
		if eng := strings.ToLower(strings.TrimSpace(spec.Engine)); eng != "" {
			if eng != "columnar" && eng != "rowwise" {
				return "", fmt.Errorf("非法表引擎 %q", spec.Engine)
			}
			opts = append(opts, OptionPair{Name: "engine", Value: eng})
		}
		if kind == "pq" {
			opts = append(opts, OptionPair{Name: "type", Value: "pq"})
		}
		opts = append(opts, spec.Options...)
		parts, err := optionParts(opts)
		if err != nil {
			return "", err
		}
		if len(parts) > 0 {
			b.WriteByte(' ')
			b.WriteString(strings.Join(parts, " "))
		}
	case "distributed":
		if len(spec.Members) == 0 {
			return "", fmt.Errorf("分布式表至少需要一个 local 或 agent")
		}
		b.WriteString(" type='distributed'")
		for _, m := range spec.Members {
			mk := strings.ToLower(strings.TrimSpace(m.Kind))
			val := strings.TrimSpace(m.Value)
			switch mk {
			case "local":
				if !validLocalList(val) {
					return "", fmt.Errorf("非法 local 表名 %q", m.Value)
				}
				b.WriteString(" local=")
				b.WriteString(quoteSQLString(val))
			case "agent":
				if !validAgent(val) {
					return "", fmt.Errorf("非法 agent %q", m.Value)
				}
				b.WriteString(" agent=")
				b.WriteString(quoteSQLString(val))
			default:
				return "", fmt.Errorf("非法分布式成员类型 %q", m.Kind)
			}
		}
	default:
		return "", fmt.Errorf("不支持的表类型 %q", spec.Kind)
	}
	return b.String(), nil
}

// BuildSchemaChanges renders ALTER statements. Rename is always last so
// earlier statements still address the original table.
func BuildSchemaChanges(table string, changes []SchemaChange) ([]string, error) {
	table, err := checkIdent(table, "表名")
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("没有变更")
	}
	var stmts []string
	var rename string
	for _, ch := range changes {
		action := strings.ToLower(strings.TrimSpace(ch.Action))
		switch action {
		case "add":
			cs, err := columnSQL(ch.Column)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, "ALTER TABLE "+quoteIdent(table)+" ADD COLUMN "+cs)
		case "drop":
			name, err := checkIdent(ch.Column.Name, "列名")
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(name, "id") {
				return nil, fmt.Errorf("不能删除 id 列")
			}
			stmts = append(stmts, "ALTER TABLE "+quoteIdent(table)+" DROP COLUMN "+quoteIdent(name))
		case "modify":
			name, err := checkIdent(ch.Column.Name, "列名")
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, "ALTER TABLE "+quoteIdent(table)+" MODIFY COLUMN "+quoteIdent(name)+" bigint")
		case "setting":
			parts, err := optionParts(ch.Settings)
			if err != nil {
				return nil, err
			}
			if len(parts) == 0 {
				return nil, fmt.Errorf("全文设置不能为空")
			}
			stmts = append(stmts, "ALTER TABLE "+quoteIdent(table)+" "+strings.Join(parts, ", "))
		case "rename":
			if rename != "" {
				return nil, fmt.Errorf("一次只能重命名一次")
			}
			rename, err = checkIdent(ch.NewName, "新表名")
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(rename, table) {
				return nil, fmt.Errorf("新表名与当前表名相同")
			}
		default:
			return nil, fmt.Errorf("不支持的变更 %q", ch.Action)
		}
	}
	if rename != "" {
		stmts = append(stmts, "ALTER TABLE "+quoteIdent(table)+" RENAME "+quoteIdent(rename))
	}
	if len(stmts) == 0 {
		return nil, fmt.Errorf("没有变更")
	}
	return stmts, nil
}

func buildCreateLike(name, like string, withData bool) (string, error) {
	name, err := checkIdent(name, "表名")
	if err != nil {
		return "", err
	}
	like, err = checkIdent(like, "源表名")
	if err != nil {
		return "", err
	}
	sql := "CREATE TABLE " + quoteIdent(name) + " LIKE " + quoteIdent(like)
	if withData {
		sql += " WITH DATA"
	}
	return sql, nil
}

func previewErr(err error) *SQLPreview {
	return &SQLPreview{SQLs: []string{}, Error: err.Error()}
}

func previewOK(sqls []string) *SQLPreview {
	if sqls == nil {
		sqls = []string{}
	}
	return &SQLPreview{SQL: strings.Join(sqls, ";\n"), SQLs: sqls}
}
