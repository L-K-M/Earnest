#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
real_find=$(command -v find)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/bin"
cat > "$scratch/bin/pg_dump" <<'EOF'
#!/bin/sh
touch "$DUMP_CALLED"
printf 'dump' > "$3"
EOF
# Exercise portable pruning even when the test host has GNU find.
cat > "$scratch/bin/find" <<'EOF'
#!/bin/sh
for argument do
    if [ "$argument" = '-printf' ]; then
        echo 'GNU find -printf is not portable' >&2
        exit 1
    fi
done
exec "$BACKUP_TEST_FIND" "$@"
EOF
chmod +x "$scratch/bin/pg_dump" "$scratch/bin/find"
export PATH="$scratch/bin:$PATH" DUMP_CALLED="$scratch/called" BACKUP_TEST_FIND="$real_find"

for keep in 0 00 01 08 -1 invalid; do
    rm -f "$DUMP_CALLED"
    if BACKUP_DIR="$scratch/invalid" BACKUP_KEEP="$keep" sh "$script_dir/backup.sh" > "$scratch/output" 2>&1; then
        echo "FAIL: BACKUP_KEEP=$keep was accepted" >&2
        exit 1
    fi
    if [ -f "$DUMP_CALLED" ]; then
        echo "FAIL: BACKUP_KEEP=$keep reached pg_dump before rejection" >&2
        exit 1
    fi
done

mkdir "$scratch/valid"
touch "$scratch/valid/earnest-20000101T000000Z-1.dump" "$scratch/valid/earnest-20000102T000000Z-1.dump"
BACKUP_DIR="$scratch/valid" BACKUP_KEEP=2 sh "$script_dir/backup.sh" > "$scratch/output"
[ -f "$DUMP_CALLED" ]
[ ! -f "$scratch/valid/earnest-20000101T000000Z-1.dump" ]
[ -f "$scratch/valid/earnest-20000102T000000Z-1.dump" ]
count=$(find "$scratch/valid" -name 'earnest-*.dump' | wc -l)
[ "$count" -eq 2 ]
echo 'PASS: backup retention validation and pruning'
