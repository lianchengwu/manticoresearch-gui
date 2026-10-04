package main

import (
	"errors"
	"io"
	"strings"
	"time"

	openapi "github.com/manticoresoftware/manticoresearch-go"
)

// QueryService executes raw SQL against a Manticore server and normalizes
// every response shape into a QueryResult for the data grid.
type QueryService struct{}

func (QueryService) ExecuteSQL(connID string, sql string) (*QueryResult, error) {
	conn, err := connStore.get(connID)
	if err != nil {
		return nil, err
	}
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, errors.New("empty query")
	}
	client, err := buildClient(conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := callCtx(conn)
	defer cancel()

	start := time.Now()
	_, httpResp, err := client.UtilsAPI.Sql(ctx).Body(sql).Execute()
	took := time.Since(start).Seconds() * 1000
	if err != nil {
		return normalizeOrError(err, took)
	}
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	res := normalizeBody(body)
	res.TookMs = took
	return res, nil
}

// normalizeOrError salvages a valid JSON body even when the generated SDK's
// typed decode rejects it (it cannot represent every Manticore response
// shape, e.g. float _score or the columns/data SQL envelope).
func normalizeOrError(err error, tookMs float64) (*QueryResult, error) {
	res := &QueryResult{TookMs: tookMs}
	var gerr *openapi.GenericOpenAPIError
	if errors.As(err, &gerr) {
		normalized := normalizeBody(gerr.Body())
		switch {
		case normalized.Error != "":
			res.Error = normalized.Error
			return res, nil
		case len(normalized.Columns) > 0 || normalized.Message != "":
			normalized.TookMs = tookMs
			return normalized, nil
		}
	}
	res.Error = describeError(err).Error()
	return res, nil
}
