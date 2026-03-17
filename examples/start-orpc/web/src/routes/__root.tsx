/// <reference types="vite/client" />
import {
  HeadContent,
  Link,
  Outlet,
  Scripts,
  createRootRouteWithContext,
} from '@tanstack/react-router'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { TanStackRouterDevtools } from '@tanstack/react-router-devtools'
import type { RouterAppContext } from '~/lib/orpc'
import appCss from '~/styles/app.css?url'

export const Route = createRootRouteWithContext<RouterAppContext>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
    ],
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  component: RootComponent,
})

function RootComponent() {
  return (
    <html>
      <head>
        <HeadContent />
      </head>
      <body>
        <nav className="border-b">
          <div className="mx-auto max-w-5xl flex items-center gap-6 px-6 h-14">
            <span className="font-semibold tracking-tight">trpcgo</span>
            <div className="flex gap-4 text-sm">
              <Link
                to="/"
                activeOptions={{ exact: true }}
                className="text-gray-500 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
                activeProps={{ className: 'text-gray-900 dark:text-gray-100 font-medium' }}
              >
                Home
              </Link>
              <Link
                to="/users"
                className="text-gray-500 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
                activeProps={{ className: 'text-gray-900 dark:text-gray-100 font-medium' }}
              >
                Users
              </Link>
            </div>
          </div>
        </nav>
        <main className="mx-auto max-w-5xl px-6 py-8">
          <Outlet />
        </main>
        <TanStackRouterDevtools position="bottom-right" />
        <ReactQueryDevtools buttonPosition="bottom-left" />
        <Scripts />
      </body>
    </html>
  )
}
