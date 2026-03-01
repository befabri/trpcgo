import { useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { trpc } from "../client";

export const Route = createFileRoute("/users_/create")({
  component: CreateUserComponent,
});

function CreateUserComponent() {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setSubmitting(true);

    try {
      const user = await trpc.user.createUser.mutate({ name, email });
      console.log(user.description)
      navigate({
        to: "/users/$userId",
        params: { userId: user.id },
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create user");
      setSubmitting(false);
    }
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
        {error && <p className="text-red-600 text-sm">{error}</p>}
        <button
          type="submit"
          disabled={submitting}
          className="bg-blue-500 text-white px-4 py-2 rounded-sm disabled:opacity-50"
        >
          {submitting ? "Creating..." : "Create User"}
        </button>
      </form>
    </div>
  );
}
