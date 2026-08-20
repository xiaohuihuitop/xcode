# XCode

XCode is a self-hosted AI API gateway and subscription distribution platform. It combines platform account pools, API-key authorization, subscriptions, balance billing, usage records, and an adapter boundary for upstream runtimes.

## What it provides

- Platform-owned model allowlists, endpoint capabilities, and account pools.
- API Keys that can authorize multiple platforms, multiple subscription instances, and balance fallback.
- Subscription-first billing with snapshot multipliers and a separate global balance multiplier.
- OpenAI Images, Chat Completions, Responses, and Messages runtime paths.
- Offline Docker delivery for servers without a registry dependency.

The product layer is XCode-owned. Sub2API is the current runtime implementation for scheduling, OAuth refresh, protocol adaptation, retries, cooling, and failover. See [Architecture](docs/ARCHITECTURE.md) for the replacement boundary.

## Quick start

Download the release asset, create an override for the inherited compose file, then load and start the stack:

```yaml
# docker-compose.xcode.yml
services:
  sub2api:
    image: xcode:latest
```

```bash
docker load -i xcode_latest.tar
docker compose -f deploy/docker-compose.yml -f docker-compose.xcode.yml up -d
```

Copy and configure `deploy/.env.example` before the first start. The inherited base compose file still uses the upstream image name, so production deployment must retain the `xcode:latest` override above. The release contract is the local image `xcode:latest` and the offline archive `xcode_latest.tar`; this project does not publish GHCR images.

## Documentation

- [Project definition](docs/PROJECT_DEFINITION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Development guide](docs/DEVELOPMENT_GUIDE.md)
- [Roadmap](docs/ROADMAP.md)
- [Deployment handoff](docs/HANDOFF.md)
- [Current project state](docs/memory/当前状态.md)

## Development

The frontend uses Vue 3, TypeScript, Vite, and Vitest. The backend uses Go, PostgreSQL, and Redis. Read the development guide before changing product rules, runtime boundaries, migrations, or release workflows.

```bash
cd backend && make test-unit
cd ../frontend && pnpm run test:run && pnpm run typecheck && pnpm run lint:check
```

## Upstream

The official source is maintained as the `upstream` remote for reviewed updates. XCode's production branch is `main`; release tags start at `v1.0.0`.

## License

This repository retains the upstream license and required notices in [LICENSE](LICENSE). Review upstream notices before redistributing a modified build.
