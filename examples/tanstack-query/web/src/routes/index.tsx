import { useState } from "react";
import { Link, createFileRoute } from "@tanstack/react-router";
import { trpc } from "../trpc";
import { Spinner } from "./-components/spinner";
import type { User } from "../../gen/trpc";

export const Route = createFileRoute("/")({
  component: IndexComponent,
});

function IndexComponent() {
  return (
    <div className="p-4 space-y-6">
      <div>
        <h2 className="text-2xl font-bold">trpcgo + React Query</h2>
        <p className="max-w-xl mt-1 text-gray-600">
          This example demonstrates every feature of the trpcgo runtime:
          queries, mutations, subscriptions, validation, middleware, error
          formatting, server-side callers, and more.
        </p>
        <p className="mt-2">
          <Link
            to="/users"
            className="py-1 px-3 bg-blue-500 text-white rounded-full text-sm"
          >
            Browse Users
          </Link>
        </p>
      </div>

      <HealthCheck />
      <ResetDemo />
      <LiveFeed />
    </div>
  );
}

/** VoidQuery demo — system.health has no input. */
function HealthCheck() {
  const { data, isLoading } = trpc.system.health.useQuery(undefined, {
    refetchInterval: 5000,
  });

  return (
    <div className="border rounded p-3 max-w-sm">
      <h3 className="font-semibold text-sm text-gray-500 mb-2">
        Server Health <span className="text-xs font-normal">(VoidQuery)</span>
      </h3>
      {isLoading ? (
        <Spinner />
      ) : data ? (
        <dl className="text-sm grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
          <dt className="text-gray-500">Status</dt>
          <dd>{data.ok ? "OK" : "Down"}</dd>
          <dt className="text-gray-500">Uptime</dt>
          <dd className="font-mono text-xs">{data.uptime}</dd>
          <dt className="text-gray-500">Users</dt>
          <dd>{data.userCount}</dd>
        </dl>
      ) : null}
    </div>
  );
}

/** VoidMutation demo — system.resetDemo has no input, uses Call internally. */
function ResetDemo() {
  const utils = trpc.useUtils();
  const reset = trpc.system.resetDemo.useMutation({
    onSuccess: () => {
      utils.user.listUsers.invalidate();
      utils.system.health.invalidate();
    },
  });

  return (
    <div className="border rounded p-3 max-w-sm">
      <h3 className="font-semibold text-sm text-gray-500 mb-2">
        Reset Demo{" "}
        <span className="text-xs font-normal">
          (VoidMutation + server-side Call)
        </span>
      </h3>
      <p className="text-xs text-gray-500 mb-2">
        Clears all users and re-seeds via <code>trpcgo.Call</code> (runs full
        middleware chain server-side).
      </p>
      <button
        onClick={() => reset.mutate()}
        disabled={reset.isPending}
        className="text-sm bg-red-500 text-white px-3 py-1 rounded disabled:opacity-50"
      >
        {reset.isPending ? "Resetting..." : "Reset Data"}
      </button>
      {reset.data && (
        <p className="text-xs text-green-600 mt-1">
          {reset.data.message} ({reset.data.userCount} users)
        </p>
      )}
      {reset.error && (
        <p className="text-xs text-red-600 mt-1">{reset.error.message}</p>
      )}
    </div>
  );
}

/** VoidSubscribe demo — user.onCreated streams new users via SSE. */
function LiveFeed() {
  const [events, setEvents] = useState<User[]>([]);

  trpc.user.onCreated.useSubscription(undefined, {
    onData: (user) => {
      setEvents((prev) => [user, ...prev].slice(0, 10));
    },
  });

  return (
    <div className="border rounded p-3 max-w-sm">
      <h3 className="font-semibold text-sm text-gray-500 mb-2">
        Live Feed{" "}
        <span className="text-xs font-normal">(VoidSubscribe / SSE)</span>
      </h3>
      {events.length === 0 ? (
        <p className="text-xs text-gray-400">
          Waiting for new users... Create one to see it here.
        </p>
      ) : (
        <ul className="space-y-1">
          {events.map((user, i) => (
            <li key={`${user.id}-${i}`} className="text-sm flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-green-400" />
              <span className="font-medium">{user.name}</span>
              <span className="text-gray-400 text-xs">{user.email}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
