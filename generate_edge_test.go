package trpcgo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/befabri/trpcgo"
)

type ZodCaseInput struct {
	Name  string `json:"name" validate:"lowercase,min=1,max=12"`
	Code  string `json:"code" validate:"uppercase,len=3"`
	Email string `json:"email" validate:"email,lowercase"`
}

type NestedTrackedInput struct {
	Text  trpcgo.TrackedEvent[string] `json:"text"`
	Count trpcgo.TrackedEvent[int]    `json:"count"`
}

func TestZodNestedTrackedInput(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(context.Context, NestedTrackedInput) (string, error) { return "", nil })
			checkGeneratedZod(t, r, `
import { NestedTrackedInputSchema as schema } from './schemas.ts';
const valid = { text: { ID: 'one', Data: 'hello', Retry: 0 }, count: { ID: 'two', Data: 2, Retry: 0 } };
schema.parse(valid);
if (schema.safeParse({ ...valid, count: { ...valid.count, Data: 'wrong' } }).success) throw new Error('tracked payload lost its concrete type');
`)
		})
	}
}

type ZodOmitEmailInput struct {
	Email string `json:"email,omitempty" validate:"omitempty,email"`
	Code  string `json:"code,omitempty" validate:"omitempty,len=3"`
	Count int    `json:"count" validate:"omitempty,gte=2"`
}

type ZodRecursiveInput struct {
	Label    string              `json:"label"`
	Children []ZodRecursiveInput `json:"children"`
	Secret   string              `json:"secret" zod_omit:"true"`
}

type ZodMutualA struct {
	Name string       `json:"name"`
	Bs   []ZodMutualB `json:"bs"`
}

type ZodMutualB struct {
	Value int          `json:"value"`
	As    []ZodMutualA `json:"as"`
}

type ZodNestedContainersInput struct {
	Matrix  [][]string                             `json:"matrix" validate:"min=1,dive,min=1,dive,min=2"`
	Groups  map[string][]string                    `json:"groups"`
	Buckets []map[string][]ZodContainerLeaf        `json:"buckets" validate:"dive,dive,dive"`
	Index   map[string]map[string]ZodContainerLeaf `json:"index" validate:"dive,dive"`
}

type ZodContainerLeaf struct {
	Value string `json:"value" validate:"min=1"`
}

type GenEmbeddedBase struct {
	Value string `json:"value"`
}

type GenShadowedInput struct {
	GenEmbeddedBase
	Value int `json:"value"`
}

type GenExtendedShadow struct {
	GenEmbeddedBase `tstype:",extends"`
	Value           int `json:"value"`
}

func TestZodCaseChecksRuntime(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodCaseInput) (string, error) {
				return "ok", nil
			})
			checkGeneratedZod(t, r, `
import { ZodCaseInputSchema as schema } from './schemas.ts';
const valid = { name: 'alice', code: 'ABC', email: 'alice@example.com' };
if (!schema.safeParse(valid).success) {
  throw new Error('valid lowercase and uppercase strings were rejected');
}
for (const input of [{ name: 'ALICE' }, { code: 'abc' }, { name: '' }, { code: 'ABCD' }, { email: 'ALICE@example.com' }, { email: 'invalid' }]) {
  if (schema.safeParse({ ...valid, ...input }).success) throw new Error('string constraint was ignored');
}
`)
		})
	}
}

func TestZodMiniOmitemptyEmailRuntime(t *testing.T) {
	r := trpcgo.NewRouter(trpcgo.WithZodMini(true))
	trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodOmitEmailInput) (string, error) {
		return "ok", nil
	})
	checkGeneratedZod(t, r, `
import { ZodOmitEmailInputSchema as schema } from './schemas.ts';
for (const input of [{}, { email: '' }, { email: 'alice@example.com' }, { code: '' }, { code: 'ABC' }, { count: 2 }]) {
  if (!schema.safeParse({ count: 0, ...input }).success) throw new Error('valid zero or nonzero value was rejected');
}
for (const input of [{ email: 'invalid' }, { code: 'AB' }, { count: 1 }, { count: '0' }]) {
  if (schema.safeParse({ count: 0, ...input }).success) throw new Error('invalid value was accepted');
}
// Go decodes a missing count as zero, which omitempty skips.
if (!schema.safeParse({}).success) throw new Error('missing omitempty count was rejected');
`)
}

