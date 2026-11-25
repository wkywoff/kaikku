#!/bin/sh
set -eu

f=/usr/share/nginx/html/.well-known/security.txt
mark='# Written by kaikku when the web container starts.'
if [ -e "$f" ] && [ "$(head -n 1 "$f")" != "$mark" ]; then exit 0; fi
mkdir -p "${f%/*}"
expires=$(date -u -d "@$(( $(date +%s) + 365 * 24 * 3600 ))" +%Y-%m-%dT%H:%M:%SZ)
cat > "$f" <<EOF
$mark
Contact: https://github.com/wkywoff/kaikku/security/advisories/new
Expires: $expires
Policy: https://github.com/wkywoff/kaikku/blob/main/SECURITY.md
Preferred-Languages: en, ru
EOF
