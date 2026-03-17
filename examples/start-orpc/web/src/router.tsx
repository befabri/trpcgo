import { QueryClient } from '@tanstack/react-query'
import { createRouter } from '@tanstack/react-router'
import { setupRouterSsrQueryIntegration } from '@tanstack/react-router-ssr-query'
import { createORPCClient } from '@orpc/client'
import { RPCLink } from '@orpc/client/fetch'
import { createTanstackQueryUtils } from '@orpc/tanstack-query'
import { routeTree } from './routeTree.gen'
import type { RouterClient } from '../gen/orpc.js'

export function getRouter() {
  const queryClient = new QueryClient()

  const baseUrl = typeof window !== 'undefined'
    ? `${window.location.origin}/rpc`
    : 'http://localhost:8080/rpc'
  const client = createORPCClient<RouterClient>(new RPCLink({
    url: baseUrl,
    headers: () => ({ 'X-Request-ID': crypto.randomUUID() }),
  }))
  const orpc = createTanstackQueryUtils(client)

  const router = createRouter({
    routeTree,
    context: { orpc, queryClient },
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
