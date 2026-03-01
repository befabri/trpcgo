import { createFileRoute } from "@tanstack/react-router";
import { trpc } from "../trpc";
import { Spinner } from "./-components/spinner";

export const Route = createFileRoute("/users/$userId")({
  component: UserDetailComponent,
});

function UserDetailComponent() {
  const { userId } = Route.useParams();
  const { data: user, isLoading } = trpc.user.getUserById.useQuery({
    id: userId,
  });

  if (isLoading || !user) {
    return (
      <div className="p-4">
        <Spinner /> Loading user...
      </div>
    );
  }

  console.log("user", user.password)

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
