package model

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// ParseFile reads an OSI semantic-model document from disk and unmarshals it.
// The format is selected by file extension: .json uses the JSON decoder,
// .yaml/.yml use the YAML decoder. ParseFile reports IO and syntax errors
// only; semantic checks are performed separately by Validate.
func ParseFile(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model file: %w", err)
	}
	return Parse(data, path)
}

// Parse unmarshals an OSI document from raw bytes. The name argument supplies
// the file extension used to pick the decoder and to label errors.
func Parse(data []byte, name string) (*Document, error) {
	var doc Document

	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".json":
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
	case ".yaml", ".yml", "":
		// sigs.k8s.io/yaml converts YAML to JSON, so the json struct tags
		// apply and JSON input is also accepted by this path.
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
	default:
		return nil, fmt.Errorf("parse %s: unsupported file extension %q (want .yaml, .yml, or .json)", name, ext)
	}

	return &doc, nil
}
