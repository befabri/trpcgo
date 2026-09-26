// Package gojson reports how this binary's encoding/json decodes the inputs
// on which Go releases disagree.
//
// Go 1.27 implements encoding/json on encoding/json/v2 unless built with
// GOEXPERIMENT=nojsonv2. Each variable probes the decoder rather than the Go
// version, so generated schemas follow the toolchain that builds the generator.
package gojson

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

var (
	// QuotedNumberPrefix reports that a quoted integer or float must begin
	// with a minus sign or a digit. Otherwise anything strconv parses decodes,
	// including a leading plus sign or point, infinities and NaN.
	QuotedNumberPrefix = !decodes[struct {
		V float64 `json:"v,string"`
	}](`{"v":"+1"}`)

	// LenientQuotedNumber reports that a quoted json.Number stores any text
	// beginning with a minus sign or a digit, unquotes a nested string
	// literal, and ignores the quoted null. Otherwise the payload must be a
	// JSON number.
	LenientQuotedNumber = decodes[struct {
		V json.Number `json:"v,string"`
	}](`{"v":"1x"}`)

	// ReplacesQuotedSurrogates reports that an unpaired surrogate escape in
	// a quoted string decodes to U+FFFD, as it does in any JSON string.
	// Otherwise the payload is rejected.
	ReplacesQuotedSurrogates = decodes[struct {
		V string `json:"v,string"`
	}](`{"v":"\"\\ud800\""}`)
)

func decodes[T any](data string) bool {
	var v T
	return json.Unmarshal([]byte(data), &v) == nil
}

type fieldName struct {
	name    string
	dropped bool
}

var fieldNames sync.Map // tag name → fieldName

// FieldName returns the key encoding/json decodes into a field with the json
// tag value tag. name is "" when the tag names no valid key and the field
// keeps its Go name. dropped reports a field the decoder never sets, which
// Go 1.27's default decoder does for a name holding a backslash or quote.
func FieldName(tag string) (name string, dropped bool) {
	if tag == "-" {
		return "", true
	}
	name, _, _ = strings.Cut(tag, ",")
	if name == "" {
		return "", false
	}
	if cached, ok := fieldNames.Load(name); ok {
		result := cached.(fieldName)
		return result.name, result.dropped
	}
	// The probe's Go name must differ from the tag name to tell the two apart;
	// the field's type and options do not affect the key.
	goName := "F"
	if name == goName {
		goName = "G"
	}
	// A lone dash skips the field; the name "-" needs the comma after it.
	probeTag := name
	if name == "-" {
		probeTag += ","
	}
	probe := reflect.StructOf([]reflect.StructField{{
		Name: goName,
		Type: reflect.TypeFor[int](),
		Tag:  reflect.StructTag(`json:` + strconv.Quote(probeTag)),
	}})
	sets := func(key string) bool {
		data, err := json.Marshal(map[string]int{key: 1})
		value := reflect.New(probe)
		return err == nil && json.Unmarshal(data, value.Interface()) == nil && value.Elem().Field(0).Int() == 1
	}
	result := fieldName{dropped: true}
	switch {
	case sets(name):
		result = fieldName{name: name}
	case sets(goName):
		result = fieldName{}
	}
	fieldNames.Store(name, result)
	return result.name, result.dropped
}
