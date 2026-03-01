import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/users/")({
  component: UsersIndexComponent,
});

function UsersIndexComponent() {
  return (
    <div className="p-4">
      <p>Select a user from the list, or create a new one.</p>
    </div>
  );
}
