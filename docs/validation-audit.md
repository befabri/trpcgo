# Validation and Zod audit

The implementation now fixes the defects reproduced by the original audit and
extends the regression suite. The audit compares trpcgo with the local
`.reference/validator` and `.reference/zod` checkouts. Passing the shared cases
establishes compatibility for those inputs; it does not establish compatibility
with every validator rule or every JSON representation.

The reference versions used for the investigation are validator `v10.30.4` and
Zod `v4.5.4-56-g7a002366`. The locked Zod test runtime is `4.5.4`.
The documented target is Zod 4.5.4 or a compatible newer 4.x release; native
string-length checks in this version count Unicode code points.

## Implemented changes

| Area | Behavior now covered | Implementation |
| --- | --- | --- |
| Recursive and generic types | Named maps and slices terminate during mapping and emit typed lazy schemas. Concrete generic inputs retain their own Go kinds, validation rules, and schema identities, including inherited types. | [static type graph](../internal/typemap/type_graph.go), [reflection type graph](../generate_type_graph.go), [schema specialization](../internal/codegen/zod_specialize.go) |
| Nested metadata | Anonymous structs retain field validation, omission, type overrides, comments, and cross-field refinements inside fields and containers. | [field metadata](../internal/typemap/typemap.go), [schema emission](../internal/codegen/zod.go) |
| Validation grammar | Rules retain their order, OR alternatives, escaped commas and pipes, and separate container, key, and element scopes. Earlier constraints survive a later `omitempty`. | [tag parsing](../internal/typemap/meta.go), [validation scopes](../internal/typemap/validation_scope.go), [rule rendering](../internal/typemap/zod_rules.go) |
| Numeric and scalar rules | Integer schemas check integral values and bounds. Float32 rules compare decoded values and parameters after rounding, including underflow, overflow, and ordered omission. `numeric` on a numeric Go kind no longer emits string checks. Combined formats and constraints remain active. Scalar `required` rejects zero values; non-nil pointers to zero remain valid. | [Go kind bases](../internal/typemap/zod.go), [scalar predicates](../internal/typemap/zod_rules.go) |
| Containers | Size constraints apply to maps and arrays; nested `dive` scopes remain separate. Integer key aliases normalize before key, element, and container validation. Raw JSON parsing preserves overwrite order; ambiguous ordinary objects fail validation. Overwritten values retain separate JSON type/range checks. | [integer map decoding](../internal/codegen/zod_integer_map.go), [ordered JSON](../internal/codegen/zod_json.go), [wire checks](../internal/codegen/zod_wire.go) |
| Wire representations | Primitive `json:",string"` fields remain strings in TypeScript and Zod. Integer checks use `BigInt`; quoted floats use Go's accepted grammar and exact rounding, shared with cross-field comparisons. String predicates normalize lone surrogates. Shared float decoding code emits once per module. | [wire metadata](../internal/typemap/type_graph.go), [decoded values](../internal/typemap/zod_decoded.go), [wire predicates](../internal/typemap/zod_rules.go) |
| Cross-field rules | Comparisons use Go kinds, pointer presence, zero values, and ordered omission. String ordering uses UTF-8 byte lengths; equality normalizes lone surrogate escapes as Go JSON decoding does. Collection equality compares lengths. Time comparisons normalize offsets and preserve nanoseconds. Integer strings compare without numeric rounding. | [reference binding](../internal/typemap/embedding.go), [refinement predicates](../internal/codegen/zod_refinement.go), [runtime helpers](../internal/codegen/zod_refinement_helpers.go) |
| Cross-field alternatives | Cross-field and scalar alternatives form one OR constraint with one field error path, including optional embedded bases. Missing direct Go field names retain validator's failure semantics, with the `nefield` exception. | [reference binding](../internal/typemap/embedding.go), [refinement predicates](../internal/codegen/zod_refinement.go) |
| Enums and maps | Named constants leave their underlying Go scalar type open, including map keys. Explicit `oneof` rules enforce closed membership; constants do not require map entries. | [schema emission](../internal/codegen/zod.go), [enum regression](../generate_wire_contract_test.go) |
| Repeated JSON fields | Raw object history is checked before merging typed struct and map fields. Map entries replace their values; repeated struct fields retain Go's merge/null behavior. Stale metadata cannot provide trusted ordering. | [typed merging](../internal/codegen/zod_wire_merge.go), [wire checks](../internal/codegen/zod_wire.go) |
| Format adapters | UUID versions, IPv4/IPv6, Base64 variants, hexadecimal prefixes, hostnames, MAC encodings, and CIDR constraints have dedicated regression cases and adapters where native Zod predicates differ. | [format adapters](../internal/typemap/zod_formats.go) |

