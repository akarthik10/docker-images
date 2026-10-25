# keymaster — implementation notes

For an implementing agent. Read README.md first; its decisions are locked. If something here
conflicts with reality (an API, a library, a file path), verify it and note the deviation in this
file. Don't silently redesign.

## Where things go
- **Service code:** new dir `keymaster/` in the repo that builds `ghcr.io/akarthik10/claude-agent`
  (`docker-images`, on Ani's GitHub). Mirror its CI and image tag scheme (current tags look like
  `0.26218.145155`). Image: `ghcr.io/akarthik10/keymaster`. Confirm with Ani before creating a new
  repo instead.
- **Manifests:** `homelab-flux/cluster/apps/keymaster/`, added to `cluster/apps/kustomization.yaml`.
  Copy the patterns from:
  - `cluster/apps/claude-agent/helm-release.yaml` (bjw-s app-template 5.1.0)
  - `cluster/apps/pocket-id/ingress-route.yaml` (Traefik IngressRoute)
  - `cluster/apps/n8n/rbac.yaml` (TokenRequest RBAC scoped by resourceNames)
  - `cluster/apps/claude-agent/claude-agent-claude-md.sops.yaml` (per-app SOPS Secret)
- **Writes to homelab-flux** go through PRs. Secrets that need the age key, and Pocket ID/GitHub UI
  steps, are **Ani-only**. List them in the PR description and don't work around them.

## Stack
Go 1.23+, single static binary, distroless image, non-root.
- OIDC: `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2` (PKCE)
- MCP: `github.com/modelcontextprotocol/go-sdk`, streamable HTTP transport
- k8s: `k8s.io/client-go` (TokenReview, TokenRequest)
- DB: `modernc.org/sqlite` (pure Go)
- Proxy: `net/http/httputil.ReverseProxy`
- UI: `html/template`, server-rendered, no JS framework, mobile-first

## Listeners (strictly separated)
- `:8080` internal: `/mcp`, `/p/<connector>/...`, `/healthz`. No ingress.
- `:8081` public, via Traefik at `keymaster.${SECRET_PUBLIC_DOMAIN}`: `/approve/<id>`,
  `/oidc/callback`, `/grants` (list + revoke + revoke-all), static assets. No tinyauth middleware;
  OIDC is the auth.

