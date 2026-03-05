package rediver

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ParamType defines the type of a parameter.
type ParamType string

const (
	ParamTypeString  ParamType = "string"
	ParamTypeNumber  ParamType = "number"
	ParamTypeBool    ParamType = "boolean"
	ParamTypeArray   ParamType = "array"
)

// Param defines a scanner parameter.
type Param struct {
	name        string
	paramType   ParamType
	arrayType   ParamType
	label       string
	description string
	required    bool
	enumValues  []string
	defaultVal  any
	envVar      string // env var name to resolve in CI mode
}

// ParamBuilder provides a fluent API for building parameters.
type ParamBuilder struct {
	param Param
}

// StringParam creates a string parameter builder.
func StringParam(name string) *ParamBuilder {
	return &ParamBuilder{
		param: Param{name: name, paramType: ParamTypeString},
	}
}

// IntParam creates an integer parameter builder.
func IntParam(name string) *ParamBuilder {
	return &ParamBuilder{
		param: Param{name: name, paramType: ParamTypeNumber},
	}
}

// BoolParam creates a boolean parameter builder.
func BoolParam(name string) *ParamBuilder {
	return &ParamBuilder{
		param: Param{name: name, paramType: ParamTypeBool},
	}
}

// StringArrayParam creates a string array parameter builder.
func StringArrayParam(name string) *ParamBuilder {
	return &ParamBuilder{
		param: Param{name: name, paramType: ParamTypeArray, arrayType: ParamTypeString},
	}
}

// IntArrayParam creates an integer array parameter builder.
func IntArrayParam(name string) *ParamBuilder {
	return &ParamBuilder{
		param: Param{name: name, paramType: ParamTypeArray, arrayType: ParamTypeNumber},
	}
}

// Label sets the display label.
func (b *ParamBuilder) Label(label string) *ParamBuilder {
	b.param.label = label
	return b
}

// Description sets the parameter description.
func (b *ParamBuilder) Description(desc string) *ParamBuilder {
	b.param.description = desc
	return b
}

// Required marks the parameter as required.
func (b *ParamBuilder) Required() *ParamBuilder {
	b.param.required = true
	return b
}

// Enum sets allowed values for the parameter.
func (b *ParamBuilder) Enum(values ...string) *ParamBuilder {
	b.param.enumValues = values
	return b
}

// Default sets the default value.
func (b *ParamBuilder) Default(val any) *ParamBuilder {
	b.param.defaultVal = val
	return b
}

// Env sets the environment variable name used to resolve this parameter in CI mode.
// In CI mode, if the env var is set, its value takes priority over defaults.
func (b *ParamBuilder) Env(envVar string) *ParamBuilder {
	b.param.envVar = envVar
	return b
}

// Build returns the configured Param.
func (b *ParamBuilder) Build() Param {
	return b.param
}

// Name returns the parameter name.
func (b *ParamBuilder) Name() string {
	return b.param.name
}

// ParamValue provides type-safe access to parameter values.
type ParamValue interface {
	// String returns the value as a string.
	String() string
	// StringOr returns the value as a string or the fallback if not set.
	StringOr(fallback string) string

	// Int returns the value as an integer.
	Int() int
	// IntOr returns the value as an integer or the fallback if not set.
	IntOr(fallback int) int

	// Bool returns the value as a boolean.
	Bool() bool
	// BoolOr returns the value as a boolean or the fallback if not set.
	BoolOr(fallback bool) bool

	// Strings returns the value as a string slice.
	Strings() []string
	// StringsOr returns the value as a string slice or the fallback if not set.
	StringsOr(fallback []string) []string

	// Ints returns the value as an integer slice.
	Ints() []int
	// IntsOr returns the value as an integer slice or the fallback if not set.
	IntsOr(fallback []int) []int

	// IsSet returns true if the parameter was provided.
	IsSet() bool
}

// paramValue implements ParamValue.
type paramValue struct {
	value any
	set   bool
}

func (p *paramValue) IsSet() bool {
	return p.set
}

