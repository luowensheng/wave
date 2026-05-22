package sqlite

import (
	"database/sql"
	"encoding/json"
	"github.com/luowensheng/wave/domain"
	"github.com/luowensheng/wave/io/http/contentloader"
	"github.com/luowensheng/wave/infra/common"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

type SQLiteStorageRef struct {
	db *sql.DB
}

var counter = 0

func printCounter(message ...any) {
	counter += 1
	fmt.Println(counter, " => ", message)
}

type SQLQueryType int

const (
	QueryTypeSelect SQLQueryType = iota
	QueryTypeInsert
	QueryTypeUpdate
	QueryTypeDelete
	QueryTypeExec
)

// ExecuteResult wraps different types of SQL execution results
type ExecuteResult struct {
	// Raw database results
	Rows   *sql.Rows
	Row    *sql.Row
	Result sql.Result
	Input  any
	User   *domain.PublicUser

	// Processed results
	Data interface{} // Can be map[string]any, []map[string]any, or scalar value

	// Metadata
	LastInsertID int64
	RowsAffected int64
	ColumnNames  []string
}

func determineQueryType(sqlStatement string) SQLQueryType {
	// Clean and normalize the SQL statement
	cleanSQL := strings.TrimSpace(sqlStatement)
	if cleanSQL == "" {
		return QueryTypeExec
	}

	// Multi-statement scripts (`UPDATE ...; SELECT ...`) classify based
	// on the LAST non-empty statement, so a trailing SELECT after a
	// write returns rows instead of RowsAffected. Splits on `;` while
	// preserving inline `;` inside quoted strings via a tiny scanner.
	if i := lastStatementStart(cleanSQL); i > 0 {
		cleanSQL = strings.TrimSpace(cleanSQL[i:])
	}

	cleanSQL = regexp.MustCompile(`\s+`).ReplaceAllString(cleanSQL, " ")
	cleanSQL = strings.ToUpper(cleanSQL)

	// Extract the first word (command)
	words := strings.Split(cleanSQL, " ")
	if len(words) == 0 {
		return QueryTypeExec
	}

	firstWord := words[0]

	switch firstWord {
	case "SELECT":
		return QueryTypeSelect
	case "INSERT":
		return QueryTypeInsert
	case "UPDATE":
		return QueryTypeUpdate
	case "DELETE":
		return QueryTypeDelete
	default:
		// For other statements like CREATE, DROP, ALTER, etc.
		return QueryTypeExec
	}
}
func isSingleRowQuery(sql string) bool {
	s := strings.ToUpper(strings.TrimSpace(sql))

	// Strip everything inside parens (subqueries) so the heuristics
	// below see only the OUTER query. Without this, a SELECT with a
	// `(... GROUP BY ...)` subquery would be classified as multi-row
	// even though the outer SELECT returns exactly one row.
	outer := stripParenContents(s)

	// 1. LIMIT must be exactly 1
	if strings.Contains(outer, "LIMIT 1") && !strings.Contains(outer, "LIMIT 10") && !strings.Contains(outer, "LIMIT 11") {
		return true
	}

	// 2. Aggregate functions WITHOUT GROUP BY → single row
	if strings.Contains(outer, "COUNT(") ||
		strings.Contains(outer, "SUM(") ||
		strings.Contains(outer, "AVG(") ||
		strings.Contains(outer, "MIN(") ||
		strings.Contains(outer, "MAX(") {

		if !strings.Contains(outer, "GROUP BY") {
			return true
		}
	}

	// 2b. Outer SELECT made entirely of scalar subqueries (each column
	// is a `(SELECT ...)` expression) → exactly one row, regardless of
	// what the subqueries do. Detect by: stripped outer has no FROM and
	// the original starts with SELECT.
	if strings.HasPrefix(outer, "SELECT") && !strings.Contains(outer, "FROM") {
		return true
	}

	// 3. Strict primary key match using regex (word boundary)
	re := regexp.MustCompile(`\b(ID|UUID|PK)\s*=\s*[^,\s]+`)
	if re.MatchString(s) {
		// reject if LIMIT > 1
		if strings.Contains(s, "LIMIT") && !strings.Contains(s, "LIMIT 1") {
			return false
		}
		return true
	}

	// 4. Explicit multi-row signals
	if strings.Contains(s, "LIMIT") && !strings.Contains(s, "LIMIT 1") {
		return false
	}

	if strings.Contains(s, "OFFSET") {
		return false
	}

	return false
}

// isScalarQuery determines if a SELECT query returns a single scalar value
// (one row, one column). Heuristic: the OUTER SELECT must contain an
// aggregate function AND have no top-level commas in its select-clause.
// Earlier versions just searched for the first FROM, which got fooled
// by subqueries — `SELECT (SELECT count(*) FROM x), (SELECT ...) FROM y`
// would extract just the first subquery as the "select clause" and
// classify the whole thing as scalar even though it returns 2 columns.
func isScalarQuery(sqlStatement string) bool {
	upperSQL := strings.ToUpper(strings.TrimSpace(sqlStatement))
	aggregateFunctions := []string{"COUNT(", "SUM(", "AVG(", "MIN(", "MAX(", "TOTAL("}
	hasAggregate := false
	for _, fn := range aggregateFunctions {
		if strings.Contains(upperSQL, fn) {
			hasAggregate = true
			break
		}
	}
	if !hasAggregate {
		return false
	}
	// Find the start of the outer SELECT's column list (after the
	// leading "SELECT") and the matching outer FROM at depth 0. Count
	// commas at depth 0 in between — any comma means multi-column.
	selectStart := strings.Index(upperSQL, "SELECT")
	if selectStart < 0 {
		return false
	}
	clauseStart := selectStart + len("SELECT")
	depth := 0
	commas := 0
	clauseEnd := -1
	for i := clauseStart; i < len(upperSQL); i++ {
		switch upperSQL[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				commas++
			}
		}
		// Match outer FROM as a whole word at depth 0.
		if depth == 0 && i+5 <= len(upperSQL) && upperSQL[i:i+4] == "FROM" {
			// confirm word boundary
			before := byte(' ')
			if i > 0 {
				before = upperSQL[i-1]
			}
			after := byte(' ')
			if i+4 < len(upperSQL) {
				after = upperSQL[i+4]
			}
			if !isSQLNameByte(before) && !isSQLNameByte(after) {
				clauseEnd = i
				break
			}
		}
	}
	_ = clauseEnd // (we only care about the comma count between selectStart and clauseEnd)
	return commas == 0
}

func isSQLNameByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

// processSelectResult processes SELECT query results into convenient Go types
func processSelectResult(rows *sql.Rows, isSingleRow bool) (interface{}, []string, error) {
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var results []map[string]any

	for rows.Next() {
		// Create a slice to hold the column values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))

		// Create pointers to the values
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan the row
		err := rows.Scan(valuePtrs...)
		if err != nil {
			return nil, nil, err
		}

		// Create map for this row
		rowMap := make(map[string]any)
		for i, col := range columns {
			val := values[i]

			// Convert byte slices to strings for better usability
			if b, ok := val.([]byte); ok {
				rowMap[col] = string(b)
			} else {
				rowMap[col] = val
			}
		}

		results = append(results, rowMap)
	}

	// Check for errors during iteration
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	// Return single map for single row, slice for multiple rows
	if isSingleRow && len(results) == 1 {
		return results[0], columns, nil
	}

	return results, columns, nil
}

// processScalarResult processes a single scalar value from SELECT
func processScalarResult(row *sql.Row) (interface{}, error) {
	var value interface{}
	err := row.Scan(&value)
	if err != nil {
		return nil, err
	}

	// Convert byte slices to strings
	if b, ok := value.([]byte); ok {
		return string(b), nil
	}

	return value, nil
}

