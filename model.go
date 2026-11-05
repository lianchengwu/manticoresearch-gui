package main

import (
	"manticoresearch-gui/internal/ddl"
	"manticoresearch-gui/internal/model"
)

// Data and network models
type Connection = model.Connection
type Hop = model.Hop
type LegacyNet = model.LegacyNet
type ProxyHop = model.ProxyHop
type LegacySSH = model.LegacySSH
type NodeAddress = model.NodeAddress
type QueryResult = model.QueryResult
type TableInfo = model.TableInfo
type BrowseOptions = model.BrowseOptions
type MutationResult = model.MutationResult

// DDL models
type ColumnDef = ddl.ColumnDef
type OptionPair = ddl.OptionPair
type DistMember = ddl.DistMember
type CreateTableSpec = ddl.CreateTableSpec
type SchemaChange = ddl.SchemaChange
type SQLPreview = ddl.SQLPreview
type SchemaApplyResult = ddl.SchemaApplyResult
