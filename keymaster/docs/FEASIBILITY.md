# Feasibility and language choice

Date: 2026-10-23

## Verdict
Feasible. It is one small service with no hard technical unknowns. The bulk is plain HTTP, SQLite and
a state machine. Estimated size is about 2.5-3.5k lines of Go (excluding tests), or roughly the same
in Node/TS plus a heavier dependency tree.

## Rough size by component (Go)
| Component | LOC |
|---|---|
| policy load + validation | 250 |
| SQLite store, migrations, grants, audit | 400 |
| agent auth (TokenReview) | 100 |
| MCP server (5 tools) | 250 |
| reverse proxy + rule matching + header strip | 350 |
| consent flow (OIDC, CSRF, templates) | 500 |
| mint: k8s TokenRequest, GitHub App | 300 |
| reaper loop, config, main | 200 |
| **Total** | **~2.4k, ~3k with polish** |

## Language comparison
| | Go | Node/TS |
|---|---|---|
| Streaming reverse proxy | stdlib `httputil.ReverseProxy`; Rewrite hook, no redirect following by default | needs `http-proxy` or hand-rolled; more edge cases (hop-by-hop, backpressure) |
| k8s TokenReview/TokenRequest | `client-go`, first class | `@kubernetes/client-node` works, less used |
| OIDC + PKCE + id_token checks | `go-oidc` + `oauth2`, small and audited | `openid-client`, good too |
| MCP | official `go-sdk` (streamable HTTP) | official TS SDK, most mature |
| SQLite | `modernc.org/sqlite`, pure Go, no cgo | `better-sqlite3` native build, or `node:sqlite` |
| Image | static binary, distroless, ~20-30 MB, no shell | node runtime + node_modules, 150 MB+ |
| Supply chain | ~10 direct deps; stdlib covers HTTP, crypto, templates | hundreds of transitive npm deps for a service whose job is guarding credentials |
| Memory safety / typing | compiled, typed, `html/template` auto-escapes (the agent's `reason` is untrusted) | TS types vanish at runtime; template escaping depends on the chosen lib |
| Concurrency (reaper, long-poll waits, streaming) | goroutines, `context` | async works, but the single thread makes a blocked loop affect everything |
| Maintainability | boring, one binary, stable stdlib | faster to iterate, but dependency churn |

## Recommendation: Go
For a credential-holding broker the deciding factors are the small dependency surface, the stdlib
proxy and `html/template` escaping, and a static distroless image. Node would be marginally faster to
write for the MCP and UI pieces, but loses on supply chain, image size and proxy robustness. Python
was not considered: the proxy and k8s side are weaker. Rust would be safest but costs more LOC and
time for no real gain at this scale.

Use the stdlib wherever possible: `net/http` (Go 1.22+ pattern routing), `log/slog`, `html/template`,
`crypto/*`. Only add: `go-oidc`, `oauth2`, MCP `go-sdk`, `client-go` (or a thin REST client, see
below), `modernc.org/sqlite`, `yaml`.

## Risks and things to verify first
1. **`client-go` weight.** It is large (binary +30 MB, many transitive deps). Only two calls are
   needed (TokenReview, TokenRequest). Consider calling the API server over REST with the in-pod SA
   token and the CA file instead. About 80 lines, and it removes the biggest dependency.
2. **MCP go-sdk maturity and `headersHelper`.** Verify the streamable HTTP server API and that Claude
   Code supports `headersHelper` for rotating bearer tokens. The fallback is a stdio shim.
3. **Pocket ID `auth_time`.** Confirm it appears in the ID token. Fallback is `prompt=login`.
4. **MCP tool timeout vs the 90s wait.** Check `MCP_TOOL_TIMEOUT`. Returning `pending` plus
   `await_grant` already handles it.
5. **Proxy and WebDAV (PROPFIND, MKCOL, Destination header).** `COPY`/`MOVE` carry a `Destination`
   header that can point outside the allowed path. Rule matching must check it too, or deny those
   methods. Add this to the policy tests.
6. **Path normalisation.** Match rules against the exact path the upstream will see (decode once,
   reject `%2f`, `..`, and double encoding) to avoid rule bypass.
7. **Grant token in the agent context.** Proxy tokens are bearer secrets in the agent's context, but
   they are scoped and short-lived, which is the point of the design.
8. **SQLite on Longhorn PVC.** Fine with a single replica; use WAL off or `busy_timeout`, and don't
   run two replicas.

## Suggested layout
```
cmd/keymaster/main.go
internal/policy/      load, validate, rule matching
internal/store/       sqlite, migrations, grants, audit
internal/agentauth/   TokenReview
internal/mcpsrv/      tools
internal/proxy/
internal/consent/     OIDC, templates, CSRF
internal/mint/        k8s, github
internal/reaper/
Dockerfile, .github/workflows/
```

## Build order
Follow IMPLEMENTATION.md P0-P4. The riskiest parts to spike first (about half a day each): the MCP
server with rotating-token auth, and the OIDC step-up with a fake provider in tests.
