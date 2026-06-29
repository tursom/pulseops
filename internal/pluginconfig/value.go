package pluginconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"pulseops/internal/pluginmodel"
)

type ValidationOptions struct {
	Overrides bool
}

func ValidateValues(schema *pluginmodel.ConfigSchema, classes map[string]pluginmodel.ConfigClass, values map[string]any, opts ValidationOptions) error {
	if schema == nil {
		if len(values) > 0 {
			return errors.New("config schema is not defined")
		}
		return nil
	}
	if values == nil {
		values = map[string]any{}
	}
	return validateFieldValues("config", schema.Fields, classes, values, opts)
}

func validateFieldValues(path string, fields map[string]pluginmodel.ConfigField, classes map[string]pluginmodel.ConfigClass, values map[string]any, opts ValidationOptions) error {
	var errs []error
	for key := range values {
		if _, ok := fields[key]; !ok {
			errs = append(errs, fmt.Errorf("%s.%s is not declared in schema", path, key))
		}
	}
	for name, field := range fields {
		value, ok := values[name]
		if !ok {
			if field.Required && !opts.Overrides {
				errs = append(errs, fmt.Errorf("%s.%s is required", path, name))
			}
			continue
		}
		if value == nil {
			if field.Required {
				errs = append(errs, fmt.Errorf("%s.%s is required", path, name))
			}
			continue
		}
		if opts.Overrides && !field.Overridable {
			errs = append(errs, fmt.Errorf("%s.%s is not overridable", path, name))
			continue
		}
		if field.Required && isEmptyValue(value) {
			errs = append(errs, fmt.Errorf("%s.%s is required", path, name))
			continue
		}
		errs = append(errs, validateFieldValue(path+"."+name, field, classes, value, opts)...)
	}
	return errors.Join(errs...)
}

func validateFieldValue(path string, field pluginmodel.ConfigField, classes map[string]pluginmodel.ConfigClass, value any, opts ValidationOptions) []error {
	switch strings.TrimSpace(field.Type) {
	case "string":
		return validateStringValue(path, field, value)
	case "number":
		return validateNumberValue(path, field, value)
	case "bool":
		if _, ok := value.(bool); !ok {
			return []error{fmt.Errorf("%s must be a bool", path)}
		}
	case "select":
		if !optionContains(field.Options, value) {
			return []error{fmt.Errorf("%s must be one of the declared options", path)}
		}
	case "multi_select":
		items, ok := configSlice(value)
		if !ok {
			return []error{fmt.Errorf("%s must be an array", path)}
		}
		var errs []error
		for i, item := range items {
			if !optionContains(field.Options, item) {
				errs = append(errs, fmt.Errorf("%s[%d] must be one of the declared options", path, i))
			}
		}
		return errs
	case "object":
		object, ok := configMap(value)
		if !ok {
			return []error{fmt.Errorf("%s must be an object", path)}
		}
		if field.Class == "JSONObject" {
			return nil
		}
		class, ok := classes[field.Class]
		if !ok {
			return []error{fmt.Errorf("%s.class %q is not defined in config_classes", path, field.Class)}
		}
		if err := validateFieldValues(path, class.Fields, classes, object, opts); err != nil {
			return []error{err}
		}
	case "array":
		if field.Items == nil {
			return []error{fmt.Errorf("%s.items is required", path)}
		}
		items, ok := configSlice(value)
		if !ok {
			return []error{fmt.Errorf("%s must be an array", path)}
		}
		var errs []error
		for i, item := range items {
			errs = append(errs, validateFieldValue(fmt.Sprintf("%s[%d]", path, i), *field.Items, classes, item, opts)...)
		}
		return errs
	case "file":
		return validateReference(path, "asset_id", value)
	case "secret":
		return validateReference(path, "secret_id", value)
	default:
		return []error{fmt.Errorf("%s.type %q is not supported", path, field.Type)}
	}
	return nil
}

func validateStringValue(path string, field pluginmodel.ConfigField, value any) []error {
	text, ok := value.(string)
	if !ok {
		return []error{fmt.Errorf("%s must be a string", path)}
	}
	var errs []error
	if field.Validation.MinLen > 0 && len(text) < field.Validation.MinLen {
		errs = append(errs, fmt.Errorf("%s length must be at least %d", path, field.Validation.MinLen))
	}
	if field.Validation.MaxLen > 0 && len(text) > field.Validation.MaxLen {
		errs = append(errs, fmt.Errorf("%s length must be at most %d", path, field.Validation.MaxLen))
	}
	if field.Validation.Pattern != "" {
		re, err := regexp.Compile(field.Validation.Pattern)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.validation.pattern is invalid: %w", path, err))
		} else if !re.MatchString(text) {
			errs = append(errs, fmt.Errorf("%s must match pattern %q", path, field.Validation.Pattern))
		}
	}
	return errs
}

func validateNumberValue(path string, field pluginmodel.ConfigField, value any) []error {
	number, ok := configNumber(value)
	if !ok {
		return []error{fmt.Errorf("%s must be a number", path)}
	}
	var errs []error
	if field.Validation.Min != nil && number < *field.Validation.Min {
		errs = append(errs, fmt.Errorf("%s must be at least %s", path, formatNumber(*field.Validation.Min)))
	}
	if field.Validation.Max != nil && number > *field.Validation.Max {
		errs = append(errs, fmt.Errorf("%s must be at most %s", path, formatNumber(*field.Validation.Max)))
	}
	if field.Validation.Step != nil && *field.Validation.Step > 0 {
		base := 0.0
		if field.Validation.Min != nil {
			base = *field.Validation.Min
		}
		steps := (number - base) / *field.Validation.Step
		if math.Abs(steps-math.Round(steps)) > 1e-9 {
			errs = append(errs, fmt.Errorf("%s must align to step %s", path, formatNumber(*field.Validation.Step)))
		}
	}
	return errs
}

func validateReference(path, key string, value any) []error {
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return []error{fmt.Errorf("%s.%s is required", path, key)}
		}
		return nil
	}
	object, ok := configMap(value)
	if !ok {
		return []error{fmt.Errorf("%s must be a %s reference", path, key)}
	}
	raw, ok := object[key]
	text, textOK := raw.(string)
	if !ok || !textOK || strings.TrimSpace(text) == "" {
		return []error{fmt.Errorf("%s.%s is required", path, key)}
	}
	if rawVersion, ok := object["version"]; ok {
		number, ok := configNumber(rawVersion)
		if !ok || number <= 0 || math.Trunc(number) != number {
			return []error{fmt.Errorf("%s.version must be a positive integer", path)}
		}
	}
	return nil
}

func optionContains(options []pluginmodel.ConfigOption, value any) bool {
	for _, option := range options {
		if valuesEqual(option.Value, value) {
			return true
		}
	}
	return false
}

func valuesEqual(left, right any) bool {
	leftNumber, leftOK := configNumber(left)
	rightNumber, rightOK := configNumber(right)
	if leftOK && rightOK {
		return math.Abs(leftNumber-rightNumber) < 1e-9
	}
	if reflect.DeepEqual(left, right) {
		return true
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func configNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func configMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out, true
	default:
		return nil, false
	}
}

func configSlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []string:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, value)
		}
		return out, true
	case []int:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, value)
		}
		return out, true
	default:
		return nil, false
	}
}

func isEmptyValue(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return value == nil
	}
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
