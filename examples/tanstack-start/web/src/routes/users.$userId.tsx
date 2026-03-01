import { createFileRoute } from "@tanstack/react-router";
import { Spinner } from "./-components/spinner";

export const Route = createFileRoute("/users/$userId")({
  loader: ({ context: { trpc }, params: { userId } }) =>
    trpc.user.getUserById.query({ id: userId }),
  pendingComponent: () => (
    <div className="p-4">
      <Spinner /> Loading user...
    </div>
  ),
  component: UserDetailComponent,
});

function UserDetailComponent() {
  const user = Route.useLoaderData();

  return (
    <div className="p-4 space-y-3">
      <h3 className="text-xl font-bold">{user.name}</h3>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
        <dt className="font-semibold text-gray-500">ID</dt>
        <dd>{user.id}</dd>
        <dt className="font-semibold text-gray-500">Name</dt>
        <dd>{user.name}</dd>
        <dt className="font-semibold text-gray-500">Email</dt>
        <dd>{user.email}</dd>
      </dl>
    </div>
  );
}
