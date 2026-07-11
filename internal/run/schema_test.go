package run

import (
	"strings"
	"testing"
)

func TestValidateStructuredOutput(t *testing.T) {
	schema := []byte(`{
		"type":"object",
		"properties":{"name":{"type":"string"},"scores":{"type":"array","items":{"type":"integer"}}},
		"required":["name","scores"],
		"additionalProperties":false
	}`)
	if err := ValidateStructuredOutput(schema, `{"name":"codeworld","scores":[1,2]}`); err != nil {
		t.Fatalf("ValidateStructuredOutput: %v", err)
	}
	if err := ValidateStructuredOutput(schema, `{"name":"codeworld","scores":[1.5]}`); err == nil || !strings.Contains(err.Error(), "expected integer") {
		t.Fatalf("err = %v, want integer error", err)
	}
	if err := ValidateStructuredOutput(schema, "not-json"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("err = %v, want JSON error", err)
	}
}
