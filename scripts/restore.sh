#!/bin/sh
set -eu
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then echo "usage: $0 <dump-file>" >&2; exit 2; fi
# Stop all application and notification workers before replacing database state.
docker compose stop earnest
docker compose up -d postgres
docker compose exec -T postgres pg_restore --username=earnest --dbname=earnest --clean --if-exists --exit-on-error < "$1"
# Failure leaves the application stopped, so an unrotated database is not served.
docker compose run --rm --no-deps earnest generation rotate
echo 'Restore and generation rotation complete. Start the application when ready.'
