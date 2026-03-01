import { createFileRoute } from "@tanstack/react-router";
import { trpc } from "../trpc";
import { Spinner } from "./-components/spinner";
import { RoleBadge, StatusBadge } from "./-components/badges";

export const Route = createFileRoute("/users/$userId")({
  component: UserDetailComponent,
});

function UserDetailComponent() {
  const { userId } = Route.useParams();
  const { data: user, isLoading, error } = trpc.user.getUserById.useQuery({
    id: userId,
  });

  if (isLoading) {
    return (
      <div className="p-4">
        <Spinner /> Loading user...
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 text-red-600">
        {error.message}
      </div>
    );
  }

  if (!user) {
    return null;
  }

  return (
    <div className="p-4 space-y-4">
      <div className="flex items-center gap-3">
        <img
          src={user.avatarUrl}
          alt={user.name}
          className="w-12 h-12 rounded-full bg-gray-100"
        />
        <div>
          <h3 className="text-xl font-bold">{user.name}</h3>
          <p className="text-sm text-gray-500">{user.email}</p>
        </div>
      </div>

      <div className="flex gap-2">
        <RoleBadge role={user.role} />
        <StatusBadge status={user.status} />
      </div>

      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        <dt className="font-semibold text-gray-500">ID</dt>
        <dd className="font-mono text-xs">{user.id}</dd>

        {user.bio && (
          <>
            <dt className="font-semibold text-gray-500">Bio</dt>
            <dd>{user.bio}</dd>
          </>
        )}

        <dt className="font-semibold text-gray-500">Created</dt>
        <dd>{new Date(user.createdAt).toLocaleDateString()}</dd>

        <dt className="font-semibold text-gray-500">Updated</dt>
        <dd>{new Date(user.updatedAt).toLocaleDateString()}</dd>
      </dl>

      {Object.keys(user.tags).length > 0 && (
        <div>
          <h4 className="text-sm font-semibold text-gray-500 mb-1">Tags</h4>
          <div className="flex flex-wrap gap-1">
            {Object.entries(user.tags).map(([key, value]) => (
              <span
                key={key}
                className="text-xs bg-gray-100 text-gray-700 px-2 py-0.5 rounded"
              >
                {key}: {value}
              </span>
            ))}
          </div>
        </div>
      )}

      <div>
        <h4 className="text-sm font-semibold text-gray-500 mb-1">
          Preferences
        </h4>
        <pre className="text-xs bg-gray-50 p-2 rounded overflow-auto">
          {JSON.stringify(user.preferences, null, 2)}
        </pre>
      </div>

      {user.extraData != null && (
        <div>
          <h4 className="text-sm font-semibold text-gray-500 mb-1">
            Extra Data
          </h4>
          <pre className="text-xs bg-gray-50 p-2 rounded overflow-auto">
            {JSON.stringify(user.extraData, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}
