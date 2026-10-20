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
                     PostgreSQL     Redis (future — not deployed)
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
  3. No secrets are required: the website is static and talks to the game
     server over its public address.

## Backend — container

- Image definition: `infra/docker/Dockerfile.server` (multi-stage,
  non-root, healthcheck on `/health`, STOPSIGNAL SIGTERM for graceful
  shutdown).
- Build:

  ```sh
  docker build -f infra/docker/Dockerfile.server \
    --build-arg VERSION="$(cat VERSION)" \
    --build-arg COMMIT="$(git rev-parse --short HEAD)" \
    -t highjack-server .
  ```

  The version comes from the repository's `VERSION` file, which is the single
  source of truth: the workspace manifests, the game client's build, the
  server binary's `/version` endpoint, and the release tag are all checked
  against it.

- Run:

  ```sh
  docker run -p 8080:8080 \
    -e HIGHJACK_ENV=production \
    -e HIGHJACK_DATABASE_URL=postgres://… \
    -e HIGHJACK_ALLOWED_ORIGINS=https://your-site.example \
    highjack-server
  ```

  `HIGHJACK_ALLOWED_ORIGINS` is not optional in production. Outside production
  the origin check is relaxed; in production an unset allow-list means no
  browser origin is admitted at all, and the server says so in its log at
  startup.

- The image carries `/migrations`; the binary applies pending migrations at
  boot when `HIGHJACK_DATABASE_URL` is set.

## One instance, on purpose

Run a single replica. The runtime marks every match left active by a previous
process as `interrupted` at boot, and that statement has **no instance or lease
scoping**: with two instances sharing one database, one instance's boot would
mark the other instance's live matches interrupted.

That is a known limitation rather than a supported configuration, and it is
tracked as RSK-1 in `docs/audit/V0_2_5_PATCHWORK.md`. The same document
records PRS-3, the per-match lock held across the database commit, which is
correct for one instance and bounded at five seconds.

Until both are resolved, scale the backend by giving each instance its own
database, or by putting a single instance behind a load balancer.

## PostgreSQL

Any managed Postgres works. Requirements: reachable DSN, TLS preferred
(`sslmode=require`). Schema is owned by `server/migrations/*.sql`, and the
migration runner refuses a migration numbered below one that has already been
applied — a late addition that would never run is an error, not a silent skip.

## Not deployed

Redis, Kubernetes, service mesh, separate realtime cluster. The server is a
single stateful-ish process by design until gameplay requires more; premature
infrastructure is an explicit non-goal (see directive §37).
