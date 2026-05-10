// Package kinds — storage_legacy_adapter bridges the new typed
// StoragePlugin into the legacy usecases/storage_access.StorageRef
// interface so plugin-backed storage backends can be referenced from
// the existing storage_access route type without any route-side change.
package kinds

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"wave/io/http/contentloader"
)

// storageExecuteTimeout caps the per-request RPC time. Long enough for
// legitimate work, short enough that a hung plugin doesn't pin a
// goroutine forever.
const storageExecuteTimeout = 30 * time.Second

// SQLQueryType mirrors infra/sqlite.SQLQueryType — kept local so the
// adapter has no dependency on infra/sqlite.
type SQLQueryType int

const (
	queryTypeSelect SQLQueryType = iota
	queryTypeInsert
	queryTypeUpdate
	queryTypeDelete
	queryTypeExec
)

// StorageRefAdapter wraps a StoragePlugin so it satisfies the legacy
// storage_access.StorageRef "Execute(template, data)" interface used by
// today's storage_access route. It renders the SQL template, infers the
// query type, and dispatches to the plugin's Query method.
//
// Returned shapes mirror infra/sqlite for back-compat:
//   - SELECT (1 row)            → map[string]any
//   - SELECT (N rows)           → []map[string]any
//   - SELECT (single col/row)   → scalar value
//   - INSERT                    → {"id", "rows_affected"}
//   - UPDATE / DELETE           → {"rows_affected"}
//   - other EXEC                → nil
type StorageRefAdapter struct {
	Plugin StoragePlugin
}

// Execute renders command as a Go text/template (substituting from
// data.GetValues()), determines the query type, and dispatches the
// resulting SQL to the underlying plugin.
func (a *StorageRefAdapter) Execute(command string, data *contentloader.DataLoader) (any, error) {
	if a == nil || a.Plugin == nil {
		return nil, fmt.Errorf("storage plugin is nil")
	}
	rendered, err := renderSQLTemplate(command, data)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), storageExecuteTimeout)
	defer cancel()

	res, err := a.Plugin.Query(ctx, &Query{SQL: rendered})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}

	switch determineQueryType(rendered) {
	case queryTypeSelect:
		switch len(res.Rows) {
		case 0:
			return []map[string]any{}, nil
		case 1:
			row := res.Rows[0]
			if len(res.Columns) == 1 && len(row) == 1 {
				for _, v := range row {
					return v, nil
				}
			}
			return row, nil
		default:
			return res.Rows, nil
		}
	case queryTypeInsert:
		return map[string]any{
			"id":            res.LastInsertID,
			"rows_affected": res.RowsAffected,
		}, nil
	case queryTypeUpdate, queryTypeDelete:
		return map[string]any{
			"rows_affected": res.RowsAffected,
		}, nil
	default:
		return nil, nil
	}
}

// renderSQLTemplate renders command as a Go text/template using values
// supplied by the DataLoader. Phase 2 deliberately keeps this simple:
// substitution-based, no parameter binding. Plugins that need
// parameterised queries will get proper named-param support in a
// follow-up phase.
func renderSQLTemplate(command string, data *contentloader.DataLoader) (string, error) {
	values := map[string]any{}
	if data != nil {
		for k, v := range data.GetValues() {
			values[k] = v
		}
	}

	tmpl, err := template.New("sql").Option("missingkey=zero").Parse(command)
	if err != nil {
		return "", fmt.Errorf("parse SQL template: %w", err)
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, values); err != nil {
		return "", fmt.Errorf("render SQL template: %w", err)
	}
	return sb.String(), nil
}

var sqlWhitespaceRE = regexp.MustCompile(`\s+`)

// determineQueryType inspects the leading keyword of a SQL statement.
// Mirrors infra/sqlite.determineQueryType.
func determineQueryType(stmt string) SQLQueryType {
	clean := strings.TrimSpace(stmt)
	if clean == "" {
		return queryTypeExec
	}
	clean = sqlWhitespaceRE.ReplaceAllString(clean, " ")
	clean = strings.ToUpper(clean)
	first := strings.SplitN(clean, " ", 2)[0]
	switch first {
	case "SELECT":
		return queryTypeSelect
	case "INSERT":
		return queryTypeInsert
	case "UPDATE":
		return queryTypeUpdate
	case "DELETE":
		return queryTypeDelete
	default:
		return queryTypeExec
	}
}
