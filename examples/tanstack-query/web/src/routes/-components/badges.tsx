import type { Role, Status } from "../../../gen/trpc";

const roleBadgeColors: Record<Role, string> = {
  admin: "bg-purple-100 text-purple-700",
  editor: "bg-blue-100 text-blue-700",
  viewer: "bg-gray-100 text-gray-600",
};

const statusBadgeColors: Record<Status, string> = {
  active: "bg-green-100 text-green-700",
  inactive: "bg-gray-100 text-gray-600",
  suspended: "bg-red-100 text-red-700",
};

const statusDotColors: Record<Status, string> = {
  active: "bg-green-400",
  inactive: "bg-gray-400",
  suspended: "bg-red-400",
};

export function RoleBadge({ role }: { role: Role }) {
  return (
    <span
      className={`text-xs px-1.5 py-0.5 rounded font-medium ${roleBadgeColors[role]}`}
    >
      {role}
    </span>
  );
}

export function StatusBadge({ status }: { status: Status }) {
  return (
    <span
      className={`text-xs px-2 py-1 rounded font-medium ${statusBadgeColors[status]}`}
    >
      {status}
    </span>
  );
}

export function StatusDot({ status }: { status: Status }) {
  return (
    <span className={`w-2 h-2 rounded-full ${statusDotColors[status]}`} />
  );
}
