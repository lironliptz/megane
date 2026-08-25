package models

// FieldType enumerates the supported JSON-schema primitive types.
type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeNumber  FieldType = "number"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeArray   FieldType = "array"
	FieldTypeObject  FieldType = "object"
)

// Field describes a single attribute in a DataModel.
type Field struct {
	Name        string
	Type        FieldType
	Description string
	Required    bool
	Items       *Field  // for FieldTypeArray: schema of each element
	Fields      []Field // for FieldTypeObject: sub-fields
}

// DataModel is the top-level descriptor for an LLM output structure.
type DataModel struct {
	Name   string
	Fields []Field
}
