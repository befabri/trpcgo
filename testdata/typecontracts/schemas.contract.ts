import type { input, output } from 'zod';
import type { Assert, Equal } from './assertions';
import type { CreateUserInput, RouterInputs } from './trpc';
import { CreateUserInputSchema as standard } from './schemas';
import { CreateUserInputSchema as mini } from './schemas-mini';

type ExpectedInput = {
  name: string;
  email: string;
  nickname?: string;
  labels: string[];
  internalId?: string;
};

// zod_omit keeps the field in the schema with its TypeScript type and no
// runtime check, so parsed values stay assignable to the procedure input.
type DomainContract = Assert<Equal<CreateUserInput, ExpectedInput>>;
type RouterContract = Assert<Equal<RouterInputs['user']['create'], ExpectedInput>>;
type StandardInput = Assert<Equal<input<typeof standard>, ExpectedInput>>;
type StandardOutput = Assert<Equal<output<typeof standard>, ExpectedInput>>;
type MiniInput = Assert<Equal<input<typeof mini>, ExpectedInput>>;
type MiniOutput = Assert<Equal<output<typeof mini>, ExpectedInput>>;

const minimal = { name: 'Alice', email: 'alice@example.com', labels: [] } satisfies input<typeof standard>;
const complete = { ...minimal, nickname: 'Al' } satisfies input<typeof mini>;
const omitted = { ...minimal, internalId: 'internal' } satisfies input<typeof standard>;
// @ts-expect-error email remains required in the schema
const missing = { name: 'Alice', labels: [] } satisfies input<typeof standard>;
// @ts-expect-error optional strings do not become nullable
const nullable = { ...minimal, nickname: null } satisfies input<typeof mini>;
// @ts-expect-error omitted fields keep their TypeScript type
const mistyped = { ...minimal, internalId: 5 } satisfies input<typeof standard>;

const parsed = standard.parse(minimal);
const parsedMini = mini.parse(complete);
type ParsedName = Assert<Equal<typeof parsed.name, string>>;
type ParsedMiniLabels = Assert<Equal<typeof parsedMini.labels, string[]>>;
type ParsedInternal = Assert<Equal<typeof parsed.internalId, string | undefined>>;
const forwarded: CreateUserInput = standard.parse(omitted);
void forwarded;