// executeSQL handles both single- and multi-statement SQL.
// Multi-statement scripts (statements separated by `;`) are executed
// by running every preamble statement with db.Exec (ignoring their
// results), then handing the final statement to executeSingleSQL so it
// goes through the normal Select/Insert/Update/Delete dispatch and
// produces the usual ExecuteResult. Params are distributed across
// statements by counting `?` placeholders in each statement.
func (ref *SQLiteStorageRef) executeSQL(sqlStatement string, params ...interface{}) (*ExecuteResult, error) {
	stmts := splitStatements(sqlStatement)
	if len(stmts) <= 1 {
		return ref.executeSingleSQL(sqlStatement, params...)
	}

	paramOffset := 0
	for _, stmt := range stmts[:len(stmts)-1] {
		n := countParams(stmt)
		end := paramOffset + n
		if end > len(params) {
			return nil, fmt.Errorf("not enough params for preamble statement (need %d, have %d)", end, len(params))
		}
		if _, err := ref.db.Exec(stmt, params[paramOffset:end]...); err != nil {
			return nil, fmt.Errorf("preamble SQL failed: %w\nSQL: %s", err, stmt)
		}
		paramOffset = end
	}

	lastStmt := stmts[len(stmts)-1]
	return ref.executeSingleSQL(lastStmt, params[paramOffset:]...)
}

// executeSingleSQL executes a single SQL statement and returns an ExecuteResult.
func (ref *SQLiteStorageRef) executeSingleSQL(sqlStatement string, params ...interface{}) (*ExecuteResult, error) {
	if ref.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	// fmt.Println("sqlStatement: ", sqlStatement, " PARAMS: ", params)
	queryType := determineQueryType(sqlStatement)
	result := &ExecuteResult{}

	switch queryType {
	case QueryTypeSelect:
		if isScalarQuery(sqlStatement) {
			// Handle scalar queries (COUNT, SUM, etc.)
			row := ref.db.QueryRow(sqlStatement, params...)
			data, err := processScalarResult(row)
			if err != nil {
				return nil, err
			}
			result.Row = row
			result.Data = data
			return result, nil

		} else if isSingleRowQuery(sqlStatement) {
			// Handle single row queries that return multiple columns
			rows, err := ref.db.Query(sqlStatement, params...)
			if err != nil {
				return nil, err
			}

			data, columns, err := processSelectResult(rows, true)
			if err != nil {
				return nil, err
			}

			result.Data = data
			result.ColumnNames = columns
			return result, nil

		} else {
			// Handle multiple row queries
			rows, err := ref.db.Query(sqlStatement, params...)
			if err != nil {
				return nil, err
			}

			data, columns, err := processSelectResult(rows, false)
			if err != nil {
				return nil, err
			}

			result.Rows = rows
			result.Data = data
			result.ColumnNames = columns
			return result, nil
		}

	case QueryTypeInsert:
		sqlResult, err := ref.db.Exec(sqlStatement, params...)
		if err != nil {
			return nil, err
		}
		result.Result = sqlResult

		// Get last insert ID for INSERT statements
		if lastID, err := sqlResult.LastInsertId(); err == nil {
			result.LastInsertID = lastID
			result.Data = lastID // Return the ID as data for convenience
		}

		if rowsAffected, err := sqlResult.RowsAffected(); err == nil {
			result.RowsAffected = rowsAffected
		}

		return result, nil

	case QueryTypeUpdate, QueryTypeDelete:
		sqlResult, err := ref.db.Exec(sqlStatement, params...)
		if err != nil {
			return nil, err
		}
		result.Result = sqlResult

		if rowsAffected, err := sqlResult.RowsAffected(); err == nil {
			result.RowsAffected = rowsAffected
			result.Data = rowsAffected // Return rows affected as data for convenience
		}

		return result, nil

	default:
		// For DDL statements like CREATE, DROP, ALTER
		sqlResult, err := ref.db.Exec(sqlStatement, params...)
		if err != nil {
			return nil, err
		}
		result.Result = sqlResult

		// For DDL, return nil data but include any rows affected info
		if rowsAffected, err := sqlResult.RowsAffected(); err == nil {
			result.RowsAffected = rowsAffected
		}

		return result, nil
	}
}

