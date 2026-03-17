import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import type { Role, Status } from '../../gen/trpc.js'

export const Route = createFileRoute('/users/$userId')({
  loader: async ({ context: { trpc, queryClient }, params: { userId } }) => {
    await queryClient.ensureQueryData(trpc.user.get.queryOptions({ id: userId }))
  },
  component: UserDetailComponent,
})

const roleBadgeClass: Record<Role, string> = {
  admin: 'bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300',
  editor: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300',
  viewer: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
}

const statusBadgeClass: Record<Status, string> = {
  active: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300',
  inactive: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
  suspended: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
}

function UserDetailComponent() {
  const { userId } = Route.useParams()
  const { trpc, queryClient } = Route.useRouteContext()
  const navigate = useNavigate()

  const user = useQuery(trpc.user.get.queryOptions({ id: userId }))

  const deleteUser = useMutation({
    ...trpc.user.delete.mutationOptions(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [['user']] })
      navigate({ to: '/users' })
    },
  })

  if (!user.data) {
    return <p className="text-sm text-gray-400">Loading...</p>
  }

  const u = user.data

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h2 className="text-xl font-semibold">{u.name}</h2>
          <p className="text-sm text-gray-500">{u.email}</p>
        </div>
        <button
          onClick={() => {
            if (confirm(`Delete ${u.name}?`)) deleteUser.mutate({ id: u.id })
          }}
          disabled={deleteUser.isPending}
          className="text-sm px-3 py-1.5 border border-red-200 text-red-600 rounded-md hover:bg-red-50 dark:border-red-800 dark:hover:bg-red-950 disabled:opacity-50 transition-colors"
        >
          {deleteUser.isPending ? 'Deleting...' : 'Delete'}
        </button>
      </div>

      <div className="flex gap-2">
        <span className={`text-xs px-2 py-0.5 rounded ${roleBadgeClass[u.role]}`}>{u.role}</span>
        <span className={`text-xs px-2 py-0.5 rounded ${statusBadgeClass[u.status]}`}>{u.status}</span>
      </div>

      {u.bio && <p className="text-sm text-gray-600 dark:text-gray-400">{u.bio}</p>}

      <dl className="grid grid-cols-2 gap-4 text-sm">
        <div>
          <dt className="text-gray-500">ID</dt>
          <dd className="font-mono text-xs">{u.id}</dd>
        </div>
        <div>
          <dt className="text-gray-500">Created</dt>
          <dd>{new Date(u.createdAt).toLocaleDateString()}</dd>
        </div>
        <div>
          <dt className="text-gray-500">Updated</dt>
          <dd>{new Date(u.updatedAt).toLocaleDateString()}</dd>
        </div>
      </dl>
    </div>
  )
}
