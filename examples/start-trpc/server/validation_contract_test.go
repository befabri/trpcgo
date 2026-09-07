package main

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/befabri/trpcgo/testdata/validationcontract"
	"github.com/go-playground/validator/v10"
)

func TestValidationContractMatchesGo(t *testing.T) {
	testValidationCasesMatchGo(t, validationcontract.Cases())
}

func testValidationCasesMatchGo(t *testing.T, cases []validationcontract.Case) {
	t.Helper()
	validate := validator.New()
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
				err = validate.Struct(input)
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
	validate := validator.New()
	cases := validationcontract.Cases()
	coverage, err := validationcontract.Coverage(cases, func(tc validationcontract.Case) []string {
		input := validationcontract.NewInput(tc.Type)
		if err := decodeValidationContract(tc.JSON, input); err != nil {
			return nil
		}
		var errs validator.ValidationErrors
		if !errors.As(validate.Struct(input), &errs) {
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
	for _, tc := range validationcontract.CustomCases {
		t.Run(tc.Name, func(t *testing.T) {
			input := validationcontract.NewInput(tc.Type)
			if input == nil {
				t.Fatalf("unknown fixture %s", tc.Type)
			}
			err := decodeValidationContract(tc.JSON, input)
			if err == nil {
				err = v.Struct(input)
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
			panicked := false
			func() { defer func() { panicked = recover() != nil }(); _ = validator.New().Var(tc.value, tc.tag) }()
			if !panicked {
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
