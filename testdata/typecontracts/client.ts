import { createTRPCClient } from '@trpc/client';
import type { Assert, Equal } from './assertions';
import type { AppRouter, RouterInputs, RouterOutputs, User } from './trpc';

type CreateInputContract = Assert<Equal<RouterInputs['user']['create'], {
  name: string;
  email: string;
  nickname?: string;
  labels: string[];
  internalId?: string;
}>>;
type GetInputContract = Assert<Equal<RouterInputs['user']['get'], { id: string }>>;
type NameContract = Assert<Equal<User['name'], string>>;
type NicknameContract = Assert<Equal<User['nickname'], string | undefined>>;
type PayloadContract = Assert<Equal<User['payload'], unknown>>;
type RecursiveContract = Assert<Equal<User['children'], User[]>>;
type PublicFields = Assert<Equal<keyof User, 'id' | 'name' | 'email' | 'nickname' | 'payload' | 'children'>>;
// tRPC serializes outputs: unknown fields can contain undefined and become
// optional in the JSON result, including inside recursive objects.
type ClientUser = {
  readonly id: string;
  name: string;
  email: string;
  nickname?: string;
  payload?: unknown;
  children: ClientUser[];
};
type GetOutputContract = Assert<Equal<RouterOutputs['user']['get'], ClientUser>>;
type CreateOutputContract = Assert<Equal<RouterOutputs['user']['create'], ClientUser>>;
type ListOutputContract = Assert<Equal<RouterOutputs['user']['list'], ClientUser[]>>;

const client = createTRPCClient<AppRouter>({ links: [] });

// Minimal and complete inputs must both remain callable.
client.user.create.mutate({ name: 'Alice', email: 'alice@example.com', labels: [] });
client.user.create.mutate({ name: 'Alice', email: 'alice@example.com', labels: ['staff'], nickname: 'Al', internalId: 'internal' });
// Each invalid call differs from a valid one in a single relevant way.
// @ts-expect-error names must remain strings
client.user.create.mutate({ name: 123, email: 'alice@example.com', labels: [] });
// @ts-expect-error email is required
client.user.create.mutate({ name: 'Alice', labels: [] });
// @ts-expect-error labels is required even when empty
client.user.create.mutate({ name: 'Alice', email: 'alice@example.com' });
// @ts-expect-error array elements must remain strings
client.user.create.mutate({ name: 'Alice', email: 'alice@example.com', labels: [123] });
// @ts-expect-error a mutation must not become a query
client.user.create.query({ name: 'Alice', email: 'alice@example.com', labels: [] });

client.user.list.query();
// @ts-expect-error a void query does not accept an input object
client.user.list.query({ page: 1 });
// @ts-expect-error the lookup requires its id
client.user.get.query({});

const user = await client.user.get.query({ id: 'user-1' });
type InferredName = Assert<Equal<typeof user.name, string>>;
const name: string = user.name;
user.name = 'Updated';
// @ts-expect-error the generated readonly modifier must reach the client
user.id = 'replacement';
// @ts-expect-error private JSON fields must not reach the client
user.secretToken;
// @ts-expect-error optional fields require narrowing
const nickname: string = user.nickname;
// @ts-expect-error unknown JSON values require narrowing
user.payload.value;
// @ts-expect-error recursive leaves retain their types
const childName: number = user.children[0].name;

client.user.events.subscribe(undefined, { onData(event) {
  type EventID = Assert<Equal<typeof event.id, string>>;
  type EventName = Assert<Equal<typeof event.data.name, string>>;
  const data: ClientUser = event.data;
  // @ts-expect-error tracked events wrap the payload in data
  event.name;
  // @ts-expect-error tracked ids must not widen to any
  const id: number = event.id;
} });
// @ts-expect-error subscriptions must not expose query calls
client.user.events.query();
