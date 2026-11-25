#!/bin/sh
set -eu

out=/etc/nginx/kaikku-real-ip.conf
proxies=$(printf '%s' "${TRUSTED_PROXIES:-}" | tr ',' ' ')
if [ -z "$(printf '%s' "$proxies" | tr -d ' ')" ]; then
  gateway=$(ip route 2>/dev/null | awk '$1 == "default" { print $3; exit }')
  proxies="127.0.0.1 $gateway"
fi

: > "$out"
for p in $proxies; do
  case "$p" in
    *[!0-9A-Fa-f.:/]*) echo "kaikku: TRUSTED_PROXIES: not an address, skipped: $p" >&2 ;;
    *) echo "set_real_ip_from $p;" >> "$out" ;;
  esac
done
echo "kaikku: X-Forwarded-For is trusted from: $(sed 's/^set_real_ip_from //; s/;$//' "$out" | tr '\n' ' ')" >&2
