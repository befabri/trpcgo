import { Link, Outlet, createFileRoute } from "@tanstack/react-router";
import { trpc } from "../trpc";
import { Spinner } from "./-components/spinner";
import { RoleBadge, StatusDot } from "./-components/badges";

export const Route = createFileRoute("/users")({
  component: UsersComponent,
});

function UsersComponent() {
  const { data, isLoading } = trpc.user.listUsers.useQuery({
    page: 1,
    perPage: 20,
  });

  if (isLoading) {
    return (
      <div className="p-4">
        <Spinner /> Loading users...
      </div>
    );
  }

  return (
    <div className="flex-1 flex">
      <div className="divide-y w-64">
        {data?.items.map((user) => (
          <div key={user.id}>
            <Link
              to="/users/$userId"
              params={{ userId: user.id }}
              preload="intent"
              className="block py-2 px-3 text-blue-700"
              activeProps={{ className: "font-bold" }}
            >
              <div className="flex items-center gap-2">
                <StatusDot status={user.status} />
                <span className="truncate">{user.name}</span>
                <span className="ml-auto">
                  <RoleBadge role={user.role} />
                </span>
              </div>
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
