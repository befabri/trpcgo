import { createTRPCClient, httpBatchLink } from "@trpc/client";
import type { AppRouter } from "../gen/trpc.js";

export const trpc = createTRPCClient<AppRouter>({
  links: [
    httpBatchLink({
      url: "http://localhost:8080/trpc",
    }),
  ],
});
