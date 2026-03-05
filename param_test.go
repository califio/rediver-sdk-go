package rediver

import (
	"encoding/json"
	"testing"
)

// --- ParamBuilder tests ---

func TestStringParam(t *testing.T) {
	p := StringParam("target").Build()
	if p.name != "target" {
		t.Errorf("name: got %q, want target", p.name)
	}
	if p.paramType != ParamTypeString {
		t.Errorf("type: got %q, want string", p.paramType)
	}
}

func TestIntParam(t *testing.T) {
	p := IntParam("port").Build()
	if p.paramType != ParamTypeNumber {
		t.Errorf("type: got %q, want number", p.paramType)
	}
}

func TestBoolParam(t *testing.T) {
	p := BoolParam("verbose").Build()
	if p.paramType != ParamTypeBool {
		t.Errorf("type: got %q, want boolean", p.paramType)
	}
}

func TestStringArrayParam(t *testing.T) {
	p := StringArrayParam("targets").Build()
	if p.paramType != ParamTypeArray {
		t.Errorf("type: got %q, want array", p.paramType)
	}
	if p.arrayType != ParamTypeString {
		t.Errorf("arrayType: got %q, want string", p.arrayType)
	}
}

func TestIntArrayParam(t *testing.T) {
	p := IntArrayParam("ports").Build()
	if p.paramType != ParamTypeArray {
		t.Errorf("type: got %q, want array", p.paramType)
	}
	if p.arrayType != ParamTypeNumber {
		t.Errorf("arrayType: got %q, want number", p.arrayType)
	}
}

func TestParamBuilder_Chain(t *testing.T) {
	p := StringParam("mode").
		Label("Scan Mode").
		Description("The scanning mode").
		Required().
		Enum("fast", "deep").
		Default("fast").
		Env("SCAN_MODE").
		Build()

	if p.label != "Scan Mode" {
		t.Errorf("label: got %q", p.label)
	}
	if p.description != "The scanning mode" {
		t.Errorf("description: got %q", p.description)
	}
	if !p.required {
		t.Error("expected required=true")
	}
	if len(p.enumValues) != 2 || p.enumValues[0] != "fast" || p.enumValues[1] != "deep" {
		t.Errorf("enum: got %v", p.enumValues)
	}
	if p.defaultVal != "fast" {
		t.Errorf("default: got %v", p.defaultVal)
	}
	if p.envVar != "SCAN_MODE" {
		t.Errorf("envVar: got %q", p.envVar)
	}
}

func TestParamBuilder_Name(t *testing.T) {
	b := StringParam("test")
	if b.Name() != "test" {
		t.Errorf("Name(): got %q, want test", b.Name())
	}
}

// --- ParamValue tests ---

func TestParamValue_String(t *testing.T) {
	pv := &paramValue{value: "hello", set: true}
	if pv.String() != "hello" {
		t.Errorf("String(): got %q", pv.String())
	}
	if pv.StringOr("x") != "hello" {
		t.Errorf("StringOr(): got %q", pv.StringOr("x"))
	}
}

func TestParamValue_String_Unset(t *testing.T) {
	pv := &paramValue{set: false}
	if pv.String() != "" {
		t.Errorf("String() unset: got %q", pv.String())
	}
	if pv.StringOr("x") != "x" {
		t.Errorf("StringOr() unset: got %q", pv.StringOr("x"))
	}
}

func TestParamValue_String_Nil(t *testing.T) {
	pv := &paramValue{value: nil, set: true}
	if pv.StringOr("x") != "x" {
		t.Errorf("StringOr() nil: got %q", pv.StringOr("x"))
	}
}

func TestParamValue_Int(t *testing.T) {
	pv := &paramValue{value: 42, set: true}
	if pv.Int() != 42 {
		t.Errorf("Int(): got %d", pv.Int())
	}
}

func TestParamValue_Int_FromFloat(t *testing.T) {
	pv := &paramValue{value: 3.7, set: true}
	if pv.Int() != 3 {
		t.Errorf("Int() from float: got %d, want 3", pv.Int())
	}
}