Standard and mini schemas share the rule representation. Rendering uses typed
check descriptions rather than parsing generated JavaScript to convert between
Zod APIs. Invalid container grammar is checked before writing the module, so
misplaced `keys`, unmatched `endkeys`, and `dive` on non-containers produce
field-specific generation errors.

## Regression tests

[`testdata/validationcontract`](../testdata/validationcontract) shares typed
fixtures and independently specified JSON inputs between generated Zod and the
actual Go decoder and validator. The corpus includes valid and invalid controls,
Unicode boundaries, numeric ranges, combined formats, tag grammar, missing
values, container/key/value rules, nested structs, and cross-field comparisons.
It includes over 100 dedicated cross-field edge cases and 22 float32 cases covering
scalar rules, OR branches, container elements, and quoted numeric values.

Coverage of the corpus is enforced, not assumed. The generator's supported tag
table is the source of truth, and two tests read it directly:

- `TestValidationContractCoversSupportedTags` in the library requires that
  every supported tag is declared by at least one fixture that has both an
  accepted and a rejected case.
- `TestValidationContractCoversSupportedTagsInGo` in the example server runs
  the real validator and additionally requires, for every rule tag, a rejection
  that the validator attributes to that tag. A fixture whose rejected cases all
  fail on a sibling rule does not count. Structural directives such as
  `omitempty`, `omitnil`, `dive`, `keys`, and `endkeys` cannot fire on their
  own and are exempt from attribution.

Adding a tag to the supported table therefore fails both tests until the
corpus gains a fixture with positive and negative controls for it. The
`testdata/validationcontract/format_types.go` family holds the per-tag controls
that other families only exercise incidentally. Each feature calls `defineCases`
with its cases and typed fixtures, adding the cases to both suites. `inputFixture`
couples a fixture's constructor and router procedure to one Go type. Both mappers
generate exactly the types selected by a contract, including the uniqueness cases.

The follow-up review adds permanent cases for validation-only omission,
scalar `omitnil`, quoted float syntax and rounding, surrogate equality and
membership, and integer map aliasing. The float decoder also has 1,334 direct
comparisons with Go's decoder, checking exact float bits. Recursive generic
regressions cover anonymous objects inside fields, slices, and maps, and schema
specialization rejects ambiguous erased identities instead of selecting one by
traversal order.

Quoted integer, boolean, and string null spellings use the same decoded-value
rules in scalar validation, enum membership, and cross-field comparisons. A
separate wire-only module contract checks that these rules also work without
integer maps or the raw JSON helper.

The combined contract emits all default-validation fixtures in one module. Smaller
contracts isolate quoted scalars without container helpers, integer maps without
fixed arrays, and arrays without integer maps. This catches shared-helper
interactions, including quoted pointer nulls in modules with integer maps, without
compiling every regression family separately. Repeated-field fixtures independently
check merge, replacement, null, and retained decoding errors against Go.

