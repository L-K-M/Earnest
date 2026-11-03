# Operations

## Backups and restore

Compose starts a backup container that makes a custom-format `pg_dump` on startup and every 24 hours. It keeps the newest 14 successful dumps by default. Set `EARNEST_BACKUP_KEEP` to change the count and `EARNEST_BACKUP_VOLUME` to an absolute directory to store dumps outside the named `backups` volume. The backup service dumps the Compose `postgres` database; if `EARNEST_DATABASE_URL` points elsewhere, arrange separate backups for that database. The restore script likewise targets Compose Postgres. Store copies on another machine and protect them and `EARNEST_SECRET_KEY`: dumps contain private reading data and encrypted credentials need the original key.

To restore, first stop every external application and notification worker using the database. From the deployment checkout run `scripts/restore.sh /path/to/earnest-….dump`. It stops the Compose application, restores with `pg_restore --clean --if-exists --exit-on-error`, and rotates the generation before allowing the application to be restarted. If either command fails, leave the application stopped and investigate; do not serve an unrotated restore. After a successful restore, run `docker compose up -d earnest`. Clients must bootstrap again; restored pending notifications are withheld or marked uncertain. See [sync](sync.md#restore-and-notification-delivery).

Periodically test this procedure against a separate disposable stack and database, then verify restored entries, generation rotation and withheld notifications before relying on a backup. Do not test by overwriting the production database.

## Metrics and logs

The fetcher metrics registry exposes Prometheus text through its HTTP handler, including fetch results, duration, ingested items and queue depth. The application server does not yet mount the handler. Inspect container logs with `docker compose logs --tail=100 earnest` or `docker compose logs backup`. Feed credentials and secrets must not be logged.

## Update

Run `./update.sh` from the deployment checkout. It updates the source, pulls external images, rebuilds the locally owned application image with fresh base layers, and recreates the stack. See [CICD.md](../CICD.md).
