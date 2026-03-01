import { useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { trpc } from "../trpc";

export const Route = createFileRoute("/users_/create")({
  component: CreateUserComponent,
});

function CreateUserComponent() {
  const navigate = useNavigate();
  const utils = trpc.useUtils();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");

  const createUser = trpc.user.createUser.useMutation({
    onSuccess: (user) => {
      // Invalidate the users list so it refetches automatically.
      utils.user.listUsers.invalidate();
      navigate({
        to: "/users/$userId",
        params: { userId: user.id },
      });
    },
  });

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    createUser.mutate({ name, email });
  }

  return (
    <div className="p-4 max-w-md space-y-4">
      <h3 className="text-xl font-bold">Create User</h3>
      <form onSubmit={handleSubmit} className="space-y-3">
        <div>
          <label className="block text-sm font-medium mb-1">Name</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            className="w-full border rounded-sm p-2"
          />
        </div>
        <div>
          <label className="block text-sm font-medium mb-1">Email</label>
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            className="w-full border rounded-sm p-2"
          />
        </div>
        {createUser.error && (
          <p className="text-red-600 text-sm">{createUser.error.message}</p>
        )}
        <button
          type="submit"
          disabled={createUser.isPending}
          className="bg-blue-500 text-white px-4 py-2 rounded-sm disabled:opacity-50"
        >
          {createUser.isPending ? "Creating..." : "Create User"}
        </button>
      </form>
    </div>
  );
}