func TestParamValue_Int_FromString(t *testing.T) {
	pv := &paramValue{value: "99", set: true}
	if pv.Int() != 99 {
		t.Errorf("Int() from string: got %d", pv.Int())
	}
}

func TestParamValue_Int_FromJsonNumber(t *testing.T) {
	pv := &paramValue{value: json.Number("123"), set: true}
	if pv.Int() != 123 {
		t.Errorf("Int() from json.Number: got %d", pv.Int())
	}
}

func TestParamValue_Int_Unset(t *testing.T) {
	pv := &paramValue{set: false}
	if pv.IntOr(10) != 10 {
		t.Errorf("IntOr() unset: got %d", pv.IntOr(10))
	}
}

func TestParamValue_Bool(t *testing.T) {
	pv := &paramValue{value: true, set: true}
	if !pv.Bool() {
		t.Error("Bool(): got false, want true")
	}
	pv = &paramValue{value: false, set: true}
	if pv.Bool() {
		t.Error("Bool(): got true, want false")
	}
}

func TestParamValue_Bool_FromString(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"true", true}, {"false", false}, {"1", true}, {"0", false},
	}
	for _, tc := range cases {
		pv := &paramValue{value: tc.input, set: true}
		if pv.Bool() != tc.want {
			t.Errorf("Bool(%q): got %v, want %v", tc.input, pv.Bool(), tc.want)
		}
	}
}

func TestParamValue_Bool_FromInt(t *testing.T) {
	// 0 = false
	pv := &paramValue{value: 0, set: true}
	if pv.Bool() {
		t.Error("Bool(0) should be false")
	}
	// 1 = true
	pv = &paramValue{value: 1, set: true}
	if !pv.Bool() {
		t.Error("Bool(1) should be true")
	}
	// -1 = true (non-zero)
	pv = &paramValue{value: -1, set: true}
	if !pv.Bool() {
		t.Error("Bool(-1) should be true")
	}
}

func TestParamValue_Bool_Unset(t *testing.T) {
	pv := &paramValue{set: false}
	if !pv.BoolOr(true) {
		t.Error("BoolOr(true) unset: got false")
	}
}

func TestParamValue_Strings(t *testing.T) {
	pv := &paramValue{value: []string{"a", "b"}, set: true}
	got := pv.Strings()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Strings(): got %v", got)
	}
}

func TestParamValue_Strings_FromAnySlice(t *testing.T) {
	pv := &paramValue{value: []any{"a", "b"}, set: true}
	got := pv.Strings()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Strings() from []any: got %v", got)
	}
}

func TestParamValue_Strings_FromCSV(t *testing.T) {
	pv := &paramValue{value: "a, b, c", set: true}
	got := pv.Strings()
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("Strings() from CSV: got %v", got)
	}
}

func TestParamValue_Strings_Unset(t *testing.T) {
	pv := &paramValue{set: false}
	got := pv.StringsOr([]string{"x"})
	if len(got) != 1 || got[0] != "x" {
		t.Errorf("StringsOr() unset: got %v", got)
	}
}

func TestParamValue_Ints(t *testing.T) {
	pv := &paramValue{value: []int{1, 2}, set: true}
	got := pv.Ints()
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("Ints(): got %v", got)
	}
}

func TestParamValue_Ints_FromAnySlice(t *testing.T) {
	pv := &paramValue{value: []any{1, 2.0, "3"}, set: true}
	got := pv.Ints()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("Ints() from []any: got %v", got)
	}
}

func TestParamValue_Ints_FromCSV(t *testing.T) {
	pv := &paramValue{value: "1,2,3", set: true}
	got := pv.Ints()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("Ints() from CSV: got %v", got)
	}
}

func TestParamValue_Ints_Unset(t *testing.T) {
	pv := &paramValue{set: false}
	got := pv.IntsOr([]int{42})
	if len(got) != 1 || got[0] != 42 {
		t.Errorf("IntsOr() unset: got %v", got)
	}
}

