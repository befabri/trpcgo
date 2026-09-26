package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/testdata/fieldcomposition"
	"github.com/befabri/trpcgo/testdata/validationcontract"
	"github.com/go-playground/validator/v10"
)

func TestValidationContractMatchesGo(t *testing.T) {
	testValidationCasesMatchGo(t, validationcontract.Cases())
}

func testValidationCasesMatchGo(t *testing.T, cases []validationcontract.Case) {
	t.Helper()
	// The documented router setup, which also validates collection roots.
	validate := trpcgo.StructValidator(validator.New().Struct)
	if len(cases) == 0 {
		t.Fatal("validation contract has no cases")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			input := validationcontract.NewInput(tc.Type)
			if input == nil {
				t.Fatalf("unknown contract type %q", tc.Type)
			}
			err := decodeValidationContract(tc.JSON, input)
			stage := "JSON decoding"
			if err == nil {
				stage = "Go validation"
				err = validate(input)
			}
			if valid := err == nil; valid != tc.Valid {
				t.Fatalf("Go acceptance = %v, want %v; type=%s, JSON=%s, %s error=%v", valid, tc.Valid, tc.Type, tc.JSON, stage, err)
			}
		})
	}
}

// Every supported tag needs a rejection that the Go validator attributes to
// that tag. A rejected case that fails on a sibling rule proves nothing about
// the tag under test, so this is stricter than the static gate in the library.
func TestValidationContractCoversSupportedTagsInGo(t *testing.T) {
	validate := trpcgo.StructValidator(validator.New().Struct)
	cases := validationcontract.Cases()
	coverage, err := validationcontract.Coverage(cases, func(tc validationcontract.Case) []string {
		input := validationcontract.NewInput(tc.Type)
		if err := decodeValidationContract(tc.JSON, input); err != nil {
			return nil
		}
		var errs validator.ValidationErrors
		if !errors.As(validate(input), &errs) {
			return nil
		}
		var tags []string
		for _, fe := range errs {
			tags = append(tags, validationcontract.RejectionTags(fe.ActualTag())...)
		}
		return tags
	})
	if err != nil {
		t.Fatal(err)
	}
	problems := validationcontract.Uncovered(coverage, true)
	for _, problem := range problems {
		t.Error(problem)
	}
	if len(problems) > 0 {
		t.Log("add cases to testdata/validationcontract whose rejection the Go validator attributes to each tag above")
	}
}

func TestNumericTagAcceptsGoIntegers(t *testing.T) {
	validate := validator.New()
	for _, value := range []int{42, -42, 0} {
		input := struct {
			Value int `validate:"numeric"`
		}{Value: value}
		if err := validate.Struct(input); err != nil {
			t.Errorf("numeric must accept Go integer %d: %v", value, err)
		}
	}
}

func decodeValidationContract(raw string, input any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return err
	}
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}

func TestCustomValidationContractMatchesGo(t *testing.T) {
	v := validator.New()
	config := validationcontract.CustomValidation()
	v.SetTagName(config.TagName)
	for name, body := range config.Aliases {
		v.RegisterAlias(name, body)
	}
	rules := map[string]validator.Func{
		"arraylength": func(fl validator.FieldLevel) bool {
			return (fl.Field().Len() == 2 && fl.Param() == "2") || (fl.Field().Len() == 3 && fl.Param() == "3")
		},
		"dynamic":     func(fl validator.FieldLevel) bool { return fl.Field().Int() >= 1 },
		"nilmap":      func(fl validator.FieldLevel) bool { return fl.Field().IsNil() },
		"zero":        func(fl validator.FieldLevel) bool { return fl.Field().Int() == 0 },
		"available":   func(validator.FieldLevel) bool { return true },
		"even":        func(fl validator.FieldLevel) bool { return fl.Field().Int()%2 == 0 && fl.Param() == "2" },
		"suffix":      func(fl validator.FieldLevel) bool { return strings.HasSuffix(fl.Field().String(), fl.Param()) },
		"rounded":     func(fl validator.FieldLevel) bool { return fl.Field().Float() == 16777216 },
		"large":       func(fl validator.FieldLevel) bool { return fl.Field().Int() == math.MaxInt64 },
		"truth":       func(fl validator.FieldLevel) bool { return fl.Field().Bool() },
		"replacement": func(fl validator.FieldLevel) bool { return fl.Field().String() == "\uFFFD" },
		"safe":        func(fl validator.FieldLevel) bool { return fl.Field().String() == "ok" },
	}
	for name, fn := range rules {
		if err := v.RegisterValidation(name, fn); err != nil {
			t.Fatal(err)
		}
	}
	v.RegisterStructValidation(func(sl validator.StructLevel) {
		value := sl.Current().Interface().(validationcontract.CustomStructInput)
		if value.End < value.Start {
			sl.ReportError(value.End, "End", "End", "window", "")
		}
	}, validationcontract.CustomStructInput{})
	v.RegisterStructValidation(func(sl validator.StructLevel) {
		value := sl.Current().Interface().(validationcontract.CustomWindow)
		if value.End < value.Start {
			sl.ReportError(value.End, "End", "End", "window", "")
		}
	}, validationcontract.CustomWindow{})
	for _, tc := range validationcontract.CustomCases {
		t.Run(tc.Name, func(t *testing.T) {
			input := validationcontract.NewInput(tc.Type)
			if input == nil {
				t.Fatalf("unknown fixture %s", tc.Type)
			}
			err := decodeValidationContract(tc.JSON, input)
			if err == nil {
				err = trpcgo.StructValidator(v.Struct)(input)
			}
			if (err == nil) != tc.Valid {
				t.Fatalf("Go validation = %v, want valid=%v; %s", err, tc.Valid, tc.JSON)
			}
		})
	}
}

