package llm

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/generative-ai-go/genai"
)

// BuildSchema dynamically constructs a *genai.Schema from any Go struct
// using reflection. It reads `json` tags for field names and `description`
// tags for schema descriptions.
//
// Supported types:
//   - Primitives: string, bool, int*, uint*, float*
//   - Pointers: treated as optional (not added to required list)
//   - Slices/arrays: mapped to genai.TypeArray
//   - Nested structs: recursed into genai.TypeObject
//   - Maps with string keys: mapped to genai.TypeObject
//
// Usage:
//
//	schema, err := llm.BuildSchema(models.PipelineLLMOutput{})
//	schema, err := llm.BuildSchema(&models.PipelineLLMOutput{})
//
// Tag examples:
//
//	type Fee struct {
//	    Amount   float64 `json:"amount"              description:"Fee amount in local currency"`
//	    Currency string  `json:"currency"             description:"ISO currency code"`
//	    Notes    *string `json:"notes,omitempty"       description:"Optional free-text notes"`
//	}
func BuildSchema(obj interface{}) (*genai.Schema, error) {
	t := reflect.TypeOf(obj)

	// Dereference pointer to get underlying type
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("BuildSchema expects a struct, got %s", t.Kind())
	}

	return buildSchemaFromType(t)
}

func buildSchemaFromType(t reflect.Type) (*genai.Schema, error) {
	// Dereference pointers
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return &genai.Schema{Type: genai.TypeString}, nil

	case reflect.Bool:
		return &genai.Schema{Type: genai.TypeBoolean}, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &genai.Schema{Type: genai.TypeInteger}, nil

	case reflect.Float32, reflect.Float64:
		return &genai.Schema{Type: genai.TypeNumber}, nil

	case reflect.Slice, reflect.Array:
		itemSchema, err := buildSchemaFromType(t.Elem())
		if err != nil {
			return nil, fmt.Errorf("array element: %w", err)
		}
		return &genai.Schema{
			Type:  genai.TypeArray,
			Items: itemSchema,
		}, nil

	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("only map[string]T is supported, got map[%s]T", t.Key().Kind())
		}
		valueSchema, err := buildSchemaFromType(t.Elem())
		if err != nil {
			return nil, fmt.Errorf("map value: %w", err)
		}
		// Gemini doesn't have a native map type — model as object with dynamic values
		return &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"*": valueSchema,
			},
		}, nil

	case reflect.Struct:
		return buildStructSchema(t)

	default:
		return nil, fmt.Errorf("unsupported type: %s", t.Kind())
	}
}

func buildStructSchema(t reflect.Type) (*genai.Schema, error) {
	properties := make(map[string]*genai.Schema)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Parse json tag for field name and omitempty
		jsonName, omit, skip := parseJSONTag(field)
		if skip {
			continue
		}

		// Build schema for this field's type
		fieldSchema, err := buildSchemaFromType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", field.Name, err)
		}

		// Apply description tag
		if desc, ok := field.Tag.Lookup("description"); ok {
			fieldSchema.Description = desc
		}

		// Apply enum tag: enum:"ILS,USD,EUR"
		if enumTag, ok := field.Tag.Lookup("enum"); ok {
			for _, v := range strings.Split(enumTag, ",") {
				fieldSchema.Enum = append(fieldSchema.Enum, strings.TrimSpace(v))
			}
		}

		properties[jsonName] = fieldSchema

		// A field is required if:
		//   - It's not a pointer type (pointers = optional)
		//   - It doesn't have omitempty in its json tag
		//   - It doesn't have a `required:"false"` tag
		isPointer := field.Type.Kind() == reflect.Ptr
		explicitRequired, hasRequiredTag := field.Tag.Lookup("required")

		if hasRequiredTag {
			if explicitRequired == "true" {
				required = append(required, jsonName)
			}
			// required:"false" → skip
		} else if !isPointer && !omit {
			required = append(required, jsonName)
		}
	}

	schema := &genai.Schema{
		Type:       genai.TypeObject,
		Properties: properties,
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema, nil
}

// parseJSONTag extracts the field name, omitempty flag, and skip flag from a json struct tag.
func parseJSONTag(field reflect.StructField) (name string, omitempty bool, skip bool) {
	tag := field.Tag.Get("json")

	if tag == "-" {
		return "", false, true
	}

	if tag == "" {
		return field.Name, false, false
	}

	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = field.Name
	}

	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			omitempty = true
		}
	}

	return name, omitempty, false
}
