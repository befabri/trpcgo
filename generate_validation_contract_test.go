package trpcgo_test

import (
	"context"
	"slices"
	"testing"

	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/testdata/validationcontract"
)

// The complete corpus runs in one module to expose interactions between helpers.
func TestZodValidationContract(t *testing.T) {
	testZodValidationCases(t, validationcontract.Cases(), inputPolicyAssertions)
}

// This module has no integer maps or fixed arrays. Quoted decoding and scalar
// refinements must work without those helpers being pulled in by another input.
func TestZodQuotedWireContract(t *testing.T) {
	cases := slices.Concat(validationcontract.WireCases, validationcontract.ScalarBoundaryCases, validationcontract.CrossFieldOmissionCases)
	testZodValidationCases(t, cases, `if (exports.parseGoJSON !== undefined) throw new Error('wire isolation gained an unrelated JSON helper');`)
}

type NumericTagNumberInput struct {
	Value int `json:"value" validate:"numeric"`
}

// numeric on a Go number must compile and keep rejecting nonnumeric object input.
func TestZodNumericTagContract(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			t.Cleanup(func() { _ = r.Close() })
			trpcgo.MustQuery(r, "numericTag", func(context.Context, NumericTagNumberInput) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `import { NumericTagNumberInputSchema as schema } from './schemas';
for (const value of [42, -42, 0]) schema.parse({value});
for (const value of [1.5, "42", true, null]) {
 if (schema.safeParse({value}).success) throw new Error('numeric accepted ' + JSON.stringify(value));
}`)
		})
	}
}

// Every tag the generator supports must be exercised by the shared corpus in
// both directions. The generator's table is the source of truth, so adding a
// tag without fixtures fails here; the Go oracle in examples/start-trpc/server
// additionally requires that the validator itself rejects a case for the tag.
func TestValidationContractCoversSupportedTags(t *testing.T) {
	cases := validationcontract.Cases()
	coverage, err := validationcontract.Coverage(cases, nil)
	if err != nil {
		t.Fatal(err)
	}
	problems := validationcontract.Uncovered(coverage, false)
	for _, problem := range problems {
		t.Error(problem)
	}
	if len(problems) > 0 {
		t.Log("add accepted and rejected cases to testdata/validationcontract for each tag above")
	}
}

const inputPolicyAssertions = `
import type { core } from 'zod';
const customRole: core.output<typeof schemas.PolicyEnumInputSchema>['role'] = 'application-defined';
const customStatus: core.output<typeof schemas.PolicyEnumInputSchema>['status'] = 2;
void customRole; void customStatus;
const provided = schemas.PolicyOmitInputSchema.parse({name:'ok',id:'supplied',keys:{}});
if (provided.id !== 'supplied') throw new Error('known omitted field was discarded');
const unvalidated = schemas.PolicyOmitInputSchema.parse({name:'ok',id:{supplied:true},keys:{}});
if (typeof unvalidated.id !== 'object') throw new Error('omitted field unexpectedly validated');
`
