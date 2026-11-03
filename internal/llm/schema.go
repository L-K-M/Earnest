package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaURL is the in-memory resource name the compiler resolves the schema
// document under. Schemas are self-contained; no external references load.
const schemaURL = "earnest://schema.json"

// Schema is a JSON Schema for structured model output, compiled once so
// validation does not re-parse the document on every call.
type Schema struct {
	// Name identifies the schema to providers that require one.
	Name string
	// Definition is the JSON Schema document sent to the provider.
	Definition json.RawMessage

	compiled *jsonschema.Schema
}

// NewSchema compiles a JSON Schema. A schema that cannot be parsed or
// compiled is an error, never a runtime surprise.
func NewSchema(name string, definition json.RawMessage) (*Schema, error) {
	if len(bytes.TrimSpace(definition)) == 0 {
		return nil, fmt.Errorf("llm: empty JSON schema")
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(definition))
	if err != nil {
		return nil, fmt.Errorf("llm: parse JSON schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, fmt.Errorf("llm: register JSON schema: %w", err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("llm: compile JSON schema: %w", err)
	}
	if strings.TrimSpace(name) == "" {
		name = "response"
	}
	return &Schema{Name: name, Definition: definition, compiled: compiled}, nil
}

// Validate reports whether text is one JSON document matching the schema.
// Output that is not valid JSON, or does not conform, is ErrInvalidOutput.
func (s *Schema) Validate(text string) error {
	if s == nil || s.compiled == nil {
		return fmt.Errorf("%w: no schema", ErrInvalidOutput)
	}
	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(text))
	if err != nil {
		return fmt.Errorf("%w: parse output: %v", ErrInvalidOutput, err)
	}
	if err := s.compiled.Validate(instance); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidOutput, err)
	}
	return nil
}
