package repository

import "embed"

// QueriesFS contains production SQL for diagnostic EXPLAIN plans.
//
//go:embed queries/sets.sql
var QueriesFS embed.FS
