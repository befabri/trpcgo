import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { useEffect } from 'react'

export const Route = createFileRoute('/')({
  component: HomePage,
})

function HomePage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Dashboard</h1>
        <p className="text-sm text-gray-500 mt-1">trpcgo + oRPC + TanStack Start</p>
      </div>
      <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
        <HealthCard />
        <ResetCard />
        <LiveFeedCard />
      </div>
    </div>
  )
}

function HealthCard() {
  const { orpc } = Route.useRouteContext()
  const health = useQuery({
    ...orpc.system.health.queryOptions({ input: undefined }),
    refetchInterval: 5000,
  })

  return (
    <div className="border rounded-lg p-5 space-y-3">
      <h2 className="text-sm font-medium text-gray-500 uppercase tracking-wide">Health</h2>
      {health.data ? (
        <div className="space-y-1 text-sm">
          <div className="flex justify-between">
            <span className="text-gray-500">Status</span>
            <span className={health.data.ok ? 'text-green-600' : 'text-red-600'}>
              {health.data.ok ? 'Healthy' : 'Unhealthy'}
            </span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-500">Uptime</span>
            <span>{health.data.uptime}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-500">Users</span>
            <span>{health.data.userCount}</span>
          </div>
        </div>
      ) : health.isLoading ? (
        <p className="text-sm text-gray-400">Loading...</p>
      ) : (
        <p className="text-sm text-red-500">Failed to load</p>
      )}
    </div>
  )
}

function ResetCard() {
  const { orpc, queryClient } = Route.useRouteContext()
  const reset = useMutation({
    ...orpc.system.reset.mutationOptions(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orpc.system.key() })
      queryClient.invalidateQueries({ queryKey: orpc.user.key() })
    },
  })

  return (
    <div className="border rounded-lg p-5 space-y-3">
      <h2 className="text-sm font-medium text-gray-500 uppercase tracking-wide">Reset Demo</h2>
      <p className="text-sm text-gray-500">Reset data to initial seed using server-side Call.</p>
      <button
        onClick={() => reset.mutate(undefined)}
        disabled={reset.isPending}
        className="text-sm px-3 py-1.5 border rounded-md hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors"
      >
        {reset.isPending ? 'Resetting...' : 'Reset Data'}
      </button>
      {reset.data && (
        <p className="text-xs text-gray-500">{reset.data.message} ({reset.data.userCount} users)</p>
      )}
    </div>
  )
}

function LiveFeedCard() {
  const { orpc, queryClient } = Route.useRouteContext()

  const stream = useQuery(
    orpc.user.onCreated.experimental_streamedOptions({
      input: undefined,
      queryFnOptions: {
        refetchMode: 'append',
        maxChunks: 10,
      },
    }),
  )

  // Invalidate user queries when new events arrive.
  const users = stream.data ?? []
  useEffect(() => {
    if (users.length > 0) {
      queryClient.invalidateQueries({ queryKey: orpc.user.list.key() })
    }
  }, [users.length, queryClient, orpc])

  return (
    <div className="border rounded-lg p-5 space-y-3">
      <h2 className="text-sm font-medium text-gray-500 uppercase tracking-wide">Live Feed</h2>
      <p className="text-xs text-gray-400">SSE subscription — new users appear here in real-time</p>
      {users.length === 0 ? (
        <p className="text-sm text-gray-400">Waiting for new users...</p>
      ) : (
        <ul className="space-y-1">
          {[...users].reverse().map((u, i) => (
            <li key={`${u.id}-${i}`} className="text-sm flex items-center gap-2">
              <span className="w-1.5 h-1.5 rounded-full bg-green-500 shrink-0" />
              <span className="truncate">{u.name}</span>
              <span className="text-gray-400 text-xs ml-auto">{u.role}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
