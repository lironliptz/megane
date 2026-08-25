package models

import "encoding/json"

// BuildJSONSchema converts a DataModel into a JSON Schema object (as a Go map).
// The returned map is compatible with Gemini's and OpenAI's schema formats.
func BuildJSONSchema(model DataModel) map[string]interface{} {
	schema := buildObjectSchema(model.Fields)
	schema["title"] = model.Name
	return schema
}

// MarshalSchema serialises the JSON Schema for a DataModel to JSON bytes.
func MarshalSchema(model DataModel) ([]byte, error) {
	return json.MarshalIndent(BuildJSONSchema(model), "", "  ")
}

// buildFieldSchema converts a single Field to its JSON Schema representation.
func buildFieldSchema(f Field) map[string]interface{} {
	schema := map[string]interface{}{
		"type": string(f.Type),
	}
	if f.Description != "" {
		schema["description"] = f.Description
	}
	switch f.Type {
	case FieldTypeArray:
		if f.Items != nil {
			schema["items"] = buildFieldSchema(*f.Items)
		}
	case FieldTypeObject:
		if len(f.Fields) > 0 {
			props, required := buildProperties(f.Fields)
			schema["properties"] = props
			if len(required) > 0 {
				schema["required"] = required
			}
		}
	}
	return schema
}

// buildObjectSchema creates the top-level "object" schema for a slice of Fields.
func buildObjectSchema(fields []Field) map[string]interface{} {
	props, required := buildProperties(fields)
	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func buildProperties(fields []Field) (map[string]interface{}, []string) {
	props := make(map[string]interface{}, len(fields))
	var required []string
	for _, f := range fields {
		props[f.Name] = buildFieldSchema(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	return props, required
}
