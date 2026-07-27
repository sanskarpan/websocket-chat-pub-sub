# Architecture Decision Records

ADRs capture the non-obvious design choices made during the development of ws-chat — the context, the decision, the tradeoffs, and the consequences. They are immutable historical records; corrections are made by superseding ADRs, not by editing old ones.

---

## Index

| ADR | Status | Decision |
|---|---|---|
| [0001 — JWT Auth & RS256](0001-jwt-authentication-and-authorization.md) | Accepted | RS256 asymmetric signing; `jti` on all token types; blacklist enforced at validation time |
| [0002 — Redis Pub/Sub & Dedup](0002-redis-pubsub-and-presence.md) | Accepted | Redis Pub/Sub for cross-node fanout; `GetDedup`/`SetDedup` for distributed deduplication |
| [0003 — Graceful Shutdown](0003-graceful-shutdown-and-connection-draining.md) | Accepted | Ordered shutdown: WS Hub → REST API → Metrics → Redis → PostgreSQL |
| [0004 — Token Blacklisting & Logout](0004-token-blacklisting-and-logout.md) | Accepted | `POST /auth/logout` blacklists both access and refresh JTIs; `validateToken` enforces on every call |

---

## Format

Each ADR follows this structure:

- **Status** — `Proposed` | `Accepted` | `Deprecated` | `Superseded by NNNN`
- **Context** — the problem and forces at play
- **Decision** — what was decided and why
- **Consequences** — positive and negative outcomes
- **Alternatives considered** — what was ruled out and why