// Close closes the database connection
func (ref *SQLiteStorageRef) Close() error {
	if ref.db != nil {
		return ref.db.Close()
	}
	return nil
}

// GetDB returns the underlying database connection for advanced operations
func (ref *SQLiteStorageRef) GetDB() *sql.DB {
	return ref.db
}

func Setup(storage *domain.StorageConfig) (*SQLiteStorageRef, error) {
	printCounter("Create directory if it doesn't exist")

	// Create directory if it doesn't exist
	dir := filepath.Dir(storage.Path)
	if dir != "." && dir != "/" && dir != "" {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	printCounter("Open database connection")

	// Open database connection
	db, err := sql.Open("sqlite3", storage.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database %s: %v", storage.Path, err)
	}

	// Test connection
	printCounter("Test connection")
	err = db.Ping()
	if err != nil {
		db.Close() // Close on error
		return nil, fmt.Errorf("failed to ping database %s: %v", storage.Path, err)
	}

	// Create the storage reference with the database connection
	ref := &SQLiteStorageRef{db: db}

	// Create tables
	printCounter("Create tables")
	for tableName, tableDef := range storage.Tables {
		if tableDef.Source != "" {
			ext := strings.ToLower(filepath.Ext(tableDef.Source))
			switch ext {
			case ".csv":
				err := createTableFromCSV(db, tableName, tableDef.Source)
				if err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("invalid extension for source: %s", ext)
			}
			continue
		}
		err := createTable(db, tableName, tableDef)
		if err != nil {
			db.Close() // Close on error
			return nil, fmt.Errorf("failed to create table %s: %v", tableName, err)
		}
		fmt.Printf("  Created table: %s\n", tableName)
	}

	printCounter("Done!")
	return ref, nil
}

// func createTableFromCSV(db *sql.DB, tableName string, path string) error {

// 	return nil
// }

func createTable(db *sql.DB, tableName string, tableDef *domain.TableDef) error {
	if tableDef == nil {
		return fmt.Errorf("table definition is nil for table %s", tableName)
	}

	if len(tableDef.Columns) == 0 {
		return fmt.Errorf("no columns defined for table %s", tableName)
	}

	// Build CREATE TABLE statement
	columns := strings.Join(tableDef.Columns, ", ")
	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", tableName, columns)

	fmt.Printf("    SQL: %s\n", createSQL)

	_, err := db.Exec(createSQL)
	return err
}

// Helper methods for common operations with processed results
func (ref *SQLiteStorageRef) QueryRow(sqlStatement string, params ...interface{}) *sql.Row {
	return ref.db.QueryRow(sqlStatement, params...)
}

func (ref *SQLiteStorageRef) Query(sqlStatement string, params ...interface{}) (*sql.Rows, error) {
	return ref.db.Query(sqlStatement, params...)
}

func (ref *SQLiteStorageRef) Exec(sqlStatement string, params ...interface{}) (sql.Result, error) {
	return ref.db.Exec(sqlStatement, params...)
}

// SelectOne executes a SELECT query and returns a single row as map[string]any
func (ref *SQLiteStorageRef) SelectOne(sqlStatement string, params ...interface{}) (map[string]any, error) {
	result, err := ref.executeSQL(sqlStatement, params...)
	if err != nil {
		return nil, err
	}

	if data, ok := result.Data.(map[string]any); ok {
		return data, nil
	}

	return nil, fmt.Errorf("query did not return a single row")
}

// SelectMany executes a SELECT query and returns multiple rows as []map[string]any
func (ref *SQLiteStorageRef) SelectMany(sqlStatement string, params ...interface{}) ([]map[string]any, error) {
	result, err := ref.executeSQL(sqlStatement, params...)
	if err != nil {
		return nil, err
	}

	// Handle both single row (convert to slice) and multiple rows
	if data, ok := result.Data.([]map[string]any); ok {
		return data, nil
	} else if data, ok := result.Data.(map[string]any); ok {
		return []map[string]any{data}, nil
	}

	return nil, fmt.Errorf("query did not return row data")
}

// SelectScalar executes a SELECT query and returns a scalar value (for COUNT, SUM, etc.)
func (ref *SQLiteStorageRef) SelectScalar(sqlStatement string, params ...interface{}) (interface{}, error) {
	result, err := ref.executeSQL(sqlStatement, params...)
	if err != nil {
		return nil, err
	}

	return result.Data, nil
}

// Insert executes an INSERT query and returns the last insert ID
func (ref *SQLiteStorageRef) Insert(sqlStatement string, params ...interface{}) (int64, error) {
	result, err := ref.executeSQL(sqlStatement, params...)
	if err != nil {
		return 0, err
	}

	return result.LastInsertID, nil
}

// Update executes an UPDATE query and returns the number of affected rows
func (ref *SQLiteStorageRef) Update(sqlStatement string, params ...interface{}) (int64, error) {
	result, err := ref.executeSQL(sqlStatement, params...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected, nil
}

// Delete executes a DELETE query and returns the number of affected rows
func (ref *SQLiteStorageRef) Delete(sqlStatement string, params ...interface{}) (int64, error) {
	result, err := ref.executeSQL(sqlStatement, params...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected, nil
}

// Execute processes a SQL template with named parameters and executes it safely
// This method prevents SQL injection by using parameterized queries
func (ref *SQLiteStorageRef) Execute(sqlStatement string, data *contentloader.DataLoader) (any, error) {
	if data == nil {
		// data = make(map[string]any)
		return nil, fmt.Errorf("missing data")
	}

	// data.Print()
	params := []any{}

	now := time.Now()

	var renderErr error

	tempValues := map[string]any{}

	funcMap := template.FuncMap{
		"getCurrentTime": func() string {
			value := now.UTC().Format("2006-01-02T15:04:05Z")
			params = append(params, value)
			return "?"
		},
		"getUser": func() *domain.PublicUser {
			user, err := data.GetUser()
			if err != nil {
				renderErr = err
				return &domain.PublicUser{}
			}
			return user
		},
		"error": func(err any) string {
			renderErr = fmt.Errorf("%v", err)
			return ""
		},
		"var": func(value any) string {
			params = append(params, value)
			return "?"
		},
		"getCurrentTimeLocal": func() string {
			value := now.Format("2006-01-02T15:04:05Z07:00") // Includes timezone
			params = append(params, value)
			return "?"
		},
		"formatTime": func(layout string) string {
			value := now.Format(layout)
			params = append(params, value)
			return "?"
		},
		"addDays": func(days int) string {
			value := now.AddDate(0, 0, days).Format("2006-01-02T15:04:05Z")
			params = append(params, value)
			return "?"
		},
		// bind takes any value and binds it as a SQL parameter, returning
		// the placeholder. Use when you have an in-template expression
		// (e.g. `getUser.Username`, a literal, an `index .Data 0`) and
		// want it bound safely. `wrap` differs: it treats its argument
		// as a *variable name* to look up in the inputs map.
		"bind": func(v any) string {
			if renderErr != nil {
				return "?"
			}
			params = append(params, v)
			return "?"
		},
		// jsonArray serializes an []any input (typically a `type: array`
		// declared input or a slice carried via `getUser.Roles`) as a
		// JSON literal that SQLite's `json_each` can iterate. Avoids
		// the default Go `%v` `[a b c]` stringification that breaks
		// downstream JSON parsing.
		"jsonArray": func(v any) string {
			if renderErr != nil {
				return "''"
			}
			b, err := json.Marshal(v)
			if err != nil {
				renderErr = fmt.Errorf("jsonArray: %w", err)
				return "''"
			}
			// Single-quoted SQL string literal. SQLite's JSON1 is happy
			// with double quotes inside single quotes.
			return "'" + strings.ReplaceAll(string(b), "'", "''") + "'"
		},
		"wrap": func(pattern string) string {
			if renderErr != nil {
				return "?"
			}

			// Extract prefix, variable name, and suffix from pattern
			prefix := ""
			suffix := ""
			varName := pattern

			// Extract leading punctuation (prefix)
			for len(varName) > 0 && isPunctuation(varName[0]) {
				prefix += string(varName[0])
				varName = varName[1:]
			}

			// Extract trailing punctuation (suffix)
			for len(varName) > 0 && isPunctuation(varName[len(varName)-1]) {
				suffix = string(varName[len(varName)-1]) + suffix
				varName = varName[:len(varName)-1]
			}

			// Get the value from data
			var value any
			var err error
			// common.PrintJSON(common.Object{
			// 	"varName":    varName,
			// 	"tempValues": tempValues,
			// })
			if strings.Contains(varName, "[") {
				var ok bool
				value, ok = tempValues[varName]
				if !ok {
					err = fmt.Errorf("iter item not found '%s'", varName)
				}

			} else {
				value, err = data.GetValue(varName)
			}
			if err != nil {
				renderErr = fmt.Errorf("variable '%s' not found: %s", varName, err.Error())
				return "?"
			}

			// Convert to string and wrap
			var strValue string
			if str, ok := value.(string); ok {
				strValue = str
			} else {
				strValue = fmt.Sprintf("%v", value)
			}

			wrappedValue := prefix + strValue + suffix
			params = append(params, wrappedValue)

			return "?"
		},
	}

	funcMap["get"] = func(key string) any {

		if strings.Contains(key, "[") {
			value, ok := tempValues[key]
			if !ok {
				// renderErr = fmt.Errorf("no value found for: '%s'", key)
				return "?"
			}
			params = append(params, value)
			return "?"
		}
		if fn, exists := funcMap[key]; exists {
			if callable, ok := fn.(func() string); ok {
				return callable()
			}
		}
		params = append(params, nil)

		return "?"
		// renderErr = fmt.Errorf("no value found for: '%s'", key)
		// return ""
	}

	funcMap["value"] = func(key string) string {
		if renderErr != nil {
			return "?"
		}
		val, err := data.GetValue(key)
		if err != nil {
			renderErr = fmt.Errorf("no value found for: '%s' -> %s", key, err.Error())
			return "?"
		}
		params = append(params, val)
		return "?"
	}

	funcMap["iterlist"] = func(key string) any {
		value, err := data.GetValue(key)
		if err != nil {
			renderErr = fmt.Errorf("no value found for: '%s' -> %s", key, err.Error())
			return nil
		}
		indices := []string{}
		list, found := value.([]any)
		if !found {
			renderErr = fmt.Errorf("non iterable type for: '%s'", key)
			return nil
		}

		for i := range list {
			itemKey := fmt.Sprintf("%s[%v]", key, i)
			tempValues[itemKey] = list[i]
			indices = append(indices, itemKey)

		}
		return indices
	}

	funcMap["itermap"] = func(key string) any {
		value, err := data.GetValue(key)
		if err != nil {
			renderErr = fmt.Errorf("no value found for: '%s' -> %s", key, err.Error())
			return nil
		}
		indices := []string{}
		list, found := value.(map[string]any)
		if !found {
			renderErr = fmt.Errorf("non iterable type for: '%s'", key)
			return nil
		}

		for i := range list {
			itemKey := fmt.Sprintf("%s[%v]", key, i)
			tempValues[itemKey] = list[i]
			indices = append(indices, itemKey)
		}
		return indices
	}

	funcMap["getindex"] = func(key string, index int) any {
		value, err := data.GetValue(key)
		if err != nil {
			renderErr = fmt.Errorf("no value found for: '%s' -> %s", key, err.Error())
			return nil
		}
		list, found := value.([]any)
		if !found {
			renderErr = fmt.Errorf("non iterable type for: '%s'", key)
			return nil
		}
		if index < 0 || index >= len(list) {
			renderErr = fmt.Errorf("invalid index (%v) for: '%s'", index, key)
			return nil
		}
		params = append(params, list[index])
		return "?"
	}
	funcMap["pathVar"] = func(varName string) string {
		return data.GetPathVar(varName)
	}

	// Add hasvalue function to your funcMap
	funcMap["hasvalue"] = func(varName string) bool {
		common.PrintJSON(common.Object{
			"varName": varName,
		})
		if renderErr != nil {
			return false
		}

		value, err := data.GetValue(varName)
		if err != nil {
			return false
		}

		// common.PrintJSON(common.Object{
		// 	"varName": varName,
		// 	"value":   value,
		// })

		// Check for various "empty" conditions
		switch v := value.(type) {
		case string:
			return strings.TrimSpace(v) != ""
		case nil:
			return false
		case []any:
			return len(v) > 0
		case map[string]any:
			return len(v) > 0

		case int, int32, int64:
			return v != 0
		case float32, float64:
			return v != 0.0
		default:
			return true // Non-empty value
		}
	}

	// Optional: Add hasvalues for multiple variables
	funcMap["hasvalues"] = func(varNames ...string) bool {
		for _, varName := range varNames {
			if !funcMap["hasvalue"].(func(string) bool)(varName) {
				return false
			}
		}
		return true
	}

	// Optional: Add hasanyvalue for OR logic
	funcMap["hasanyvalue"] = func(varNames ...string) bool {
		for _, varName := range varNames {
			if funcMap["hasvalue"].(func(string) bool)(varName) {
				return true
			}
		}
		return false
	}
	// paramIndex := 0

	// Create template functions for each parameter

	for key, value := range data.GetValues() {
		// Capture the value in the closure
		paramValue := value
		// paramName := key

		funcMap[key] = func() string {
			if renderErr != nil {
				return "?" // Don't add more params if there's already an error
			}

			params = append(params, paramValue)
			// paramIndex++
			return "?"
		}
	}

	// raw returns the raw declared-input value (any type) without binding
	// it as a SQL parameter. Use to feed values into helpers like
	// jsonArray that need the actual Go value, not a `?` placeholder.
	// Example: {{ jsonArray (raw "tags") }}.
	funcMap["raw"] = func(key string) any {
		v, err := data.GetValue(key)
		if err != nil {
			renderErr = fmt.Errorf("raw: %w", err)
			return nil
		}
		return v
	}

	// Render the template. Use the DataLoader as context (not the values
	// map) so that only the registered {{name}} function-call syntax works.
	// Dot-notation ({{.name}}) would require the DataLoader to have exported
	// fields matching input names, which it doesn't — this deliberately
	// prevents un-parameterised value injection into SQL.
	tmpl, err := template.New("sql").Funcs(funcMap).Parse(sqlStatement)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL template: %v", err)
	}

	var rendered strings.Builder
	err = tmpl.Execute(&rendered, data)
	if err != nil {
		return nil, fmt.Errorf("failed to render SQL template: %v -> [\n%s\n]\n\n", err, rendered.String())
	}

	if renderErr != nil {
		return nil, renderErr
	}

	renderedSQL := removeEmptyLines(rendered.String())

	fmt.Println("RENDERED:\n```", renderedSQL, "\n```")

	result, err := ref.executeSQL(renderedSQL, params...)
	if err != nil {
		return nil, err
	}
	result.Input = data
	result.User, _ = data.GetUser()
	return result, nil
}

// Transaction support
func (ref *SQLiteStorageRef) Begin() (*sql.Tx, error) {
	return ref.db.Begin()
}
