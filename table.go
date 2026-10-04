package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	openapi "github.com/manticoresoftware/manticoresearch-go"
)

// TableService covers table inspection and document CRUD. Browsing uses the
// Search API (DSL), writes use the Index API, schema uses SQL endpoints.
type TableService struct{}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func runSQL(conn *Connection, sql string) (*QueryResult, error) {
	return QueryService{}.ExecuteSQL(conn.ID, sql)
}

// ListTables returns the sidebar table list via SHOW TABLES.
func (t TableService) ListTables(connID string) ([]TableInfo, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	res, err := runSQL(conn, "SHOW TABLES")
	if err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	typeCol := -1
	for i, c := range res.Columns {
		if strings.EqualFold(c, "type") {
			typeCol = i
			break
		}
	}
	if typeCol < 0 && len(res.Columns) > 1 {
		typeCol = 1
	}
	tables := make([]TableInfo, 0, len(res.Rows))
	for _, row := range res.Rows {
		if len(row) == 0 {
			continue
		}
		name := fmt.Sprintf("%v", row[0])
		typ := ""
		if typeCol > 0 && typeCol < len(row) && row[typeCol] != nil {
			typ = fmt.Sprintf("%v", row[typeCol])
		}
		tables = append(tables, TableInfo{Name: name, Type: typ})
	}
	return tables, nil
}

// DescribeTable returns the schema (DESCRIBE) of a table.
func (t TableService) DescribeTable(connID, table string) (*QueryResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	return runSQL(conn, "DESCRIBE "+quoteIdent(table))
}

// ShowCreateTable returns the full CREATE statement of a table.
func (t TableService) ShowCreateTable(connID, table string) (*QueryResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	return runSQL(conn, "SHOW CREATE TABLE "+quoteIdent(table))
}

// TruncateTable empties a table.
func (t TableService) TruncateTable(connID, table string) (*QueryResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	return runSQL(conn, "TRUNCATE TABLE "+quoteIdent(table))
}

// DropTable deletes a table.
func (t TableService) DropTable(connID, table string) (*QueryResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	return runSQL(conn, "DROP TABLE "+quoteIdent(table))
}

// BrowseDocuments lists documents with pagination, optional full-text query
// and sorting, through the Search API.
func (t TableService) BrowseDocuments(connID string, opts BrowseOptions) (*QueryResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 50
	}
	page := opts.Page
	if page < 0 {
		page = 0
	}
	limit := int32(pageSize)
	offset := int32(page * pageSize)

	req := openapi.SearchRequest{
		Table:  &opts.Table,
		Limit:  &limit,
		Offset: &offset,
	}
	if q := strings.TrimSpace(opts.Query); q != "" {
		req.Query = &openapi.SearchQuery{QueryString: &q}
	} else {
		req.Query = &openapi.SearchQuery{MatchAll: map[string]any{}}
	}
	if sortBy := strings.TrimSpace(opts.SortBy); sortBy != "" && identRe.MatchString(sortBy) {
		order := "desc"
		if opts.SortAsc {
			order = "asc"
		}
		req.Sort = []map[string]any{{sortBy: map[string]string{"order": order}}}
	}

	client, err := buildClient(conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := callCtx(conn)
	defer cancel()

	start := time.Now()
	_, httpResp, err := client.SearchAPI.Search(ctx).SearchRequest(req).Execute()
	took := time.Since(start).Seconds() * 1000
	if err != nil {
		return normalizeOrError(err, took)
	}
	body, err := readAll(httpResp)
	if err != nil {
		return nil, err
	}
	res := normalizeBody(body)
	res.TookMs = took
	return res, nil
}

// GetDocument fetches one document by id (numeric or string, e.g. percolate).
func (t TableService) GetDocument(connID, table, id string) (*QueryResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	var where string
	if isNumericID(id) {
		where = id
	} else {
		where = "'" + strings.ReplaceAll(id, "'", "''") + "'"
	}
	return runSQL(conn, fmt.Sprintf("SELECT * FROM %s WHERE id = %s", quoteIdent(table), where))
}

