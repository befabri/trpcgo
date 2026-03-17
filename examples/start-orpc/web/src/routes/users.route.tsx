import { Link, Outlet, createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import type { Role, Status } from '../../gen/orpc.js'

export const Route = createFileRoute('/users')({
  loader: async ({ context: { orpc, queryClient } }) => {
    await queryClient.ensureQueryData(orpc.user.list.queryOptions({ input: { page: 1, perPage: 10 } }))
  },
  component: UsersLayout,
})

const roleBadgeClass: Record<Role, string> = {
  admin: 'bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300',
  editor: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300',
  viewer: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
}

const statusDotClass: Record<Status, string> = {
  active: 'bg-green-500',
  inactive: 'bg-gray-400',
  suspended: 'bg-red-500',
}

function UsersLayout() {
  const { orpc } = Route.useRouteContext()
  const [page, setPage] = useState(1)

  const users = useQuery(orpc.user.list.queryOptions({ input: { page, perPage: 10 } }))

  const data = users.data
  const totalPages = data ? Math.ceil(data.total / data.perPage) : 0

  return (
    <div className="flex gap-8 min-h-[60vh]">
      <aside className="w-64 shrink-0 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">Users</h2>
          <Link
            to="/users/create"
            className="text-sm px-2.5 py-1 border rounded-md hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
          >
            + New
          </Link>
        </div>
        {data ? (
          <ul className="space-y-1">
            {data.items.map((user) => (
              <li key={user.id}>
                <Link
                  to="/users/$userId"
                  params={{ userId: user.id }}
                  className="flex items-center gap-2 px-3 py-2 rounded-md text-sm hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
                  activeProps={{ className: 'bg-gray-100 dark:bg-gray-800 font-medium' }}
                >
                  <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${statusDotClass[user.status]}`} />
                  <span className="truncate flex-1">{user.name}</span>
                  <span className={`text-xs px-1.5 py-0.5 rounded ${roleBadgeClass[user.role]}`}>
                    {user.role}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-gray-400 px-3">Loading...</p>
        )}
        {totalPages > 1 && (
          <div className="flex gap-2 px-3">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page <= 1}
              className="text-xs px-2 py-1 border rounded disabled:opacity-30"
            >
              Prev
            </button>
            <span className="text-xs text-gray-500 py-1">
              {page} / {totalPages}
            </span>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
              className="text-xs px-2 py-1 border rounded disabled:opacity-30"
            >
              Next
            </button>
          </div>
        )}
      </aside>
      <div className="flex-1 min-w-0">
        <Outlet />
      </div>
    </div>
  )
}
