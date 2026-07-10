package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
)

// ValidateStructuredOutput 校验 CLI 自动化常用的 JSON Schema 核心字段。
func ValidateStructuredOutput(schemaData []byte, output string) error {
	var schema any
	if err := decodeJSON(schemaData, &schema); err != nil {
		return fmt.Errorf("invalid output schema: %w", err)
	}
	var value any
	if err := decodeJSON([]byte(output), &value); err != nil {
		return fmt.Errorf("model output is not valid JSON: %w", err)
	}
	return validateSchemaValue(schema, value, "$")
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("multiple JSON values")
	} else if err != io.EOF {
		return err
	}
	return nil
}

func validateSchemaValue(rawSchema, value any, path string) error {
	if allowed, ok := rawSchema.(bool); ok {
		if allowed {
			return nil
		}
		return fmt.Errorf("%s is rejected by schema", path)
	}
	schema, ok := rawSchema.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid schema at %s: expected object or boolean", path)
	}
	if enum, ok := schema["enum"].([]any); ok && !containsJSONValue(enum, value) {
		return fmt.Errorf("%s is not one of the allowed values", path)
	}
	if rawType, ok := schema["type"]; ok && !matchesSchemaType(rawType, value) {
		return fmt.Errorf("%s has type %s, expected %v", path, jsonType(value), rawType)
	}

	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, rawName := range required {
				name, ok := rawName.(string)
				if !ok {
					return fmt.Errorf("invalid required entry at %s", path)
				}
				if _, exists := object[name]; !exists {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		}
		for name, child := range object {
			if childSchema, exists := properties[name]; exists {
				if err := validateSchemaValue(childSchema, child, path+"."+name); err != nil {
					return err
				}
				continue
			}
			switch additional := schema["additionalProperties"].(type) {
			case bool:
				if !additional {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
			case map[string]any:
				if err := validateSchemaValue(additional, child, path+"."+name); err != nil {
					return err
				}
			}
		}
	}

	if array, ok := value.([]any); ok {
		if itemSchema, exists := schema["items"]; exists {
			for index, item := range array {
				if err := validateSchemaValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func matchesSchemaType(rawType, value any) bool {
	switch types := rawType.(type) {
	case string:
		return matchesType(types, value)
	case []any:
		for _, item := range types {
			if name, ok := item.(string); ok && matchesType(name, value) {
				return true
			}
		}
	}
	return false
}

func matchesType(name string, value any) bool {
	switch name {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		parsed, err := number.Float64()
		return err == nil && parsed == math.Trunc(parsed)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func jsonType(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

func containsJSONValue(items []any, value any) bool {
	want, err := json.Marshal(value)
	if err != nil {
		return false
	}
	for _, item := range items {
		got, err := json.Marshal(item)
		if err == nil && bytes.Equal(got, want) {
			return true
		}
	}
	return false
}

func structuredOutputInstruction(schemaData []byte) string {
	return "Return only one JSON value that conforms to this JSON Schema. Do not wrap it in Markdown fences.\n\n" + strings.TrimSpace(string(schemaData))
}
