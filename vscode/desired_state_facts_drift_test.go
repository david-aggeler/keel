package vscode

import (
	"reflect"
	"strings"
	"testing"
)

// TestSchemaDriftGuardCoversDesiredStateFactFields proves the acceptance
// criterion directly: rename or retype a desired-state fact field in the Go
// wire struct without the matching schema edit and the drift guard fails.
//
// The guard's comparison is exercised against synthetic mutations of the real
// wire types, so the assertion is about the guard's reach, not about the
// current shape of the schema.
//
// DHF-TEST: keel/requirement-127
func TestSchemaDriftGuardCoversDesiredStateFactFields(t *testing.T) {
	discovery := loadSchemas(t)["discovery"]

	for _, tc := range []struct {
		name string
		ref  string
		typ  reflect.Type
	}{
		{"desired_state_group", desiredStateGroupFactsRef, reflect.TypeOf(DesiredStateGroupFacts{})},
		{"desired_state_row", desiredStateRowFactsRef, reflect.TypeOf(DesiredStateRowFacts{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := schemaAtRef(discovery, tc.ref)
			if len(schema.Properties) == 0 {
				t.Fatalf("discovery schema declares no properties at %s: the guard cannot see the fact fields", tc.ref)
			}
			if err := compareSchemaToType(schema, tc.typ); err != nil {
				t.Fatalf("schema and Go type already drift: %v", err)
			}

			for i := 0; i < tc.typ.NumField(); i++ {
				renamed := mutateField(tc.typ, i, renameJSONField)
				if err := compareSchemaToType(schema, renamed); err == nil {
					t.Fatalf("guard did not catch field %s renamed in Go without a schema edit", tc.typ.Field(i).Name)
				}
				retyped := mutateField(tc.typ, i, retypeField)
				if err := compareSchemaToType(schema, retyped); err == nil {
					t.Fatalf("guard did not catch field %s retyped in Go without a schema edit", tc.typ.Field(i).Name)
				}
			}
		})
	}
}

// mutateField rebuilds typ with one field passed through mutate, so the guard
// can be pointed at a wire struct that has drifted from the schema.
func mutateField(typ reflect.Type, index int, mutate func(reflect.StructField) reflect.StructField) reflect.Type {
	fields := make([]reflect.StructField, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		field.Index = nil
		if i == index {
			field = mutate(field)
		}
		fields = append(fields, field)
	}
	return reflect.StructOf(fields)
}

func renameJSONField(field reflect.StructField) reflect.StructField {
	tag := string(field.Tag.Get("json"))
	name, opts, hasOpts := strings.Cut(tag, ",")
	renamed := name + "_renamed"
	if hasOpts {
		renamed += "," + opts
	}
	field.Tag = reflect.StructTag(`json:"` + renamed + `"`)
	return field
}

// retypeField swaps the field's Go type for one that serializes as a different
// JSON type — the exact mistake that keeps a property name in place while
// silently changing what travels under it.
func retypeField(field reflect.StructField) reflect.StructField {
	if field.Type.Kind() == reflect.Bool {
		field.Type = reflect.TypeOf("")
	} else {
		field.Type = reflect.TypeOf(false)
	}
	return field
}
