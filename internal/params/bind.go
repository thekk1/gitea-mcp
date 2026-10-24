package params

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Bind decodes args into out, a pointer to a struct, replacing the repeated
// args["x"].(string)/!ok extraction pattern. It round-trips args through JSON
// so JSON numbers land in the correct Go numeric field types.
//
// Struct fields tagged `required:"true"` must be present in args; a missing
// key, or an empty string value for a string field, returns an error naming
// the field's json tag.
func Bind(args map[string]any, out any) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("params.Bind: out must be a pointer to a struct, got %T", out)
	}

	if err := checkRequiredFields(args, v.Elem().Type()); err != nil {
		return err
	}

	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("params.Bind: marshal args: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			return fmt.Errorf("%s must be %s", typeErr.Field, jsonTypeName(typeErr.Type))
		}
		return fmt.Errorf("params.Bind: %w", err)
	}
	return nil
}

// jsonTypeName describes a Go target type in the JSON terms a tool caller uses.
func jsonTypeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Slice, reflect.Array:
		return "an array"
	case reflect.Struct, reflect.Map:
		return "an object"
	default:
		return "a number"
	}
}

func checkRequiredFields(args map[string]any, t reflect.Type) error {
	for field := range t.Fields() {
		if field.Tag.Get("required") != "true" {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		val, ok := args[name]
		if !ok || val == nil { // explicit null counts as missing
			return fmt.Errorf("%s is required", name)
		}
		if s, isString := val.(string); isString && s == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}
