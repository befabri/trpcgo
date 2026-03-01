import { createTRPCReact } from "@trpc/react-query";
import type { AppRouter } from "../gen/trpc.js";

export const trpc = createTRPCReact<AppRouter>();
