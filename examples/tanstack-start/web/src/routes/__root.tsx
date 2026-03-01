import {
  Link,
  Outlet,
  createRootRouteWithContext,
  useRouterState,
} from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";
import { Spinner } from "./-components/spinner";
import type { trpc } from "../client";

export interface RouterAppContext {
  trpc: typeof trpc;
}

export const Route = createRootRouteWithContext<RouterAppContext>()({
  component: RootComponent,
});

function RootComponent() {
  const isFetching = useRouterState({ select: (s) => s.isLoading });

  return (
    <>
      <div className="min-h-screen flex flex-col">
        <div className="flex items-center border-b gap-2">
          <h1 className="text-3xl p-2">trpcgo</h1>
          <span className="text-sm text-gray-500">
            TanStack Router + Go tRPC backend
          </span>
          <div
            className={`text-3xl duration-300 delay-0 opacity-0 ${isFetching ? "duration-1000 opacity-40" : ""}`}
          >
            <Spinner />
          </div>
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
