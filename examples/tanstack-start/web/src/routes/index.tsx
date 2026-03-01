import { Link, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: IndexComponent,
});

function IndexComponent() {
  return (
    <div className="p-4 space-y-4">
      <h2 className="text-2xl font-bold">trpcgo Example</h2>
      <p className="max-w-xl">
        This is a TanStack Router app talking to a <strong>Go backend</strong>{" "}
        via the tRPC HTTP wire protocol. The TypeScript types are generated from
        TypeSpec by <code>@trpcgo/js-client-emitter</code>, and the Go server
        uses the <code>trpcgo</code> runtime library.
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