Tests are organized by behavior. Root generator contracts cover validation, arrays,
wire decoding, type graphs, configuration, and client types; shared generation,
compiler, and runtime setup lives in `generate_helpers_test.go`. Every contract
resolves zod, tsx, the TypeScript compiler, and the tRPC packages from the one
locked tree in `testdata/zodruntime`. The fixture layout and the rules for adding
cases are in the `testdata/validationcontract` package comment.

[`TestZodValidationContract`](../generate_validation_contract_test.go) compiles
and executes each case with both static and reflection generation and both
standard and mini Zod. Every case must agree with its Go acceptance expectation.
There is no known-gap allowlist. Missing schemas, compilation errors, runtime
exceptions, and acceptance differences fail the test. Accepted output is decoded
back into the Go type and compared with the original decoded value, catching
field loss or unintended transformations.

Modules containing integer maps or fixed arrays expose `parseGoJSON`; the shared JSON corpus
uses it to retain source order. Independent object-input contracts verify that
ambiguous aliases are rejected, distinct keys normalize without mutating input,
stale order metadata is not trusted, and recursive input/output types compile.
The JSON helper has separate parser, duplicate-key, prototype, mutation, and
cross-module tests.

The actual Go oracle runs in the example server module to keep its validator
dependency out of the library module. Existing embedded-field and refinement
cases run there too. Additional contracts verify recursive containers, concrete
generic identity and inheritance, anonymous metadata, sparse enum-key maps, and
numeric-tag compilation.

Install the Node dependencies, then run the library contracts:

```bash
npm ci --prefix testdata/zodruntime
go test -run '^TestZod' -count=1 .
```

Run the Go oracle from `examples/start-trpc/server`:

```bash
go test -count=1 ./...
```

CI installs the Node dependencies and runs the root and example-server race
suites. Generated schemas are compiled with the locked TypeScript 7 compiler
and a TypeScript 5.9.3 compatibility job. Local runs without Node dependencies
may skip runtime contracts; CI treats missing dependencies as failures.

Schema-generated fuzz tests remain useful for self-consistency. They cannot
find a missing rule if that rule also disappears from their generated inputs.
New compatibility regressions should start from independent inputs, include
positive and negative controls, and reduce disagreements into shared fixtures.

## Additional compatibility work

The selected backend-relevant gaps now have shared implementation and regression
coverage:

- `WithValidator` runs once for every typed root, including typed nil, scalar,
  slice, map, pointer, and struct inputs. Void inputs skip it. Tests exercise
  query, mutation, subscription, and internal calls, including callback errors.
- `WithZodValidation(zodconfig.Config)` and CLI `--zod-config` supply explicit
  aliases, alternate tag names, scalar/collection predicates, object checks, and
  imports. Alias expansion precedes field-reference binding in both mappers.
  Strict configuration fails unsupported rules; explicit server-only rules
  remain diagnostic. Configuration errors preserve existing generated files.
- Email, URL, IP, and timestamp adapters compare directly with Go's parsers.
  The format suite has more than 4,000 independent oracle vectors, plus shared
  cases through both mappers and Zod styles. Unicode category and case checks
  cover all codepoints against the generator's Go Unicode tables and emit only
  the tables needed by a module. The target is Go 1.26 and validator v10.30.4.
- `unique` compares decoded scalar values, comparable structs, and fixed arrays.
  `unique=Field` resolves exported and promoted Go field names before JSON
  renaming or shadowing. Noncomparable unselected fields do not prevent a valid
  field selection. Independent cases cover structural equality, float32,
  quoted int64, lone surrogates, pointers, map behavior, and nested scopes.
- Fixed arrays retain their Go length and decode by padding zero values or
  skipping surplus entries before validation. Tests include nested and named
  arrays, byte arrays, repeated fields, and ignored invalid surplus values.
  Zero-value required/omission checks retain order and distinguish nil from
  allocated empty collections. Element validation follows dive for both
  supplied and absent arrays; optional missing-array checks run on Go's zero
  array at the containing object. Parsed outputs type-check as client inputs
  and outputs, including recursive and concrete generic types.
  Contextual public type variants admit synthesized nil values in array positions
  while ordinary structs keep their existing null policy. Compiler contracts pass
  parsed outputs to router inputs and outputs, including recursive structs and
  generic instances whose public arguments erase distinct Go pointer types.
