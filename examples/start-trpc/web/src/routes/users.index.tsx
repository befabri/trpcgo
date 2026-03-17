import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/users/')({
  component: UsersIndexComponent,
})

function UsersIndexComponent() {
  return (
    <div className="flex items-center justify-center h-full text-gray-400 text-sm">
      Select a user from the list or create a new one.
    </div>
  )
}
