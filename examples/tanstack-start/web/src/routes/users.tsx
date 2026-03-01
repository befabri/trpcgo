import { Link, Outlet, createFileRoute } from "@tanstack/react-router";
import { Spinner } from "./-components/spinner";

export const Route = createFileRoute("/users")({
  loader: ({ context: { trpc } }) => trpc.user.listUsers.query(),
  pendingComponent: () => (
    <div className="p-4">
      <Spinner /> Loading users...
    </div>
  ),
  component: UsersComponent,
});

function UsersComponent() {
  const users = Route.useLoaderData();

  return (
    <div className="flex-1 flex">
      <div className="divide-y w-56">
        {users.map((user) => (
          <div key={user.id}>
            <Link
              to="/users/$userId"
              params={{ userId: user.id }}
              preload="intent"
              className="block py-2 px-3 text-blue-700"
              activeProps={{ className: "font-bold" }}
            >
              {user.name}
            </Link>
          </div>
        ))}
        <div>
          <Link
            to="/users/create"
            className="block py-2 px-3 text-green-700"
            activeProps={{ className: "font-bold" }}
          >
            + Create User
          </Link>
        </div>
      </div>
      <div className="flex-1 border-l border-gray-200">
        <Outlet />
      </div>
    </div>
  );
}
