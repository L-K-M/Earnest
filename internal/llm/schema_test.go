package llm

import (
	"encoding/json"
	"errors"
	"testing"
)

const testSchema = `{
	"type": "object",
	"properties": {
		"label": {"type": "string", "enum": ["press_release", "analysis"]},
		"score": {"type": "number", "minimum": 0, "maximum": 1}
	},
	"required": ["label", "score"],
	"additionalProperties": false
}`

func TestSchemaValidate(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{name: "valid", output: `{"label":"press_release","score":0.5}`},
		{name: "valid second enum", output: `{"score":1,"label":"analysis"}`},
		{name: "empty", output: ``, wantErr: true},
		{name: "not json", output: `the label is press_release`, wantErr: true},
		{name: "trailing garbage", output: `{"label":"press_release","score":0.5} extra`, wantErr: true},
		{name: "array", output: `[]`, wantErr: true},
		{name: "missing required", output: `{"label":"press_release"}`, wantErr: true},
		{name: "extra property", output: `{"label":"press_release","score":0.5,"extra":1}`, wantErr: true},
		{name: "wrong type", output: `{"label":7,"score":0.5}`, wantErr: true},
		{name: "enum", output: `{"label":"other","score":0.5}`, wantErr: true},
		{name: "range", output: `{"label":"press_release","score":2}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := NewSchema("label", json.RawMessage(testSchema))
			if err != nil {
				t.Fatal(err)
			}
			err = schema.Validate(test.output)
			if test.wantErr && !errors.Is(err, ErrInvalidOutput) {
				t.Fatalf("error = %v, want ErrInvalidOutput", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNewSchemaRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name       string
		definition string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"not json", "{"},
		{"invalid schema", `{"type": 7}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSchema("x", json.RawMessage(test.definition)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSchemaDefaultName(t *testing.T) {
	schema, err := NewSchema("", json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	if schema.Name != "response" {
		t.Errorf("name = %q, want response", schema.Name)
	}
}
