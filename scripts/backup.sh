#!/bin/sh
set -eu
: "${PGHOST:=postgres}" "${PGUSER:=earnest}" "${PGDATABASE:=earnest}" "${BACKUP_DIR:=/backups}" "${BACKUP_KEEP:=14}"
export PGHOST PGUSER PGDATABASE
case "$BACKUP_KEEP" in ''|*[!0-9]*|0*) echo 'BACKUP_KEEP must be a positive integer' >&2; exit 1;; esac
mkdir -p "$BACKUP_DIR"
umask 077
tmp=$(mktemp "$BACKUP_DIR/.earnest-XXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM
pg_dump -Fc -f "$tmp"
name="$BACKUP_DIR/earnest-$(date -u +%Y%m%dT%H%M%SZ)-$$.dump"
mv "$tmp" "$name"
trap - EXIT HUP INT TERM
# Filenames begin with sortable UTC timestamps; never prune failed dumps.
find "$BACKUP_DIR" -maxdepth 1 -type f -name 'earnest-*.dump' -exec basename {} \; | sort -r | tail -n "+$((BACKUP_KEEP + 1))" | while IFS= read -r old; do rm -f -- "$BACKUP_DIR/$old"; done
printf '%s\n' "$name"