func TestZodRecursiveSchemaModule(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodRecursiveInput) (string, error) {
				return "ok", nil
			})
			checkGeneratedZod(t, r, `
import { ZodRecursiveInputSchema as schema } from './schemas.ts';
const input = { label: 'root', children: [{ label: 'child', children: [] }] };
if (!schema.safeParse(input).success) throw new Error('valid recursive input was rejected');
if (schema.safeParse({ label: 'root', children: [{ label: 12, children: [] }] }).success) {
  throw new Error('invalid nested label was accepted');
}
const parsed = schema.parse(input);
const label: string = parsed.children[0].label;
// @ts-expect-error recursive output must retain its string type
const wrong: number = parsed.children[0].label;
const secret: string | undefined = parsed.secret;
// @ts-expect-error unvalidated fields keep their public type and may be absent
const secretString: string = parsed.secret;
if ('secret' in parsed) throw new Error('absent unvalidated field was materialized');
const opaque = { arbitrary: true };
if ((schema.parse({ ...input, secret: opaque } as unknown).secret as unknown) !== opaque) throw new Error('unvalidated field was not preserved');
void [label, wrong, secret];
`)
		})
	}
}

func TestZodMutualRecursionAndExtendsModule(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "mutual", func(context.Context, ZodMutualA) (string, error) { return "ok", nil })
			trpcgo.MustQuery(r, "extended", func(context.Context, ZodCyclicNode) (string, error) { return "ok", nil })
			checkGeneratedZod(t, r, `
import { ZodMutualASchema as mutual, ZodCyclicNodeSchema as extended } from './schemas.ts';
const input = { name: 'a', bs: [{ value: 1, as: [{ name: 'b', bs: [] }] }] };
const parsed = mutual.parse(input);
const name: string = parsed.bs[0].as[0].name;
// @ts-expect-error mutual recursion must retain leaf types
const wrong: number = parsed.bs[0].as[0].name;
void [name, wrong];
if (mutual.safeParse({ name: 'a', bs: [{ value: 'bad', as: [] }] }).success) throw new Error('invalid recursive leaf accepted');
extended.parse({ id: 'root', children: [{ id: 'child', children: [] }] });
if (extended.safeParse({ id: 'root', children: [{ id: 12, children: [] }] }).success) throw new Error('invalid inherited leaf accepted');
`)
		})
	}
}

func TestZodNestedContainersRuntime(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "check", func(_ context.Context, input ZodNestedContainersInput) (string, error) {
				return "ok", nil
			})
			checkGeneratedZod(t, r, `
import { ZodNestedContainersInputSchema as schema } from './schemas.ts';
const valid = { matrix: [['ab']], groups: { first: ['b'] }, buckets: [{ first: [{ value: 'x' }] }], index: { a: { b: { value: 'y' } } } };
if (!schema.safeParse(valid).success) {
  throw new Error('valid nested containers were rejected');
}
for (const input of [
  { matrix: [[12]] }, { matrix: [] }, { matrix: [[]] }, { matrix: [['a']] },
  { groups: { first: [12] } }, { buckets: [{ first: [{ value: 12 }] }] },
  { index: { a: { b: { value: '' } } } },
]) {
  if (schema.safeParse({ ...valid, ...input }).success) throw new Error('invalid nested value was accepted');
}
`)
		})
	}
}

