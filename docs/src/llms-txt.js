export function renderLlmsTxt(site) {
	const base = site instanceof URL ? new URL(site.href) : new URL(site);
	if (!base.pathname.endsWith('/')) base.pathname += '/';

	const href = (path) => new URL(path.replace(/^\//, ''), base).toString();

	return `# trpcgo

> Go runtime and code generator for serving tRPC-compatible APIs from Go and generating TypeScript and Zod contracts for tRPC v11 clients.

trpcgo uses Go handlers, structs, and struct tags as the source of truth. It registers typed queries, mutations, and SSE subscriptions on a plain \`net/http\` handler, speaks the tRPC HTTP protocol, and generates an \`AppRouter\` type for \`@trpc/client\`, \`@trpc/react-query\`, and \`@trpc/tanstack-react-query\`.

## Full Context

- [llms-full.txt](${href('/llms-full.txt')}): complete documentation as Markdown. Use this when you need exact runtime behavior, options, examples, or edge cases.

## Core Workflow

1. Create a \`trpcgo.Router\` with options such as \`WithStrictInput\`, \`WithValidator\`, \`WithTypeOutput\`, and \`WithZodOutput\`.
2. Register typed Go handlers with \`MustQuery\`, \`MustMutation\`, or subscription helpers.
3. Mount \`trpc.NewHandler(router, "/trpc")\` behind \`net/http\` or another Go HTTP stack.
4. Generate TypeScript with \`go tool trpcgo generate\`, \`go generate\`, or the router dev watcher.
5. Import the generated \`AppRouter\` type in the frontend and call procedures with tRPC v11 clients.

## Essential Docs

- [Overview](${href('/')}): what trpcgo is for and the basic server/client shape.
- [Core Concepts](${href('/concepts/')}): router, procedures, HTTP handler, generated contracts, and validation model.
- [Install](${href('/install/')}): Go module setup, generator setup, frontend packages, and requirements.
- [Quick Start](${href('/quick-start/')}): build one endpoint, generate TypeScript and Zod, and call it from a client.

## Runtime Docs

- [Procedures](${href('/procedures/')}): queries, mutations, subscriptions, void procedures, metadata, middleware, and base procedures.
- [Router & Options](${href('/router-options/')}): batching, strict input decoding, body limits, validators, dev generation, and hooks.
- [HTTP Protocol](${href('/http-protocol/')}): request methods, input encoding, batching, JSONL streaming, SSE, and error envelopes.
- [Middleware & Metadata](${href('/middleware/')}): global middleware, per-procedure middleware, typed metadata, and context derivation.
- [Subscriptions](${href('/subscriptions/')}): SSE subscription handlers, lifecycle, limits, and client links.
- [Response Metadata](${href('/response-metadata/')}): setting headers, status codes, cookies, and cache controls from handlers.
- [Errors](${href('/errors/')}): tRPC-compatible error codes, sanitization, dev stacks, custom formatting, and logging hooks.

## Type Generation Docs

- [Code Generation](${href('/code-generation/')}): static analysis, CLI flags, runtime generation, dev watch mode, and generated exports.
- [Frontend Setup](${href('/frontend-setup/')}): vanilla tRPC client, React Query, TanStack helpers, batching, subscriptions, and credentials.
- [Struct Tags](${href('/struct-tags/')}): \`json\`, \`tstype\`, optional fields, readonly fields, aliases, enums, and comments.
- [Zod Schemas](${href('/zod-schemas/')}): generated schemas from Go \`validate\` tags, \`zod\` vs \`zod/mini\`, and client validation.

## Operations And Reference

- [Security & Production](${href('/security-production/')}): strict decoding, body limits, CORS, auth, rate limits, SSE limits, and safe errors.
- [CLI Reference](${href('/reference/cli/')}): \`trpcgo generate\` flags, examples, package patterns, watch mode, and exit behavior.
- [Compatibility](${href('/reference/compatibility/')}): tRPC v11 compatibility, supported transports, Go type support, and known limits.

## Notes For LLMs

- Go handlers and Go types are the source of truth. The frontend should consume generated TypeScript and Zod output instead of hand-written router contracts.
- Server-side validation only runs when a validator is configured with \`WithValidator\`. Zod generation can still use Go \`validate\` tags for client-side schemas.
- Prefer \`go generate ./...\` or \`go tool trpcgo generate\` in production. \`WithDev(true)\` and router output paths are for development workflows.
- trpcgo provides the tRPC protocol handler, optional CORS handling, typing, generation, and validation hooks. Auth, persistence, and framework integration remain application concerns.
`;
}
