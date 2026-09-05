package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/befabri/trpcgo/testdata/fieldcomposition"
	"github.com/go-playground/validator/v10"
)

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