## Configuration
- **Env:** `KM_PUBLIC_URL`, `KM_OIDC_ISSUER` (`https://id.${SECRET_PUBLIC_DOMAIN}`),
  `KM_OIDC_CLIENT_ID`, `KM_OIDC_CLIENT_SECRET`, `KM_OWNER_SUB` (Ani's Pocket ID subject),
  `KM_DB_PATH`, `KM_POLICY_PATH`, `KM_CREDS_DIR`.
- **Policy** is a ConfigMap mounted at `KM_POLICY_PATH`, stored in git. Shape is illustrative;
  names and values are not final:

```yaml
agents:
  - sa: default/claude-agent            # TokenReview username system:serviceaccount:default:claude-agent
    allowed_connectors: [copyparty, miniflux, k8s, github]
connectors:
  copyparty:
    kind: proxy
    upstream: http://copyparty.default.svc.cluster.local:3923
    auth: {type: basic, user_file: copyparty-user, pass_file: copyparty-pass}   # files under KM_CREDS_DIR
    untrusted_content: true
    scopes:
      notes-read:  {tier: 1, max_ttl: 72h, rules: [{methods: [GET, HEAD, PROPFIND], paths: ["^/Documents/Notes/ani(/.*)?$"]}]}
      notes-write: {tier: 2, max_ttl: 1h,  rules: [{methods: [GET, HEAD, PROPFIND, PUT, MKCOL], paths: ["^/Documents/Notes/ani(/.*)?$"]}]}
  miniflux:
    kind: proxy
    upstream: http://miniflux.default.svc.cluster.local:8080
    auth: {type: header, name: X-Auth-Token, value_file: miniflux-token}
    untrusted_content: true
    scopes:
      read: {tier: 1, max_ttl: 72h, rules: [{methods: [GET], paths: ["^/v1/.*"]}]}
  k8s:
    kind: mint
    minter: k8s_tokenrequest
    params: {namespace: default, serviceaccount: claude-agent-writer, audience: https://kubernetes.default.svc}
    scopes:
      write: {tier: 2, max_ttl: 10m}    # TokenRequest minimum is 600s; mint exactly the grant TTL, never below 600
  github:
    kind: mint
    minter: github_app
    params: {app_id_file: gh-app-id, installation_id_file: gh-installation-id, key_file: gh-app-key.pem}
    scopes:
      homelab-flux-write: {tier: 2, max_ttl: 1h, params: {repositories: [homelab-flux], permissions: {contents: write, pull_requests: write}}}
defaults: {proxy_ttl: 15m, pending_expiry: 10m, auth_max_age: 120s}
```

Validate the policy at startup and fail closed. A scope with no rules on a proxy connector is
invalid. Tier 3 means the scope is never grantable. Tier 0 is auto-approved and logged. Tier 1 can
be approved with "remember" (uses max_ttl). Tier 2 always needs the full passkey flow.

## Data model (SQLite)
- `grants(id, agent, status[pending|active|denied|expired|released|revoked], scopes_json,
  requested_ttl, granted_ttl, reason, created_at, approved_at, expires_at, approver_sub,
  token_hash, oidc_state_hash, mint_handles_json)`
- `audit(id, ts, grant_id, agent, event, connector, method, path, status, bytes, detail)`. Events:
  request, approve, deny, narrow, mint, revoke_upstream, proxy, expire, release, revoke,
  policy_load.

`token_hash` is SHA-256 of a 256-bit random grant token. The plaintext is returned to the agent
exactly once and never stored. A background loop runs every 15 seconds: expires grants and runs
upstream revocation for mint handles (GitHub `DELETE /installation/token`; k8s tokens can't be
revoked, so the short TTL is the control and that gets logged).

## MCP tools (endpoint `/mcp`, authed by the agent's SA token)
- `list_connectors()` → connectors, scopes, tiers and max TTLs allowed for this agent, plus the
  proxy base URL and short usage notes.
- `request_access(scopes[], reason, ttl)` → creates a pending grant. Tier 0 approves immediately.
  Otherwise it calls the notifier and waits up to 90s (stays below Claude Code's MCP tool timeout;
  check `MCP_TOOL_TIMEOUT`). Returns `{grant_id, status, approval_url}`; when active, also
  `{grant_token, expires_at, granted_scopes, minted: {...}}`.
- `await_grant(grant_id)` → same wait and return, for when the user is slow.
- `list_grants()` → this agent's active and pending grants (no tokens).
- `release(grant_id)` → ends the grant now and revokes upstream.

**Agent auth on `/mcp`:** `Authorization: Bearer <projected SA token>` with audience `keymaster`,
checked via TokenReview and mapped to `agents[].sa`. The token file rotates, so verify that Claude
Code's `headersHelper` MCP setting exists and use it to cat the file. If it doesn't exist, ship a
tiny stdio→HTTP shim in the claude-agent image that reads the file on every request.

## Proxy (`/p/<connector>/<path>`)
1. Require `Authorization: Bearer <grant_token>`, look up by hash, and check the grant is active,
   unexpired and belongs to the connector.
2. Match method + path (after stripping the `/p/<connector>` prefix, cleaned, no `..`) against the
   rules of the granted scopes. No match → 403 with a JSON error naming the scope needed.
3. Strip all incoming `Authorization`, `Cookie` and `Proxy-*` headers. Inject the connector auth.
4. Fixed upstream only. Don't follow redirects. Rewrite `Location` headers that point at the
   upstream to `/p/<connector>/...`, and drop any that point elsewhere.
5. Write audit metadata (never bodies). Stream bodies; don't buffer.

## Consent flow (`:8081`)
1. Open `/approve/<id>` and set a short-lived pre-auth cookie.
2. Redirect to Pocket ID with PKCE, `prompt=login`, `max_age=0`, and `state` bound to the grant id
   plus a nonce.
3. On the callback, verify the ID token, `sub == KM_OWNER_SUB` and
   `now - auth_time <= auth_max_age`. **Confirm Pocket ID puts `auth_time` in the ID token.** If it
   doesn't, record that and rely on `prompt=login`, which the Pocket ID tests confirm forces
   reauthentication.
4. Render the consent page:
   - the agent identity
   - the scopes (checkboxes, so Ani can narrow)
   - a TTL selector capped at max_ttl
   - the reason, shown in a box labelled "Agent's stated reason (unverified)"
   - currently active grants
   - a warning banner if a write scope would coexist with an active `untrusted_content` grant
5. Approve/Deny is a POST protected by CSRF and accepted only within 5 minutes of the login, once.
6. Mint on approval for mint connectors. The minted value is held only in memory until
   `request_access`/`await_grant` hands it over, and is dropped once delivered.

## Kubernetes manifests (`cluster/apps/keymaster/`)
- **HelmRelease** (app-template): two service ports, a PVC (`longhorn-persist`, 1Gi), the policy
  ConfigMap and the SOPS Secret mounted read-only at `KM_CREDS_DIR`. `automountServiceAccountToken:
  true`. Non-root and readOnlyRootFilesystem.
- **RBAC** for SA `keymaster`:
  - ClusterRoleBinding to `system:auth-delegator` (TokenReview).
  - Role in `default`: `create` on `serviceaccounts/token` with `resourceNames:
    [claude-agent-writer]`.
  - No secrets RBAC: credentials arrive as a volume.
- **SOPS Secret** `keymaster-credentials`: copyparty user/pass, miniflux token, GitHub App
  id/installation id/private key (new key, generated by Ani), OIDC client secret. Ani encrypts it.
- **IngressRoute** for `:8081` only.
- **NetworkPolicies:**
  - keymaster ingress: `:8080` from pods labelled as agents, `:8081` from the traefik namespace.
  - claude-agent egress: kube-dns, the kube-apiserver (k3s: the node IP, 6443), keymaster:8080,
    and `0.0.0.0/0` except the cluster pod and service CIDRs and the LAN `192.168.1.0/24`.
    Confirm with Ani whether LAN access (router at 192.168.1.1) must stay; some past sessions used
    it.
- **On the claude-agent HelmRelease:** add a projected SA token volume (audience `keymaster`,
  expiry 1h) and register the MCP server at startup (`claude mcp add --transport http ...`, or
  `.mcp.json` in `/home/node/ops-agent`).

## Phases and acceptance
Each phase is a PR (code PR + homelab-flux PR) with tests passing.

- **P0 scaffold:** binary, config/policy loading with validation, SQLite migrations, both
  listeners, healthz, Dockerfile, CI, manifests (no agent wiring yet).
  - ✅ Pod runs; a bad policy crashes it with a clear error.
- **P1 grants + consent + MCP:** all of the above, plus a built-in `echo` proxy connector
  (upstream = an internal handler) for testing.
  - ✅ The agent calls `request_access(echo:any)`, Ani opens the link on her phone, signs in with
    a passkey, and the tool returns active.
  - ✅ A second approval right away still asks for the passkey.
  - ✅ Expiry, release and revoke-all each make the proxy return 401 within 15 seconds.
- **P2 proxy connectors:** copyparty and miniflux.
  - ✅ Read scope can GET but gets 403 on PUT.
  - ✅ A path outside the rule gets 403.
  - ✅ An agent-supplied Authorization header never reaches upstream (verified with the echo
    connector).
  - ✅ The audit log shows every call.
- **P3 mint connectors:** k8s and GitHub.
  - ✅ The k8s token works with `kubectl --token` and expires.
  - ✅ The GitHub token can push a branch and open a PR on homelab-flux, and is dead (401) right
    after release or expiry.
  - Note: the GitHub App installation must include `homelab-flux`. That's a known open Ani action;
    see the JIT doc.
- **P4 cutover:**
  - Apply the agent egress NetworkPolicy.
  - Verify the agent can't reach `copyparty.default.svc` directly, but can through keymaster.
  - Remove `COPYPARTY_CLAUDE_PASSWORD` (and, later, mail/Actual) from the claude-agent env.
  - Update the claude-agent CLAUDE.md and the `cluster-ops` / `obsidian-notes` skills. The new
    rule: "hit a wall → `list_connectors` → `request_access`; release when done."
  - Retire the n8n JIT workflow, the `claude-agent-elevation` Secret and the
    `n8n-elevation-writer` RBAC, only after P3 has been verified in real use.

## Tests (required)
- Unit: policy validation, rule matching (including `..`, encoded slashes, case), grant state
  machine, TTL capping, header stripping, Location rewrite.
- Integration: a fake OIDC provider (`httptest`) issuing tokens with controlled `auth_time`/`sub`.
  Rejected cases: stale auth_time, wrong sub, replayed state, double approve.
- No test may need the real Pocket ID or cluster. Live checks are listed per phase above.

## Out of scope for now
- Per-app permission specifics, tier assignments, mail/Actual/Immich connectors.
- Notifier backends (keep the interface: `Notify(ctx, grant, approvalURL) error`; v1 is a no-op).
- Multiple human approvers, HTTPS_PROXY placeholder mode.

## Ani-only actions (collect in PR descriptions)
1. Create the Pocket ID OIDC client: redirect `https://keymaster.<domain>/oidc/callback`, PKCE,
   and record `KM_OWNER_SUB`.
2. Generate a new GitHub App private key, and add `homelab-flux` to the App installation.
3. SOPS-encrypt `keymaster-credentials`.
4. Merge PRs.
5. Decide whether the agent keeps LAN egress.

## Deviations (verified 2026-10-23)
- Service code lives in its own repo (`akarthik10/keymaster`), not in `docker-images`. CI there
  tests, then publishes `ghcr.io/akarthik10/keymaster` with the same `0.YYDDD.HHMMSS` tag scheme.
- Kubernetes calls (TokenReview, TokenRequest) use a ~60-line REST client (`internal/kube`), not
  `client-go`.
- MCP runs stateless (go-sdk `Stateless: true`): the agent's SA token is checked on every request
  and a server is built per request, bound to that agent. No `Mcp-Session-Id`.
- `echo` connector is `upstream: "internal:echo"` in the policy; it reflects what upstream would see.
- Consent sessions and OIDC state are in memory (single replica); a restart drops pending logins.
- Tier 1 "remember": the consent page offers a "Remember" checkbox when a tier 1 scope is present. It stores
  (agent, scope) in the `remembered` table for the scope's max_ttl. Later requests made up entirely of tier 0
  and remembered tier 1 scopes auto-approve (`auto:remembered`, TTL capped at the remaining consent). Any tier 2
  scope in the request still needs the passkey. Revoke and revoke-all forget remembered consent.
- Mint handles are persisted in `grants.mint_handles_json` (for GitHub that is the live installation token, on the
  PVC, until the grant ends) and `Recover()` re-arms or runs upstream revocation at startup.
- Not implemented: notifier backends. Consent sessions and OIDC state stay in memory.
- Dev: `KM_DEV_AGENT=ns/name` disables agent auth and skips the in-cluster k8s client. Never set it
  in the cluster.
- Env: `KM_PUBLIC_URL`, `KM_OIDC_ISSUER`, `KM_OIDC_CLIENT_ID`, `KM_OIDC_CLIENT_SECRET`, `KM_OWNER_SUB`
  are required. Optional: `KM_CREDS_DIR`, `KM_PROXY_URL`, `KM_WAIT`, `KM_POLICY_PATH`, `KM_DB_PATH`,
  `KM_INTERNAL_ADDR`, `KM_PUBLIC_ADDR`.