func TestParamValue_Bool_FromFloat(t *testing.T) {
	pv := &paramValue{value: 0.0, set: true}
	if pv.Bool() {
		t.Error("Bool(0.0) should be false")
	}
	pv = &paramValue{value: 1.5, set: true}
	if !pv.Bool() {
		t.Error("Bool(1.5) should be true")
	}
}

func TestParamValue_Bool_FromJsonNumber(t *testing.T) {
	pv := &paramValue{value: json.Number("1"), set: true}
	if !pv.Bool() {
		t.Error("Bool(json.Number 1) should be true")
	}
	pv = &paramValue{value: json.Number("0"), set: true}
	if pv.Bool() {
		t.Error("Bool(json.Number 0) should be false")
	}
}

func TestParamValue_Int_InvalidString(t *testing.T) {
	pv := &paramValue{value: "not-a-number", set: true}
	if pv.IntOr(77) != 77 {
		t.Errorf("IntOr() invalid string: got %d, want 77", pv.IntOr(77))
	}
}

func TestParamValue_Strings_FromAnySlice_WithNil(t *testing.T) {
	pv := &paramValue{value: []any{"a", nil, "b"}, set: true}
	got := pv.Strings()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Strings() with nil: got %v, want [a b]", got)
	}
}

func TestParamValue_IsSet(t *testing.T) {
	if (&paramValue{set: true}).IsSet() != true {
		t.Error("IsSet should be true")
	}
	if (&paramValue{set: false}).IsSet() != false {
		t.Error("IsSet should be false")
	}
}

func TestToString_AllTypes(t *testing.T) {
	cases := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{[]byte("bytes"), "bytes"},
		{42, "42"},
		{3.14, "3.14"},
		{true, "true"},
		{json.Number("99"), "99"},
	}
	for _, tc := range cases {
		got := toString(tc.input, "fallback")
		if got != tc.want {
			t.Errorf("toString(%v): got %q, want %q", tc.input, got, tc.want)
		}
	}

	// Unknown type returns fallback
	type custom struct{}
	if got := toString(custom{}, "fb"); got != "fb" {
		t.Errorf("toString(custom): got %q, want fb", got)
	}
}

// --- ParamsToJSONSchema tests ---

func TestParamsToJSONSchema_Empty(t *testing.T) {
	if got := ParamsToJSONSchema(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestParamsToJSONSchema_Full(t *testing.T) {
	params := []Param{
		StringParam("mode").Label("Mode").Description("Scan mode").Required().Enum("fast", "deep").Default("fast").Build(),
		IntParam("port").Build(),
	}

	schema := ParamsToJSONSchema(params)
	if schema["type"] != "object" {
		t.Errorf("schema type: got %v", schema["type"])
	}

	props := schema["properties"].(map[string]interface{})
	modeProp := props["mode"].(map[string]interface{})
	if modeProp["type"] != "string" {
		t.Errorf("mode type: got %v", modeProp["type"])
	}
	if modeProp["title"] != "Mode" {
		t.Errorf("mode title: got %v", modeProp["title"])
	}
	if modeProp["description"] != "Scan mode" {
		t.Errorf("mode description: got %v", modeProp["description"])
	}
	if modeProp["default"] != "fast" {
		t.Errorf("mode default: got %v", modeProp["default"])
	}

	required := schema["required"].([]string)
	if len(required) != 1 || required[0] != "mode" {
		t.Errorf("required: got %v", required)
	}
}

func TestParamsToJSONSchema_ArrayWithItems(t *testing.T) {
	params := []Param{
		StringArrayParam("targets").Build(),
	}
	schema := ParamsToJSONSchema(params)
	props := schema["properties"].(map[string]interface{})
	targetProp := props["targets"].(map[string]interface{})

	items := targetProp["items"].(map[string]interface{})
	if items["type"] != "string" {
		t.Errorf("items type: got %v", items["type"])
	}
}
