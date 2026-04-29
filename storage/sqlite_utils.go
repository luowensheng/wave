package storage

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func removeEmptyLines(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

func isPunctuation(c byte) bool {
	// Add any punctuation you want to support
	return c == '%' || c == '*' || c == '?' || c == '+' || c == '-' || c == '_'
}

func createTableFromCSV(db *sql.DB, tableName string, path string) error {
	// Open and read the CSV file
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open CSV file %s: %v", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read the header row to get column names
	headers, err := reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV headers from %s: %v", path, err)
	}

	// Clean headers (remove whitespace, handle empty columns)
	for i, header := range headers {
		headers[i] = strings.TrimSpace(header)
		if headers[i] == "" {
			headers[i] = fmt.Sprintf("column_%d", i+1)
		}
	}

	// Ensure no duplicate column names
	seenNames := make(map[string]bool)
	for i, header := range headers {
		originalHeader := header
		counter := 1
		for seenNames[header] {
			header = fmt.Sprintf("%s_%d", originalHeader, counter)
			counter++
		}
		headers[i] = header
		seenNames[header] = true
	}

	// Read a few sample rows to infer data types
	var sampleRows [][]string
	sampleSize := 100 // Sample first 100 rows for type inference

	for i := 0; i < sampleSize; i++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV row from %s: %v", path, err)
		}
		sampleRows = append(sampleRows, row)
	}

	// Infer column types based on sample data
	columnTypes := make([]string, len(headers))
	for i := range headers {
		columnTypes[i] = inferColumnType(sampleRows, i)
	}

	// Create the table with inferred schema
	err = createTableWithSchema(db, tableName, headers, columnTypes)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %v", tableName, err)
	}

	// Reopen file to read all data for insertion
	file.Close()
	file, err = os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to reopen CSV file %s: %v", path, err)
	}
	defer file.Close()

	reader = csv.NewReader(file)

	// Skip header row
	_, err = reader.Read()
	if err != nil {
		return fmt.Errorf("failed to skip header row: %v", err)
	}

	// Prepare insert statement
	placeholders := make([]string, len(headers))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(headers, ", "),
		strings.Join(placeholders, ", "))

	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %v", err)
	}
	defer stmt.Close()

	// Insert data rows
	rowCount := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV row: %v", err)
		}

		// Convert row values to interface{} slice
		values := make([]interface{}, len(headers))
		for i, value := range row {
			if i >= len(values) {
				break // Skip extra columns
			}
			values[i] = convertValue(strings.TrimSpace(value), columnTypes[i])
		}

		// Handle case where row has fewer columns than headers
		for i := len(row); i < len(values); i++ {
			values[i] = nil
		}

		_, err = stmt.Exec(values...)
		if err != nil {
			return fmt.Errorf("failed to insert row %d: %v", rowCount+1, err)
		}
		rowCount++
	}

	fmt.Printf("  Created table: %s (%d rows)\n", tableName, rowCount)
	return nil
}

// Helper function to create table with specific schema
func createTableWithSchema(db *sql.DB, tableName string, columns []string, types []string) error {
	var columnDefs []string
	for i, col := range columns {
		columnDefs = append(columnDefs, fmt.Sprintf("%s %s", col, types[i]))
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)",
		tableName,
		strings.Join(columnDefs, ", "))

	_, err := db.Exec(createSQL)
	return err
}

// Helper function to infer column type from sample data
func inferColumnType(sampleRows [][]string, columnIndex int) string {
	if len(sampleRows) == 0 {
		return "TEXT"
	}

	hasInt := true
	hasFloat := true
	hasDate := true
	maxLength := 0

	for _, row := range sampleRows {
		if columnIndex >= len(row) {
			continue
		}

		value := strings.TrimSpace(row[columnIndex])
		if value == "" {
			continue // Skip empty values for type inference
		}

		if len(value) > maxLength {
			maxLength = len(value)
		}

		// Check if it's an integer
		if hasInt {
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				hasInt = false
			}
		}

		// Check if it's a float
		if hasFloat {
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				hasFloat = false
			}
		}

		// Check if it's a date (basic patterns)
		if hasDate {
			datePatterns := []string{
				"2006-01-02",
				"01/02/2006",
				"2006-01-02 15:04:05",
				"01/02/2006 15:04:05",
			}
			isDate := false
			for _, pattern := range datePatterns {
				if _, err := time.Parse(pattern, value); err == nil {
					isDate = true
					break
				}
			}
			if !isDate {
				hasDate = false
			}
		}
	}

	// Return appropriate SQL type
	if hasInt {
		return "INTEGER"
	}
	if hasFloat {
		return "REAL"
	}
	if hasDate {
		return "TEXT" // Store dates as text for simplicity
	}
	if maxLength > 255 {
		return "TEXT"
	}
	return "TEXT"
}

// Helper function to convert string values to appropriate types
func convertValue(value string, columnType string) interface{} {
	if value == "" {
		return nil
	}

	switch columnType {
	case "INTEGER":
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	case "REAL":
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}

	return value // Default to string
}
