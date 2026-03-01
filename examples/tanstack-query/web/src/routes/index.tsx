import { Link, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: IndexComponent,
});

function IndexComponent() {
  return (
    <div className="p-4 space-y-4">
      <h2 className="text-2xl font-bold">trpcgo + React Query</h2>
      <p className="max-w-xl">
        This example uses <code>@trpc/react-query</code> with{" "}
        <code>@tanstack/react-query</code> for automatic caching, refetching,
        and React hooks. The Go backend generates TypeScript types automatically.
      </p>
      <p>
        <Link
          to="/users"
          className="py-1 px-3 bg-blue-500 text-white rounded-full text-sm"
        >
          Browse Users
        </Link>
      </p>
    </div>
  );
}
