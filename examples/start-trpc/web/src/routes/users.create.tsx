import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { CreateUserInputSchema } from '../../gen/zod.js'
import type { CreateUserInput, Role } from '../../gen/trpc.js'

export const Route = createFileRoute('/users/create')({
  component: CreateUserComponent,
})

function CreateUserComponent() {
  const { trpc } = Route.useRouteContext()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [form, setForm] = useState<CreateUserInput>({
    name: '',
    email: '',
    role: '' as Role,
    bio: undefined,
  })
  const [errors, setErrors] = useState<Record<string, string>>({})

  const createUser = useMutation({
    ...trpc.user.create.mutationOptions(),
    onSuccess: (user) => {
      queryClient.invalidateQueries({ queryKey: [['user']] })
      navigate({ to: '/users/$userId', params: { userId: user.id } })
    },
  })

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setErrors({})

    const input = { ...form }
    if (!input.role) delete (input as any).role
    if (!input.bio) delete (input as any).bio

    const result = CreateUserInputSchema.safeParse(input)
    if (!result.success) {
      const fieldErrors: Record<string, string> = {}
      for (const issue of result.error.issues) {
        const key = String(issue.path[0])
        if (!fieldErrors[key]) fieldErrors[key] = issue.message
      }
      setErrors(fieldErrors)
      return
    }

    createUser.mutate(result.data as CreateUserInput)
  }

  return (
    <div className="max-w-md">
      <h2 className="text-xl font-semibold mb-6">Create User</h2>
      <form onSubmit={handleSubmit} className="space-y-4">
        <Field label="Name" error={errors.name}>
          <input
            type="text"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            className="w-full border rounded-md px-3 py-2 text-sm bg-transparent focus:outline-none focus:ring-1 focus:ring-gray-400"
            placeholder="Alice"
          />
        </Field>

        <Field label="Email" error={errors.email}>
          <input
            type="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            className="w-full border rounded-md px-3 py-2 text-sm bg-transparent focus:outline-none focus:ring-1 focus:ring-gray-400"
            placeholder="alice@example.com"
          />
        </Field>

        <Field label="Role" error={errors.role}>
          <select
            value={form.role}
            onChange={(e) => setForm({ ...form, role: e.target.value as Role })}
            className="w-full border rounded-md px-3 py-2 text-sm bg-transparent focus:outline-none focus:ring-1 focus:ring-gray-400"
          >
            <option value="">Default (viewer)</option>
            <option value="admin">Admin</option>
            <option value="editor">Editor</option>
            <option value="viewer">Viewer</option>
          </select>
        </Field>

        <Field label="Bio" error={errors.bio}>
          <textarea
            value={form.bio ?? ''}
            onChange={(e) => setForm({ ...form, bio: e.target.value || undefined })}
            rows={3}
            className="w-full border rounded-md px-3 py-2 text-sm bg-transparent focus:outline-none focus:ring-1 focus:ring-gray-400 resize-none"
            placeholder="Optional biography..."
          />
        </Field>

        {createUser.error && (
          <p className="text-sm text-red-600">{createUser.error.message}</p>
        )}

        <button
          type="submit"
          disabled={createUser.isPending}
          className="w-full py-2 text-sm font-medium border rounded-md hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors"
        >
          {createUser.isPending ? 'Creating...' : 'Create User'}
        </button>
      </form>
    </div>
  )
}

function Field({
  label,
  error,
  children,
}: {
  label: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-1">
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">{label}</label>
      {children}
      {error && <p className="text-xs text-red-600">{error}</p>}
    </div>
  )
}
