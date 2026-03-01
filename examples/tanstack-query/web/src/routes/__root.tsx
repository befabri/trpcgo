import { Link, Outlet, createRootRoute } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";

export const Route = createRootRoute({
  component: RootComponent,
});

function RootComponent() {
  return (
    <>
      <div className="min-h-screen flex flex-col">
        <div className="flex items-center border-b gap-2">
          <h1 className="text-3xl p-2">trpcgo</h1>
          <span className="text-sm text-gray-500">
            React Query + Go tRPC backend
          </span>
        </div>
        <div className="flex-1 flex">
          <div className="divide-y w-56">
            {(
              [
                ["/", "Home"],
                ["/users", "Users"],
              ] as const
            ).map(([to, label]) => (
              <div key={to}>
                <Link
                  to={to}
                  preload="intent"
                  className="block py-2 px-3 text-blue-700"
                  activeProps={{ className: "font-bold" }}
                >
                  {label}
                </Link>
              </div>
            ))}
          </div>
          <div className="flex-1 border-l border-gray-200">
            <Outlet />
          </div>
        </div>
      </div>
      <TanStackRouterDevtools position="bottom-right" />
    </>
  );
}
