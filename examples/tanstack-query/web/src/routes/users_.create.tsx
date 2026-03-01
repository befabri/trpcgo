import { useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { trpc } from "../trpc";
import type { Role } from "../../gen/trpc";
import { CreateUserInputSchema } from "../../gen/zod";

export const Route = createFileRoute("/users_/create")({
  component: CreateUserComponent,
});

function CreateUserComponent() {
  const navigate = useNavigate();
  const utils = trpc.useUtils();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [bio, setBio] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const createUser = trpc.user.createUser.useMutation({
    onSuccess: (user) => {
      utils.user.listUsers.invalidate();
      navigate({
        to: "/users/$userId",
        params: { userId: user.id },
      });
    },
  });

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFieldErrors({});

    const input = { name, email, role, bio: bio || undefined };
    const result = CreateUserInputSchema.safeParse(input);
    if (!result.success) {
      const errors: Record<string, string> = {};
      for (const issue of result.error.issues) {
        const key = issue.path[0];
        if (typeof key === "string" && !errors[key]) {
          errors[key] = issue.message;
        }
      }
      setFieldErrors(errors);
      return;
    }

    createUser.mutate(result.data);
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
            className="w-full border rounded-sm p-2"
          />
          {fieldErrors.name && (
            <p className="text-red-600 text-xs mt-1">{fieldErrors.name}</p>
          )}
        </div>
        <div>
          <label className="block text-sm font-medium mb-1">Email</label>
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full border rounded-sm p-2"
          />
          {fieldErrors.email && (
            <p className="text-red-600 text-xs mt-1">{fieldErrors.email}</p>
          )}
        </div>
        <div>
          <label className="block text-sm font-medium mb-1">Role</label>
          <select
            value={role}
            onChange={(e) => setRole(e.target.value as Role)}
            className="w-full border rounded-sm p-2"
          >
            <option value="viewer">Viewer</option>
            <option value="editor">Editor</option>
            <option value="admin">Admin</option>
          </select>
        </div>
        <div>
          <label className="block text-sm font-medium mb-1">
            Bio (optional)
          </label>
          <textarea
            value={bio}
            onChange={(e) => setBio(e.target.value)}
            rows={3}
            className="w-full border rounded-sm p-2"
            placeholder="Tell us about yourself..."
          />
          {fieldErrors.bio && (
            <p className="text-red-600 text-xs mt-1">{fieldErrors.bio}</p>
          )}
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
