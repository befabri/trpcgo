import type { TRPCOptionsProxy } from '@trpc/tanstack-react-query'
import type { QueryClient } from '@tanstack/react-query'
import type { AppRouter } from '../../gen/trpc.js'

export interface RouterAppContext {
  trpc: TRPCOptionsProxy<AppRouter>
  queryClient: QueryClient
}
