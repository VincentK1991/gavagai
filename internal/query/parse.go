package query

import (
	"encoding/json"
	"fmt"
	"os"
)

// ParseFile reads a query IR from a JSON file on disk.
func ParseFile(path string) (*Query, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read query file: %w", err)
	}
	return Parse(data)
}

// Parse unmarshals a query IR from raw JSON bytes.
func Parse(data []byte) (*Query, error) {
	var q Query
	if err := json.Unmarshal(data, &q); err != nil {
		return nil, fmt.Errorf("parse query: %w", err)
	}
	return &q, nil
}
