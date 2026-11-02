# Earnest

> [!IMPORTANT]
> LLM disclosure: This codebase was written with substantial help from large language models: AI coding agents working from the [`AGENTS.md`](AGENTS.md) brief in this repo.

An RSS reader for web, Mac and Android, with an API that LLMs can use: agents
get their own identity, feed allowlist, read position, annotations, budget and
revert log. Self-hosted on your own hardware via Docker Compose.

> ### Status: scaffold
>
> Nothing is built yet beyond the deployment plumbing: one Go binary that
> serves `/healthz` and its build version, a Postgres + pgvector sidecar, and
> the compose / update / release chain — so the infrastructure is real before
> the service layer lands. The product plan is the
> [preliminary research](docs/research/README.md).

## Deploy

```sh
cp .env.example .env          # set EARNEST_DB_PASSWORD for real deployments
docker compose up -d --build  # earnest on :8080, postgres + pgvector alongside
```

On a deployment host, `./update.sh` syncs the checkout, rebuilds the image
locally with fresh base layers and recreates the stack.

## Develop

`make check` mirrors CI (gofmt, `go vet`, `go test -race`, compose validation,
image build). `scripts/build.sh` produces `dist/earnest` (`--docker` also
builds `earnest:local`); `scripts/release.sh X.Y.Z --push` tags a release —
the tag is the version, stamped into the binary at build time.

## License

[Unlicense](LICENSE) — public domain.
