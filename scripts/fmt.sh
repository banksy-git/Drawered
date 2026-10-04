#!/bin/sh
# Formats Go sources with gofmt, then converts leading tabs to four spaces
# (project convention: spaces, not tabs).
#
# With --check, exits non-zero if any file would change.
set -eu

cd "$(dirname "$0")/.."
files=$(find cmd internal -name '*.go' -not -path '*/node_modules/*')

fmt_one() {
    gofmt "$1" | perl -pe 's/^(\t+)/"    " x length($1)/e'
}

if [ "${1:-}" = "--check" ]; then
    bad=0
    for f in $files; do
        if ! fmt_one "$f" | cmp -s - "$f"; then
            echo "needs formatting: $f"
            bad=1
        fi
    done
    exit $bad
fi

for f in $files; do
    tmp="$f.fmt.$$"
    fmt_one "$f" > "$tmp"
    if cmp -s "$tmp" "$f"; then rm "$tmp"; else mv "$tmp" "$f"; fi
done
