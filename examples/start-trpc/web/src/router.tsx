import { QueryClient } from '@tanstack/react-query'
import { createRouter } from '@tanstack/react-router'
import { setupRouterSsrQueryIntegration } from '@tanstack/react-router-ssr-query'
import { createTRPCClient, httpBatchLink } from '@trpc/client'
import { createTRPCOptionsProxy } from '@trpc/tanstack-react-query'
import { routeTree } from './routeTree.gen'
import type { AppRouter } from '../gen/trpc.js'

export function getRouter() {
  const queryClient = new QueryClient()

  const trpc = createTRPCOptionsProxy<AppRouter>({
    client: createTRPCClient({
      links: [
        httpBatchLink({
          url: '/trpc',
          headers: () => ({ 'X-Request-ID': crypto.randomUUID() }),
        }),
      ],
    }),
    queryClient,
  })

  const router = createRouter({
    routeTree,
    context: { trpc, queryClient },
    defaultPreload: 'intent',
    scrollRestoration: true,
  })
  setupRouterSsrQueryIntegration({ router, queryClient })

  return router
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
