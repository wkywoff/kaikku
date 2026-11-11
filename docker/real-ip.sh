#!/bin/sh
# Кому nginx верит в X-Forwarded-For (шаблон подключает этот список).
# По адресу клиента считаются лимиты входа, а заголовок может прислать кто
# угодно, поэтому верим только своему reverse proxy:
#   по умолчанию — этой машине: прокси на хосте (127.0.0.1:8080) приходит в
#   контейнер с адреса шлюза сети Docker;
#   TRUSTED_PROXIES — адреса или подсети прокси на других машинах, через
#   пробел или запятую: 10.0.0.5 192.168.1.0/24.
# Остальным (телефон в домашней сети при HTTP_BIND=0.0.0.0, соседние
# контейнеры) заголовок не поможет: для них адрес — адрес соединения.
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
