import type { RouterUtils } from '@orpc/tanstack-query'
import type { QueryClient } from '@tanstack/react-query'
import type { RouterClient } from '../../gen/orpc.js'

export type ORPCUtils = RouterUtils<RouterClient>

export interface RouterAppContext {
  orpc: ORPCUtils
  queryClient: QueryClient
}
