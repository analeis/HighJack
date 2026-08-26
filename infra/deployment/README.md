# Deployment

## Target architecture

```
                    Cloudflare
                         │
                 ┌───────┴───────┐
                 │               │
              Website          API/WS
            (Pages, static)        │
                              Go server
                            (container)
                                  │
                          ┌───────┴───────┐
                          │               │
                     PostgreSQL     Redis (future — not deployed in v0.1)
```

Website and backend deploy and scale independently.

## Website — Cloudflare Pages

- Configuration: `infra/cloudflare/wrangler.toml`
- Build: `bun run --filter='@highjack/web' build` from the repository root
  (workspace resolution requires the root as build root).
- Output: `apps/web/dist` — fully static; no server-side runtime needed.
- Setup:
  1. Create a Pages project connected to this repository, or use
     `bunx wrangler pages deploy apps/web/dist`.
  2. Set production domain + custom domains in the dashboard.
  3. No secrets required for v0.1 (no server integration yet).

## Backend — container

- Image definition: `infra/docker/Dockerfile.server` (multi-stage,
  non-root, healthcheck on `/health`, STOPSIGNAL SIGTERM for graceful
  shutdown).
- Build:

  ```sh
  docker build -f infra/docker/Dockerfile.server \
    --build-arg VERSION=$(cat package.json | grep '"version"' | head -1) \
    -t highjack-server .
  ```

- Run:

  ```sh
  docker run -p 8080:8080 \
    -e HIGHJACK_ENV=production \
    -e HIGHJACK_DATABASE_URL=postgres://… \
    highjack-server
  ```

- The image carries `/migrations`; the binary applies pending migrations at
  boot when `HIGHJACK_DATABASE_URL` is set. Run a single replica first or
  coordinate migrations externally once multiple replicas exist (v0.1
  assumes one instance).

## PostgreSQL

Any managed Postgres works. Requirements: reachable DSN, TLS preferred
(`sslmode=require`). Schema is owned by `server/migrations/*.sql`.

## Not deployed in v0.1

Redis, Kubernetes, service mesh, separate realtime cluster. The server is
a single stateful-ish process by design until gameplay requires more;
premature infrastructure is an explicit non-goal (see directive §37).
