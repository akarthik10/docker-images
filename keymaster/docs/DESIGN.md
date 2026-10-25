# Agent access broker ("keymaster") — design (LOCKED 2026-10-23)

Supersedes the n8n JIT minter + `claude-agent-elevation` drop box described in
`user-projects/jit-access-expansion/README.md`. That doc stays as history.

## Problem
The `claude-agent` pod (claude remote-control, steered from Ani's phone) keeps hitting permission
walls. Current state:
- Standing read creds in env (k8s RO SA, Grafana RO, GitHub RO): fine.
- Standing app passwords in env (mail, Actual, copyparty): too broad, and they never expire.
- JIT write: iOS Shortcut → n8n webhook → token written to a Secret. Problems: the approval is a
  shared header secret, not user auth. It's one coarse button. n8n's credential store broke
  silently when its encryption key changed. And apps with only long-lived passwords can't be
  safely "minted" at all.

Goal: a general, OAuth-like consent model. The agent asks for scoped, time-boxed access, Ani
approves on any device with a passkey, and access is revocable and audited. It has to grow to
many apps and many agents.

## Prior art checked (2026-10-23)
Teleport OSS (no approval UI or plugins in OSS), Pomerium (policy, no approval flow), agentgateway,
mcp-context-forge, Docker MCP gateway, ToolHive (no approvals), HumanLayer (approvals, no passkey,
no proxy, stale), OpenBao (k8s token minting only), Infisical Agent Vault (credential-injecting
proxy, no approval flow), Octelium, Warpgate. **None covers >=70%.** Composing them (Pomerium or
Agent Vault + OpenBao + a custom approval service) is more moving parts than one small service,
and the approval/consent piece, which is the core of this, would still be custom. Decision:
build it.
Idea borrowed for later: Agent Vault's HTTPS_PROXY placeholder-swap mode, for third-party
internet APIs.

## Architecture
```
agent ──MCP: request_access(scopes, reason, ttl)──► keymaster ──► pending grant + approval URL
  │                                                    │  (notifier: v1 = URL returned to agent,
  │◄── URL shown in chat ──────────────────────────────┘   shown in chat; pluggable later)
Ani taps ─► consent page ─► Pocket ID (prompt=login, max_age=0) ─► passkey ─► approve / narrow / deny
agent ──HTTP keymaster:8080/p/<connector>/... + grant token ─► policy check ─► inject creds ─► app
agent ◄── for "mint" connectors: native short-lived token (k8s TokenRequest, GitHub App token)
```

## Locked decisions
1. **One service, keymaster.** It owns grants, consent, audit, the credential-injecting proxy and
   token minting. Its credentials come from a SOPS-encrypted k8s Secret. Its state lives in SQLite
   on a PVC. n8n is removed from this path.
2. **Two connector kinds:**
   - `proxy` (default): the agent never sees the app credential. keymaster checks the grant on
     every request, enforces method/path rules, injects credentials and logs the call. Revoking
     takes effect on the next request.
   - `mint`: only for systems with native short-lived scoped tokens where native tooling is
     needed (k8s TokenRequest for kubectl, GitHub App token for git). The token is returned in the
     MCP response, and keymaster revokes it actively when the grant ends, where the API allows.
   - Non-HTTP apps (IMAP) get a small HTTP adapter connector later. Same model.
3. **Consent is OAuth-style with step-up.** Every approval needs a fresh Pocket ID login
   (`prompt=login` + `max_age=0`, verified supported in the Pocket ID source). keymaster checks
   that `auth_time` is fresh and that `sub` is the owner. Ani can narrow scopes or shorten the TTL
   before approving. tinyauth is not used for approvals: its long-lived session can't serve as
   step-up. **Passkeys are Pocket ID's job.** keymaster never implements WebAuthn or stores
   authenticators itself. It only trusts a fresh Pocket ID login, so passkey support (phone
   keychain, password manager, security key) comes from Pocket ID.
4. **Scopes** look like `<connector>:<scope>`. They're defined per connector in a policy file kept
   in git, with rules, a max TTL and a tier. Tiers (exact per-app assignment comes later):
   - 0: standing, logged.
   - 1: remembered consent, lasting days.
   - 2: JIT with a passkey every time.
   - 3: never grantable.
   Changing the policy requires a homelab-flux PR. keymaster can't change its own policy.
5. **Agents authenticate with their k8s SA token.** It's a projected token with audience
   `keymaster`, checked via TokenReview. Grants belong to an agent identity, so adding more agent
   pods later is just more SAs plus policy.
6. **Lockdown is part of the design, not an option:**
   - The agent pod's egress is limited to keymaster:8080, the kube-apiserver, DNS and the
     internet. No direct pod-to-pod access to apps (many trust tinyauth at the ingress and are open
     on ClusterIP).
   - App credentials leave the agent's env.
7. **The MCP surface is control plane only:** `list_connectors`, `request_access`, `await_grant`,
   `list_grants`, `release`. Data goes over plain HTTP through the proxy, so the agent can still
   improvise inside a grant (same policy as before: safety comes from scope + TTL + approval, not
   from a command allowlist).
8. **Notifier is pluggable.** v1 is a no-op: the URL is returned to the agent and shown in chat.
   Slack, ntfy and push are left open.

## Hardening principles (contain prompt-injection blast radius)
- Short TTLs by default (proxy 15m; k8s 10m, the API's minimum; GitHub revoked when the grant ends).
  The agent releases grants when its task is done.
- The approval page treats the agent's `reason` as untrusted text: escaped and labelled as the
  agent's claim. It shows the currently active grants and warns when a write grant would coexist
  with a read grant on an `untrusted_content` connector (feeds, mail, notes).
- Pending requests expire after 10 minutes. Approvals are single-use and bound to the grant via
  OIDC state.
- The proxy strips the agent's auth headers and cookies, uses a fixed upstream per connector,
  follows no cross-host redirects and logs metadata only (no bodies).
- Revoke-all kill switch in the UI. Full audit log.

## Rollout
1. Scaffold.
2. Grants, consent and MCP.
3. Proxy connectors: copyparty, miniflux.
4. Mint connectors: k8s-writer, GitHub.
5. Cutover: agent NetworkPolicy, drop env creds, retire the n8n path.

Mail, Actual and others come after. See IMPLEMENTATION.md.
