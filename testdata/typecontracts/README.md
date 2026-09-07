# Generated TypeScript contracts

`TestGeneratedClientContracts` generates fresh TypeScript and Zod schemas from
`router.go`, using both reflection and static analysis. It then compiles the
fixtures in strict mode against the installed tRPC and Zod packages:

- `client.ts` checks valid calls, rejected calls, exact input and output types,
  optional and readonly fields, recursive types, and tracked subscriptions.
- `schemas.contract.ts` checks Zod and Zod Mini input/output inference against
  an independently written shape, including intentional `zod_omit` fields.
- `assertions.ts` provides exact type comparisons so widening a field to `any`
  cannot silently make a positive assertion pass.

These files are compiler tests; they do not execute client requests.
`TestZodRuntimeValidation` also compiles its assertion scripts before executing
the runtime checks for Zod and Zod Mini.

From the repository root:

```sh
npm ci --prefix testdata/zodruntime
go test -count=1 -run '^(TestGeneratedClientContracts|TestZodRuntimeValidation)$' .
```

The compiler, zod, and the tRPC packages all come from `testdata/zodruntime`
(TypeScript 7), so the example app's dependency versions never affect these
tests. CI also runs the Go suite with TypeScript 5.9.3 to check compatibility. To try another compiler, including a native build
from `.reference/typescript-go`, pass its executable path:

```sh
TRPCGO_TSC=/absolute/path/to/tsgo go test -count=1 -run '^TestGeneratedClientContracts$' .
```

Use `Assert<Equal<Actual, Expected>>` for exact inferred types. For a rejected
operation, place `@ts-expect-error` immediately above the offending line and
keep it different from a valid operation in one relevant way. If the compiler
starts accepting that operation, the unused directive makes the test fail.