func (p *paramValue) String() string {
	return p.StringOr("")
}

func (p *paramValue) StringOr(fallback string) string {
	if !p.set || p.value == nil {
		return fallback
	}
	return toString(p.value, fallback)
}

func (p *paramValue) Int() int {
	return p.IntOr(0)
}

func (p *paramValue) IntOr(fallback int) int {
	if !p.set || p.value == nil {
		return fallback
	}
	return toInt(p.value, fallback)
}

func (p *paramValue) Bool() bool {
	return p.BoolOr(false)
}

func (p *paramValue) BoolOr(fallback bool) bool {
	if !p.set || p.value == nil {
		return fallback
	}
	return toBool(p.value, fallback)
}

func (p *paramValue) Strings() []string {
	return p.StringsOr(nil)
}

func (p *paramValue) StringsOr(fallback []string) []string {
	if !p.set || p.value == nil {
		return fallback
	}
	return toStrings(p.value, fallback)
}

func (p *paramValue) Ints() []int {
	return p.IntsOr(nil)
}

func (p *paramValue) IntsOr(fallback []int) []int {
	if !p.set || p.value == nil {
		return fallback
	}
	return toInts(p.value, fallback)
}

// Helper functions for type conversion

func toString(v any, fallback string) string {
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	case int, int8, int16, int32, int64:
		return strconv.FormatInt(toInt64(val), 10)
	case float32, float64:
		return strconv.FormatFloat(toFloat64(val), 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case json.Number:
		return val.String()
	default:
		return fallback
	}
}

func toInt(v any, fallback int) int {
	switch val := v.(type) {
	case int:
		return val
	case int8:
		return int(val)
	case int16:
		return int(val)
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float32:
		return int(val)
	case float64:
		return int(val)
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return int(i)
		}
	}
	return fallback
}

func toBool(v any, fallback bool) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	case int, int8, int16, int32, int64:
		return toInt64(val) != 0
	case float32, float64:
		return toFloat64(val) != 0
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i != 0
		}
	}
	return fallback
}

func toStrings(v any, fallback []string) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if item != nil {
				result = append(result, toString(item, ""))
			}
		}
		return result
	case string:
		// Handle comma-separated strings
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			if s := strings.TrimSpace(part); s != "" {
				result = append(result, s)
			}
		}
		return result
	}
	return fallback
}

func toInts(v any, fallback []int) []int {
	switch val := v.(type) {
	case []int:
		return val
	case []any:
		result := make([]int, 0, len(val))
		for _, item := range val {
			result = append(result, toInt(item, 0))
		}
		return result
	case string:
		// Handle comma-separated integers
		parts := strings.Split(val, ",")
		result := make([]int, 0, len(parts))
		for _, part := range parts {
			if i, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				result = append(result, i)
			}
		}
		return result
	}
	return fallback
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int8:
		return int64(val)
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	default:
		return 0
	}
}

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float32:
		return float64(val)
	case float64:
		return val
	default:
		return 0
	}
}

// ParamsToJSONSchema converts param definitions to a JSON Schema draft-07 object.
func ParamsToJSONSchema(params []Param) map[string]interface{} {
	if len(params) == 0 {
		return nil
	}
	properties := make(map[string]interface{})
	var required []string

	for _, p := range params {
		prop := map[string]interface{}{"type": string(p.paramType)}
		if p.label != "" {
			prop["title"] = p.label
		}
		if p.description != "" {
			prop["description"] = p.description
		}
		if p.defaultVal != nil {
			prop["default"] = p.defaultVal
		}
		if len(p.enumValues) > 0 {
			prop["enum"] = p.enumValues
		}
		if p.paramType == ParamTypeArray && p.arrayType != "" {
			prop["items"] = map[string]interface{}{"type": string(p.arrayType)}
		}
		properties[p.name] = prop
		if p.required {
			required = append(required, p.name)
		}
	}

	schema := map[string]interface{}{
		"type":                 "object",
		"properties":          properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
