---
title: Frontend Setup
description: Use generated trpcgo router types with tRPC clients, React Query, TanStack Router, and generated Zod schemas.
---

Use the generated `AppRouter` type with tRPC v11 clients to get typed inputs, outputs, and procedure names in your frontend.

Start with the [package installation steps](/install/#frontend-packages), then [generate your router types](/code-generation/). The examples below assume generated files live in `gen/`; adjust the imports to match your project and use procedure names registered by your Go server.

A relative URL such as `/trpc` sends requests to the frontend's origin. Use it when your app and API share an origin or your dev server proxies `/trpc` to Go. Otherwise, use the Go server's full URL on each link.

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

If your frontend runs at `http://localhost:3000` and calls Go at `http://localhost:8080`, configure both CORS and trust for the frontend origin on the Go handler:

```go
handler := trpc.NewHandler(router, "/trpc",
    trpc.WithCORS(trpc.CORSConfig{
        AllowedOrigins: []string{"http://localhost:3000"},
    }),
    trpc.WithTrustedOrigins("http://localhost:3000"),
)
```

## React Query

Choose this integration if you want procedure hooks such as `trpc.user.get.useQuery()`. The [TanStack integration below](#tanstack-react-query-helpers) instead provides options for TanStack Query's own hooks.

```ts
// src/trpc.ts
import { createTRPCReact } from '@trpc/react-query';
import type { AppRouter } from '../gen/trpc.js';

export const trpc = createTRPCReact<AppRouter>();
```

Wrap your app in both providers. Create the clients once per mounted provider so rerenders keep the same query cache and connections:

```tsx
// src/AppProviders.tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { httpBatchLink, httpSubscriptionLink, splitLink } from '@trpc/client';
import { useState, type ReactNode } from 'react';
import { trpc } from './trpc';

export function AppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(() => new QueryClient());
  const [trpcClient] = useState(() => trpc.createClient({
    links: [
      splitLink({
        condition: (op) => op.type === 'subscription',
        true: httpSubscriptionLink({ url: '/trpc' }),
        false: httpBatchLink({ url: '/trpc' }),
      }),
    ],
  }));

  return (
    <trpc.Provider client={trpcClient} queryClient={queryClient}>
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    </trpc.Provider>
  );
}
```

## Cross-Origin Cookies

For cookie-authenticated apps served from a different origin in development, configure credentials on both transports:

```ts
import { createTRPCClient, httpBatchLink, httpSubscriptionLink, splitLink } from '@trpc/client';
import type { AppRouter } from '../gen/trpc.js';

const API_URL = 'http://localhost:8080';

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

On the Go handler, also set `AllowCredentials: true` in `trpc.CORSConfig`, alongside the exact `AllowedOrigins` and `WithTrustedOrigins` shown above. See [Security & Production](/security-production/#configure-cors) for a complete configuration.

## TanStack React Query Helpers

With `@trpc/tanstack-react-query`, create a typed context and provider:

```tsx
// src/trpc.tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createTRPCClient, httpBatchLink } from '@trpc/client';
import { createTRPCContext } from '@trpc/tanstack-react-query';
import { useState, type ReactNode } from 'react';
import type { AppRouter } from '../gen/trpc.js';

export const { TRPCProvider, useTRPC } = createTRPCContext<AppRouter>();

export function AppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(() => new QueryClient());
  const [trpcClient] = useState(() => createTRPCClient<AppRouter>({
    links: [httpBatchLink({ url: '/trpc' })],
  }));

  return (
    <QueryClientProvider client={queryClient}>
      <TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
        {children}
      </TRPCProvider>
    </QueryClientProvider>
  );
}
```

Inside a component rendered under `AppProviders`, call `useTRPC()` to get the typed options:

```tsx
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTRPC } from './trpc';

export function Users() {
  const trpc = useTRPC();
  const queryClient = useQueryClient();
  const users = useQuery(trpc.user.list.queryOptions({ page: 1, perPage: 20 }));
  const createUser = useMutation({
    ...trpc.user.create.mutationOptions(),
    onSuccess: () => queryClient.invalidateQueries(trpc.user.list.pathFilter()),
  });

  return (
    <button
      disabled={createUser.isPending}
      onClick={() => createUser.mutate({ name: 'Alice', email: 'alice@example.com' })}
    >
      {users.isPending ? 'Loading users…' : 'Add Alice'}
    </button>
  );
}
```

The [TanStack Start example](https://github.com/befabri/trpcgo/tree/main/examples/start-trpc) shows how to share a query client with TanStack Router.

## Infinite Queries

tRPC offers `infiniteQueryOptions` for a query whose input has a `cursor` field. Declare it on the Go input struct and return the next cursor with each page:

```go
type ListPostsInput struct {
    Limit  int     `json:"limit,omitempty"`
    Cursor *string `json:"cursor,omitempty"`
}

type PostPage struct {
    Items      []Post  `json:"items"`
    NextCursor *string `json:"nextCursor"`
}
```

```tsx
import { useInfiniteQuery } from '@tanstack/react-query';

const posts = useInfiniteQuery(trpc.post.list.infiniteQueryOptions(
  { limit: 20 },
  { getNextPageParam: (page) => page.nextCursor ?? undefined },
));
```

The integration sends `direction` with every page, including the first. Strict input drops it when the input struct does not declare it, so the handler above keeps working. Declare a `Direction` field with the `direction` JSON key only when the handler paginates in both directions.

## RouterInputs And RouterOutputs

Generated helpers let you reuse exact procedure types in UI code.

```ts
import type { RouterInputs, RouterOutputs } from '../gen/trpc.js';

type CreateUserInput = RouterInputs['user']['create'];
type CreatedUser = RouterOutputs['user']['create'];
```

## Client-Side Zod Validation

Generated schemas translate supported Go `validate` tags into client-side checks. Pass a plain input object to the schema; if your form gives you a browser `FormData`, convert it first and parse numeric or boolean fields as needed.

```ts
import { CreateUserInputSchema } from '../gen/zod.js';

const values = Object.fromEntries(formData.entries());
const parsed = CreateUserInputSchema.safeParse(values);
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
  source.close();
});

source.addEventListener('return', () => source.close());
```

Also call `source.close()` when the consuming component unmounts. Native `EventSource` reconnects when a connection closes, so closing it on terminal events prevents a completed stream from restarting.

For typed subscriptions, route subscription operations to `httpSubscriptionLink` and queries and mutations to `httpBatchLink`, as shown above. See [Subscriptions](/subscriptions/) for tracked events, input encoding, and reconnect behavior, and [Subscription Limitations](/reference/compatibility/#subscription-limitations) for final values and completion behavior.
