# Earnest

> [!IMPORTANT]
> LLM disclosure: This codebase was written with substantial help from large language models: AI coding agents working from the [`AGENTS.md`](AGENTS.md) brief in this repo.

Earnest is a self-hosted RSS reader for your own server. You can read on the web and through compatible clients. Agents are governed actors with their own identity, read position, annotations, notification budget and revert log.

The server provides a web client, Google Reader compatibility, MCP and a CLI for agents, Mac and Android clients, and an optional LLM pipeline.

## Quick start

```sh
git clone https://github.com/L-K-M/Earnest.git
cd Earnest
cp .env.example .env
```

Edit `.env` and set `EARNEST_DB_PASSWORD` and `EARNEST_SECRET_KEY` to separate random secrets, for example with `openssl rand -hex 32`. The app secret must be at least 32 bytes. Compose has a development database password fallback, so do not leave the database password empty on a deployment.

```sh
docker compose up -d --build
docker compose exec earnest earnest user create <name>
```

Enter your password at the prompt. Open http://localhost:8080, sign in and import your OPML file during onboarding. For access from other devices, configure both HTTPS origins as described in [operations](docs/operations.md).

See the [documentation index](docs/README.md), [configuration](docs/configuration.md), [agents](docs/agents.md), and [CLI](docs/cli.md). For development, `make check` runs the CI checks; `scripts/build.sh` builds the binary. Releases use Git tags as versions; see [CICD.md](CICD.md).

## License

[Unlicense](LICENSE), public domain.