func TestGenerateTSGenericConcreteFields(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			r := trpcgo.NewRouter()
			// Registration order decides which instantiation is reflected first;
			// Total and Page must stay int either way.
			intPath, stringPath := "a.ints", "b.strings"
			if reverse {
				intPath, stringPath = "b.ints", "a.strings"
			}
			trpcgo.MustVoidQuery(r, intPath, func(_ context.Context) (GenPage[int], error) {
				return GenPage[int]{}, nil
			})
			trpcgo.MustVoidQuery(r, stringPath, func(_ context.Context) (GenPage[string], error) {
				return GenPage[string]{}, nil
			})
			trpcgo.MustVoidQuery(r, "pair.same", func(context.Context) (GenPair[int, int], error) { return GenPair[int, int]{}, nil })
			trpcgo.MustVoidQuery(r, "pair.different", func(context.Context) (GenPair[string, bool], error) { return GenPair[string, bool]{}, nil })
			trpcgo.MustVoidQuery(r, "nested", func(context.Context) (GenPage[GenPair[string, int]], error) {
				return GenPage[GenPair[string, int]]{}, nil
			})
			checkGeneratedRouterContract(t, r, fmt.Sprintf(`
import type { RouterOutputs } from './trpc';
declare const response: RouterOutputs['%s']['strings'];
const total: number = response.total;
const page: number = response.page;
const items: string[] = response.items;
declare const same: RouterOutputs['pair']['same'];
declare const different: RouterOutputs['pair']['different'];
declare const nested: RouterOutputs['nested'];
const first: number = same.first;
const second: number = same.second;
const text: string = different.first;
const flag: boolean = different.second;
const nestedFirst: string = nested.items[0].first;
const nestedSecond: number = nested.items[0].second;
const nestedTotal: number = nested.total;
void [total, page, items, first, second, text, flag, nestedFirst, nestedSecond, nestedTotal];
`, stringPath[:1]))
		})
	}
}

type GenRecursive[T any] struct {
	Value    T                 `json:"value"`
	Count    int               `json:"count"`
	Children []GenRecursive[T] `json:"children"`
}

func TestGenerateRecursiveGenericContracts(t *testing.T) {
	for _, mini := range []bool{false, true} {
		t.Run(zodStyleName(mini), func(t *testing.T) {
			r := trpcgo.NewRouter(trpcgo.WithZodMini(mini))
			trpcgo.MustQuery(r, "number", func(_ context.Context, in GenRecursive[int]) (GenRecursive[int], error) { return in, nil })
			trpcgo.MustQuery(r, "text", func(_ context.Context, in GenRecursive[string]) (GenRecursive[string], error) { return in, nil })
			checkGeneratedZod(t, r, `
import * as schemas from './schemas.ts';
// Private $go variants sit beside the public schemas; only public exports count.
const all = Object.entries(schemas).filter(([name]) => !name.startsWith('$')).map(([, schema]) => schema);
if (all.length !== 2) throw new Error('instantiations were not specialized');
for (const value of [1, 'text']) {
  const input = { value, count: 1, children: [{ value, count: 2, children: [] }] };
  if (all.filter(schema => schema.safeParse(input).success).length !== 1) throw new Error('recursive instantiation lost its type');
  if (all.some(schema => schema.safeParse({ ...input, count: 'invalid' }).success)) throw new Error('concrete count lost its type');
}
`)
			checkGeneratedRouterContract(t, r, `
import type { RouterOutputs } from './trpc';
declare const text: RouterOutputs['text'];
declare const number: RouterOutputs['number'];
const value: string = text.children[0].value;
const count: number = text.count;
const numeric: number = number.children[0].value;
void [value, count, numeric];
`)
		})
	}
}

func TestGenerateTSShadowedEmbeddedField(t *testing.T) {
	r := trpcgo.NewRouter()
	trpcgo.MustVoidQuery(r, "shadow", func(_ context.Context) (GenShadowedInput, error) {
		return GenShadowedInput{}, nil
	})
	trpcgo.MustVoidQuery(r, "extended", func(context.Context) (GenExtendedShadow, error) { return GenExtendedShadow{}, nil })
	trpcgo.MustVoidQuery(r, "inline", func(context.Context) (struct {
		GenEmbeddedBase
		Value int `json:"value"`
	}, error) {
		return struct {
			GenEmbeddedBase
			Value int `json:"value"`
		}{}, nil
	})
	checkGeneratedRouterContract(t, r, `
import type { RouterOutputs } from './trpc';
declare const response: RouterOutputs['shadow'];
declare const extended: RouterOutputs['extended'];
declare const inline: RouterOutputs['inline'];
const value: number = response.value;
const extendedValue: number = extended.value;
const inlineValue: number = inline.value;
void [value, extendedValue, inlineValue];
`)
}
