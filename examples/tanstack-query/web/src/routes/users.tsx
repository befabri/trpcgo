import { useState } from "react";
import { Link, Outlet, createFileRoute } from "@tanstack/react-router";
import { trpc } from "../trpc";
import { Spinner } from "./-components/spinner";
import { RoleBadge, StatusDot } from "./-components/badges";
import { ListUsersInputSchema } from "../../gen/zod";

export const Route = createFileRoute("/users")({
  component: UsersComponent,
});

function UsersComponent() {
  const [page, setPage] = useState(1);
  const perPage = 5;
  const utils = trpc.useUtils();

  const validated = ListUsersInputSchema.safeParse({ page, perPage });
  const { data, isLoading } = trpc.user.listUsers.useQuery(
    { page, perPage },
    { enabled: validated.success },
  );

  // Auto-refresh the list when any tab creates a user (via SSE subscription).
  trpc.user.onCreated.useSubscription(undefined, {
    onData: () => {
      utils.user.listUsers.invalidate();
    },
  });

  if (isLoading) {
    return (
      <div className="p-4">
        <Spinner /> Loading users...
      </div>
    );
  }

  const totalPages = data ? Math.ceil(data.total / data.perPage) : 0;

  return (
    <div className="flex-1 flex">
      <div className="w-64 flex flex-col">
        <div className="divide-y flex-1">
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
        {totalPages > 1 && (
          <div className="flex items-center justify-between border-t px-3 py-2 text-sm">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page <= 1}
              className="text-blue-600 disabled:text-gray-300"
            >
              Prev
            </button>
            <span className="text-gray-500">
              {page} / {totalPages}
            </span>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
              className="text-blue-600 disabled:text-gray-300"
            >
              Next
            </button>
          </div>
        )}
      </div>
      <div className="flex-1 border-l border-gray-200">
        <Outlet />
      </div>
    </div>
  );
}