// Generation rejects these types because the backend itself cannot evaluate
// their unique rule. They must not acquire invented JSON structural equality.
func TestUniqueUnsupportedValuesPanicInGoValidator(t *testing.T) {
	for _, tc := range []struct {
		name, tag string
		value     any
	}{
		{"maps", "unique", []map[string]int{{"a": 1}}},
		{"slices", "unique", [][]int{{1}}},
		{"arrays of slices", "unique", [][1][]int{{[]int{1}}}},
		{"missing selected field", "unique=Missing", []struct{ ID int }{{1}}},
		{"noncomparable selected field", "unique=ID", []struct{ ID []int }{{[]int{1}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if panicked, _ := recovers(func() { _ = validator.New().Var(tc.value, tc.tag) }); !panicked {
				t.Fatal("backend accepted an unsupported unique comparison")
			}
		})
	}
}

func TestUniqueNestedPointersUseGoIdentity(t *testing.T) {
	first, second := 1, 1
	type item struct{ Value *int }
	values := []item{{&first}, {&second}}
	if err := validator.New().Var(values, "unique"); err != nil {
		t.Fatalf("separate Go pointers with equal JSON values must stay distinct: %v", err)
	}
	values[1].Value = values[0].Value
	if err := validator.New().Var(values, "unique"); err == nil {
		t.Fatal("reusing the same pointer must fail unique")
	}
}

func TestGeneratedRefinementContractMatchesGo(t *testing.T) {
	v := validator.New()
	for _, tc := range slices.Concat(fieldcomposition.RefinementCases, fieldcomposition.ElementValidationCases, fieldcomposition.NumericEnumValidationCases) {
		t.Run(tc.Type+"/"+tc.JSON, func(t *testing.T) {
			input := fieldcomposition.NewRefinementInput(tc.Type)
			if err := json.Unmarshal([]byte(tc.JSON), input); err != nil {
				t.Fatal(err)
			}
			if err := v.Struct(input); (err == nil) != tc.Valid {
				t.Fatalf("Go validation=%v; generated schema must accept=%v", err, tc.Valid)
			}
		})
	}
}

// Use tagged fields, not Var: validator skips a struct's own rules when it
// has no field name. Every kind is exercised directly and through non-nil
// pointers/interfaces so their unwrapping cannot conceal an invalid rule.
func TestValidatorKindsMatchGenerator(t *testing.T) {
	type namedFloat float64
	type namedBool bool
	samples := []any{
		true, int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1), uintptr(1),
		float32(1), float64(1), complex64(1), complex128(1), "1",
		[1]int{1}, []int{1}, map[string]int{"one": 1}, struct{ N int }{1},
		make(chan int), func() {}, unsafe.Pointer(new(int)),
		time.Unix(1, 0).UTC(), []byte{1}, json.Number("1"), json.RawMessage(`1`),
		namedFloat(1), namedBool(true),
	}
	seen := make(map[reflect.Kind]bool)
	for _, value := range samples {
		seen[reflect.TypeOf(value).Kind()] = true
	}
	// Invalid has no field type; pointer and interface are tested as wrappers.
	for kind := reflect.Bool; kind <= reflect.UnsafePointer; kind++ {
		if kind != reflect.Pointer && kind != reflect.Interface && !seen[kind] {
			t.Fatalf("no validator sample for Go kind %s", kind)
		}
	}
	validate := validator.New()
	for _, tag := range validationcontract.SupportedTags() {
		t.Run(tag, func(t *testing.T) {
			for _, sample := range samples {
				typ := reflect.TypeOf(sample)
				kind := validationcontract.ValidatorKind(typ)
				t.Run(typ.String(), func(t *testing.T) {
					for _, value := range []reflect.Value{reflect.Zero(typ), reflect.ValueOf(sample)} {
						pointer := reflect.New(typ)
						pointer.Elem().Set(value)
						iface := reflect.New(reflect.TypeFor[any]()).Elem()
						iface.Set(value)
						for _, field := range []reflect.Value{value, pointer, iface} {
							input, rule := validatorKindInput(tag, field)
							panicked, panicValue := recovers(func() { _ = validate.Struct(input) })
							wantPanic := !validationcontract.ValidatorAcceptsKind(tag, kind)
							if panicked != wantPanic {
								t.Errorf("%s on %s (zero=%v): panic=%v (%v), generator expects panic=%v", rule, field.Type(), value.IsZero(), panicked, panicValue, wantPanic)
							}
						}
					}
				})
			}
		})
	}
}

