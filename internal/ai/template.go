package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"text/template"
)

func renderTemplate(prompt PromptSpec, data map[string]any) (string, error) {
	tmplText := prompt.Text
	if tmplText == "" {
		return "", fmt.Errorf("prompt text is empty")
	}
	funcMap := template.FuncMap{
		"json":     jsonFunc,
		"table":    tableFunc,
		"len":      lenFunc,
		"avg":      avgFunc,
		"count":    countFunc,
		"filter":   filterFunc,
		"failures": filterFailuresFunc,
	}
	tmpl := template.New("prompt").Funcs(funcMap)
	tmpl, err := tmpl.Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{
		"DataSources": data,
	}); err != nil {
		return "", fmt.Errorf("execute prompt template: %w", err)
	}
	return buf.String(), nil
}

func jsonFunc(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(raw)
}

func lenFunc(v any) int {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
		return rv.Len()
	}
	return 0
}

func countFunc(v any) int {
	return lenFunc(v)
}

func filterFunc(items any, field string, value string) any {
	rv := reflect.ValueOf(items)
	if rv.Kind() != reflect.Slice {
		return items
	}
	var result []any
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct && item.Kind() != reflect.Map {
			continue
		}
		var fieldVal reflect.Value
		if item.Kind() == reflect.Struct {
			fieldVal = item.FieldByName(field)
		} else {
			fieldVal = item.MapIndex(reflect.ValueOf(field))
		}
		if fieldVal.IsValid() && fmt.Sprintf("%v", fieldVal.Interface()) == value {
			result = append(result, item.Interface())
		}
	}
	return result
}

func filterFailuresFunc(items any) any {
	rv := reflect.ValueOf(items)
	if rv.Kind() != reflect.Slice {
		return items
	}
	var result []any
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			continue
		}
		runStatus := item.FieldByName("RunStatus")
		checkStatus := item.FieldByName("CheckStatus")
		if (runStatus.IsValid() && runStatus.String() == "failed") ||
			(checkStatus.IsValid() && checkStatus.String() == "fail") {
			result = append(result, item.Interface())
		}
	}
	return result
}

func avgFunc(items any, field string) float64 {
	rv := reflect.ValueOf(items)
	if rv.Kind() != reflect.Slice || rv.Len() == 0 {
		return 0
	}
	var sum float64
	var count int
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			continue
		}
		fieldVal := item.FieldByName(field)
		if !fieldVal.IsValid() {
			continue
		}
		switch fieldVal.Kind() {
		case reflect.Int, reflect.Int64, reflect.Int32:
			sum += float64(fieldVal.Int())
			count++
		case reflect.Float64, reflect.Float32:
			sum += fieldVal.Float()
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func tableFunc(items any, fields ...string) string {
	rv := reflect.ValueOf(items)
	if rv.Kind() != reflect.Slice || rv.Len() == 0 {
		return ""
	}
	if len(fields) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("| ")
	for _, f := range fields {
		sb.WriteString(f)
		sb.WriteString(" | ")
	}
	sb.WriteString("\n|")
	for range fields {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			continue
		}
		sb.WriteString("| ")
		for _, field := range fields {
			fieldVal := item.FieldByName(field)
			if fieldVal.IsValid() {
				sb.WriteString(fmt.Sprintf("%v", fieldVal.Interface()))
			}
			sb.WriteString(" | ")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
