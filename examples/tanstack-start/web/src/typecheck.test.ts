// Type-level tests for the generated AppRouter.
// This file is only type-checked (`tsc --noEmit`), never executed at runtime.
// If it compiles, the generated types are compatible with @trpc/client.

import { createTRPCClient, httpBatchLink } from "@trpc/client";
import type { AppRouter, User, GetUserByIdInput, CreateUserInput } from "../gen/trpc.js";

// 1. createTRPCClient accepts AppRouter as a type parameter
const trpc = createTRPCClient<AppRouter>({
  links: [httpBatchLink({ url: "http://localhost:8080/trpc" })],
});

// 2. Query with typed input returns typed output
async function testGetUserById() {
  const user = await trpc.user.getUserById.query({ id: "1" });

  // Output is User
  const _id: string = user.id;
  const _name: string = user.name;
  const _email: string = user.email;

  // @ts-expect-error — input requires `id` field
  await trpc.user.getUserById.query({});

  // @ts-expect-error — `id` must be string, not number
  await trpc.user.getUserById.query({ id: 123 });
}

// 3. Query with void input returns typed array output
async function testListUsers() {
  const users = await trpc.user.listUsers.query();

  // Output is User[]
  const _first: User = users[0];
  const _len: number = users.length;
}

// 4. Mutation with typed input returns typed output
async function testCreateUser() {
  const user = await trpc.user.createUser.mutate({
    name: "Charlie",
    email: "charlie@example.com",
  });

  // Output is User
  const _id: string = user.id;

  // @ts-expect-error — input requires `name` and `email`
  await trpc.user.createUser.mutate({});

  // @ts-expect-error — `name` must be string
  await trpc.user.createUser.mutate({ name: 123, email: "x" });
}

// 5. Namespace access is typed — invalid procedure names fail
async function testInvalidProcedure() {
  // @ts-expect-error — no such procedure
  await trpc.user.nonExistent.query();

  // @ts-expect-error — no such namespace
  await trpc.admin.something.query();
}

// 6. Cannot call query as mutation or vice versa
async function testMethodMismatch() {
  // @ts-expect-error — getUserById is a query, not a mutation
  await trpc.user.getUserById.mutate({ id: "1" });

  // @ts-expect-error — createUser is a mutation, not a query
  await trpc.user.createUser.query({ name: "X", email: "x@x.com" });
}

// Prevent unused variable warnings
void testGetUserById;
void testListUsers;
void testCreateUser;
void testInvalidProcedure;
void testMethodMismatch;
