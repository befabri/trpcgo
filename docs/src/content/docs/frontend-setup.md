---
title: Frontend Setup
description: Use generated trpcgo router types with tRPC clients, React Query, TanStack Router, and generated Zod schemas.
---

The generated `AppRouter` type plugs into normal tRPC v11 clients.

## Vanilla Client

```ts
import { createTRPCClient, httpBatchLink } from '@trpc/client';
import type { AppRouter } from '../gen/trpc.js';

export const client = createTRPCClient<AppRouter>({
  links: [
    httpBatchLink({
      url: 'http://localhost:8080/trpc',
    }),
  ],
});

const user = await client.user.get.query({ id: '1' });
const created = await client.user.create.mutate({ name: 'Alice', email: 'alice@example.com' });
```

## React Query

```ts
import { createTRPCReact } from '@trpc/react-query';
import type { AppRouter } from '../gen/trpc.js';

export const trpc = createTRPCReact<AppRouter>();
```

Create a client with query batching and SSE subscription routing:

```ts
import { httpBatchLink, httpSubscriptionLink, splitLink } from '@trpc/client';

const trpcClient = trpc.createClient({
  links: [
    splitLink({
      condition: (op) => op.type === 'subscription',
      true: httpSubscriptionLink({ url: '/trpc' }),
      false: httpBatchLink({ url: '/trpc' }),
    }),
  ],
});
```

For cookie-authenticated apps served from a different origin in development, configure credentials on both transports:

```ts
import { createTRPCClient, httpBatchLink, httpSubscriptionLink, splitLink } from '@trpc/client';
import type { AppRouter } from '../gen/trpc.js';

const trpcClient = createTRPCClient<AppRouter>({
  links: [
    splitLink({
      condition: (op) => op.type === 'subscription',
      true: httpSubscriptionLink({
        url: `${API_URL}/trpc`,
        eventSourceOptions: { withCredentials: true },
      }),
      false: httpBatchLink({
        url: `${API_URL}/trpc`,
        fetch(url, options) {
          return fetch(url, { ...options, credentials: 'include' });
        },
      }),
    }),
  ],
});
```

## TanStack React Query Helpers

With `@trpc/tanstack-react-query`, create a typed context and provider:

```tsx
import { QueryClient } from '@tanstack/react-query';
import { createTRPCClient, httpBatchLink } from '@trpc/client';
import { createTRPCContext } from '@trpc/tanstack-react-query';
import type { ReactNode } from 'react';
import type { AppRouter } from '../gen/trpc.js';

export const { TRPCProvider, useTRPC } = createTRPCContext<AppRouter>();

const queryClient = new QueryClient();
const trpcClient = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: '/trpc' })],
});

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
      {children}
    </TRPCProvider>
  );
}
```

Then use generated query and mutation options:

```ts
const users = useQuery(trpc.user.list.queryOptions({ page: 1, perPage: 20 }));

const createUser = useMutation({
  ...trpc.user.create.mutationOptions(),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: [['user']] }),
});
```

## RouterInputs And RouterOutputs

Generated helpers let you reuse exact procedure types in UI code.

```ts
import type { RouterInputs, RouterOutputs } from '../gen/trpc.js';

type CreateUserInput = RouterInputs['user']['create'];
type CreatedUser = RouterOutputs['user']['create'];
```

## Client-Side Zod Validation

Generated schemas match Go `validate` tags for procedure inputs.

```ts
import { CreateUserInputSchema } from '../gen/zod.js';

const parsed = CreateUserInputSchema.safeParse(formData);
if (!parsed.success) {
  setErrors(parsed.error.flatten().fieldErrors);
  return;
}

await client.user.create.mutate(parsed.data);
```

See [Zod Schemas](/zod-schemas/) for generation options and supported validation tags.

## Subscriptions With EventSource

Subscriptions are SSE streams. You can consume them directly:

```ts
const source = new EventSource('/trpc/user.onCreated');

source.onmessage = (event) => {
  const user = JSON.parse(event.data);
  console.log(user);
};

source.addEventListener('serialized-error', (event) => {
  console.error(JSON.parse(event.data));
});
```

If you use tRPC's subscription link, route subscription operations to `httpSubscriptionLink` and queries/mutations to `httpBatchLink`.