// Ordered rules accept any time-convertible struct. Keep this distinction
// covered without treating all type-dependent panics as kind restrictions:
// nefield, for example, asserts time.Time directly instead of converting.
func TestOrderedValidatorKindsAcceptDefinedTime(t *testing.T) {
	type namedTime time.Time
	validate := validator.New()
	for _, tag := range []string{"min", "max", "gt", "gte", "lt", "lte"} {
		for _, sample := range []namedTime{{}, namedTime(time.Unix(1, 0).UTC())} {
			value := reflect.ValueOf(sample)
			input, rule := validatorKindInput(tag, value)
			panicked, panicValue := recovers(func() { _ = validate.Struct(input) })
			if panicked || !validationcontract.ValidatorAcceptsKind(tag, validationcontract.ValidatorKind(value.Type())) {
				t.Errorf("%s on defined time: panic=%v (%v), want both Go and generator to support the kind", rule, panicked, panicValue)
			}
		}
	}
}

// Generation rejects this tag, but a running server still needs to handle
// callers that never generated a schema or are using an older client. The
// root suite covers recovery itself; this checks the real validator's panic
// reaches the error hook with its diagnostics.
func TestValidatorKindPanicReachesErrorHook(t *testing.T) {
	type choiceInput struct {
		Value float64 `json:"value" validate:"oneof=1 2"`
	}
	reported := make(chan *trpcgo.Error, 1)
	r := trpcgo.NewRouter(
		trpcgo.WithValidator(trpcgo.StructValidator(validator.New().Struct)),
		trpcgo.WithOnError(func(_ context.Context, err *trpcgo.Error, _ string) { reported <- err }),
	)
	t.Cleanup(func() { _ = r.Close() })
	trpcgo.MustMutation(r, "choice", func(context.Context, choiceInput) (string, error) {
		t.Error("handler ran after validator panicked")
		return "", nil
	})
	result, err := r.RawCall(t.Context(), "choice", []byte(`{"value":1}`))
	trpcErr, ok := errors.AsType[*trpcgo.Error](err)
	if result != nil || !ok || trpcErr.Code != trpcgo.CodeInternalServerError || trpcErr.Cause != nil || trpcErr.Message != "internal server error" {
		t.Fatalf("RawCall = (%v, %v), want sanitized internal error", result, err)
	}
	if len(reported) != 1 {
		t.Fatalf("reported %d errors, want the panic", len(reported))
	}
	cause, ok := errors.AsType[*trpcgo.PanicError](<-reported)
	if !ok || !strings.Contains(cause.Error(), "Bad field type float64") || !strings.Contains(string(cause.Stack), "isOneOf") {
		t.Fatalf("lost validator panic diagnostics: %v", cause)
	}
}

func validatorKindInput(tag string, value reflect.Value) (any, string) {
	rule := tag
	switch tag {
	case "min", "max", "len", "gt", "gte", "lt", "lte", "eq", "ne", "oneof",
		"contains", "excludes", "containsany", "excludesall", "startswith", "endswith", "startsnotwith", "endsnotwith":
		// 1 is a valid signed/unsigned/float/length/bool parameter.
		rule += "=1"
	case "eqfield", "nefield", "gtfield", "gtefield", "ltfield", "ltefield":
		rule += "=Other"
	case "keys", "endkeys":
		// These delimit scopes rather than constrain kinds. Exercise each in
		// a valid map scope; malformed placement is tested by the parser.
		entries := reflect.MakeMap(reflect.MapOf(reflect.TypeFor[string](), value.Type()))
		entries.SetMapIndex(reflect.ValueOf("one"), value)
		value = entries
		rule = "dive,keys,endkeys"
	}
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "Value", Type: value.Type(), Tag: reflect.StructTag("validate:" + strconv.Quote(rule))},
		{Name: "Other", Type: value.Type()},
	})
	input := reflect.New(typ).Elem()
	input.Field(0).Set(value)
	input.Field(1).Set(value)
	return input.Interface(), rule
}

// recovers runs fn and reports whether it panicked, with the panic value.
func recovers(fn func()) (panicked bool, value any) {
	defer func() {
		value = recover()
		panicked = value != nil
	}()
	fn()
	return false, nil
}