// InsertDocument adds a document; id may be empty for auto-assignment.
func (t TableService) InsertDocument(connID, table string, id string, doc map[string]any) (*MutationResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	req := openapi.InsertDocumentRequest{Table: table, Doc: doc}
	if doc == nil {
		return nil, errors.New("文档内容不能为空")
	}
	if isNumericID(id) {
		n, _ := strconv.ParseUint(id, 10, 64)
		req.Id = &n
	}
	client, err := buildClient(conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := callCtx(conn)
	defer cancel()
	resp, _, err := client.IndexAPI.Insert(ctx).InsertDocumentRequest(req).Execute()
	if err != nil {
		return nil, describeError(err)
	}
	msg := "文档已创建"
	if resp != nil && resp.Result != nil && *resp.Result != "" {
		msg = fmt.Sprintf("文档已创建 (%s)", *resp.Result)
	}
	return &MutationResult{Message: msg}, nil
}

// ReplaceDocument fully replaces a document.
func (t TableService) ReplaceDocument(connID, table, id string, doc map[string]any) (*MutationResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("无效的文档 id: %s", id)
	}
	if doc == nil {
		return nil, errors.New("文档内容不能为空")
	}
	req := openapi.InsertDocumentRequest{Table: table, Doc: doc, Id: &n}
	client, err := buildClient(conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := callCtx(conn)
	defer cancel()
	resp, _, err := client.IndexAPI.Replace(ctx).InsertDocumentRequest(req).Execute()
	if err != nil {
		return nil, describeError(err)
	}
	msg := "文档已替换"
	if resp != nil && resp.Result != nil && *resp.Result != "" {
		msg = fmt.Sprintf("文档已替换 (%s)", *resp.Result)
	}
	return &MutationResult{Message: msg}, nil
}

// UpdateDocument updates the given (non full-text) fields of a document.
func (t TableService) UpdateDocument(connID, table, id string, doc map[string]any) (*MutationResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("无效的文档 id: %s", id)
	}
	if doc == nil {
		return nil, errors.New("文档内容不能为空")
	}
	req := openapi.UpdateDocumentRequest{Table: table, Doc: doc, Id: &n}
	client, err := buildClient(conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := callCtx(conn)
	defer cancel()
	resp, _, err := client.IndexAPI.Update(ctx).UpdateDocumentRequest(req).Execute()
	if err != nil {
		return nil, describeError(err)
	}
	updated := int32(0)
	if resp != nil && resp.Updated != nil {
		updated = *resp.Updated
	}
	return &MutationResult{Message: fmt.Sprintf("已更新 %d 条", updated)}, nil
}

// DeleteDocument removes a document by id.
func (t TableService) DeleteDocument(connID, table, id string) (*MutationResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	req := openapi.DeleteDocumentRequest{Table: table}
	if isNumericID(id) {
		n, _ := strconv.ParseUint(id, 10, 64)
		req.Id = &n
	} else {
		req.Query = map[string]any{"equals": map[string]any{"id": id}}
	}
	client, err := buildClient(conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := callCtx(conn)
	defer cancel()
	resp, _, err := client.IndexAPI.Delete(ctx).DeleteDocumentRequest(req).Execute()
	if err != nil {
		return nil, describeError(err)
	}
	deleted := int32(0)
	if resp != nil && resp.Deleted != nil {
		deleted = *resp.Deleted
	}
	return &MutationResult{Message: fmt.Sprintf("已删除 %d 条", deleted)}, nil
}

func readAll(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	return io.ReadAll(resp.Body)
}

func isNumericID(id string) bool {
	if id == "" {
		return false
	}
	_, err := strconv.ParseUint(id, 10, 64)
	return err == nil
}