- Objects reject unknown fields by default. `WithStrictInput(false)` and CLI
  `--zod-allow-unknown-fields` preserve unknown properties. An explicit
  `zod_omit` field remains optional and unvalidated when supplied.
- Inferred constants no longer close a Go scalar type. Both public TypeScript
  types and Zod schemas accept unlisted values within Go bounds. Explicit
  `oneof` still narrows inputs. Constants remain available for autocomplete and
  runtime use.

The custom-configuration corpus is also checked with real Go registrations.
It exercises nested aliases, alternate tags, cross-field OR branches, key and
value scopes, decoded quoted values, imports, missing values, explicit object
checks, and server-only alternatives. Imported predicates that throw, return a
non-boolean, or return a fulfilled/rejected Promise fail synchronously without
leaking an unhandled rejection.

## Remaining boundaries

- **Server code is not browser code.** Configuration does not inspect arbitrary
  callbacks, custom JSON/text unmarshaling, database queries, authorization,
  filesystem access, or service dependencies. Supply a portable predicate when
  one exists; mark other rules server-only. Explicit object checks apply to
  generated named object schemas; flattened embedded checks belong on their
  containing object. These checks do not reproduce Go reflection callbacks.
- **Some validator features still need implementation.** Nested field paths,
  `*csfield`, conditional required/excluded rules, and other unlisted tags are
  outside the supported set. Unsupported tags produce comments, or generation
  errors with `Strict: true`. Nested reference paths and malformed scopes fail
  generation instead of emitting misleading code.
- **JavaScript values lose source information.** Ordinary JSON numbers cannot
  preserve every Go int64/uint64 or lexical distinctions such as `1` versus
  `1.0`. Use integer `json:",string"` for exact full-range integers. Plain Go
  `int` uses a safe-integer schema. Original escapes inside time.Time JSON
  strings are another lexical distinction a decoded object cannot recover.
- **Go identity has no portable JSON counterpart.** The validator dereferences
  collection elements or selected fields once for `unique`. Nested pointer
  identity, inaccessible selectors, and noncomparable values that would panic
  in the Go validator produce explicit errors. Map parameters are ignored by
  the backend; map uniqueness compares whole values.
  Fixed-array zero-value rules also reject cases requiring hidden fields,
  time.Time location identity, or JSON representations that cannot distinguish
  json.Number/json.RawMessage's Go zero from a supplied value.
- **Schema policy remains explicit.** Optional properties do not universally
  accept JSON null; coercion, defaults, transformations, and custom type tags
  are not inferred from server code. Output-only types do not receive input
  schemas. Modules requiring Go decoding for integer maps or fixed arrays use
  object pipelines, which do not expose `.shape` or `.safeExtend()`.
- **Ordered JSON parsing has a defined scope.** `parseGoJSON` retains source
  occurrences needed for integer aliases and repeated-field merging. It is
  not a complete Go decoder and cannot recover precision already lost from
  JavaScript numbers. Unknown overwritten properties and original entry order
  require raw JSON text. Unsupported custom map key kinds fail generation.

## Further feature work

1. Preserve nested Go namespace metadata to implement `*csfield` and conditional
   rules without guessing paths from renamed TypeScript properties.
2. Extend structured diagnostics with tag positions and machine-readable status.
3. Broaden raw JSON lexical metadata where a real API needs exact numeric or
   custom-decoder input semantics; keep such wire behavior explicit.
4. Add explicit nullability and output-schema policies if applications need them.

The implementation and its tests should grow together: preserve a concrete
failing input, implement the rule in the shared representation, and verify the
result through both mappers, both Zod APIs, and the Go oracle.
