#!/bin/sh
# Run only in a fresh disposable VM. The host must have systemd and nginx.
set -eu
[ "${SINGFORGE_DISPOSABLE_VM:-}" = 1 ] || { echo 'requires disposable VM' >&2; exit 1; }
[ ! -e /var/lib/singforge/current ] && [ ! -e /usr/local/bin/singforge ] || exit 1
umask 077
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
install -m 755 "$1" /usr/local/bin/singforge
cd "$tmp"
singforge install --mode direct --host 127.0.0.1 --listen 127.0.0.1
singforge protocol configure --id reality --port 18443
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 -subj /CN=sub.example.test -addext subjectAltName=DNS:sub.example.test,DNS:cdn.example.test -keyout key -out cert 2>/dev/null
singforge cloudflare enable --mode cdn --host cdn.example.test --cert cert --key key
singforge subscription publish --host sub.example.test --cert cert --key key --ca cert
url=$(singforge subscription rotate --format mihomo --ttl 5m)
fetch(){ curl -sS -w '%{http_code}' -o body --cacert cert --noproxy '*' --resolve sub.example.test:443:127.0.0.1 "$1"; }
test "$(fetch "$url")" = 200
grep -q proxies body
test "$(fetch https://sub.example.test/sub.txt)" = 404
pid=$(systemctl show -p MainPID --value singforge-core)
singforge subscription revoke --yes
test "$(fetch "$url")" = 404
test "$pid" = "$(systemctl show -p MainPID --value singforge-core)"
singforge rules schedule --yes
systemctl is-enabled singforge-rules.timer
singforge rules unschedule --yes
test ! -e /etc/systemd/system/singforge-rules.timer
before=$(readlink /var/lib/singforge/current)
# Keep an independently owned port busy while exercising activation rollback.
python3 -c 'import socket,time; s=socket.socket(); s.bind(("0.0.0.0",0)); s.listen(); print(s.getsockname()[1],flush=True); time.sleep(180)' > busy-port &
busy=$!
trap 'kill "$busy" 2>/dev/null || true; rm -rf "$tmp"' EXIT
while [ ! -s busy-port ]; do sleep 0.2; done
if singforge protocol configure --id ss --port "$(cat busy-port)"; then exit 1; fi
test "$before" = "$(readlink /var/lib/singforge/current)"
# Simulate a digest-verified update whose run mode fails after successful validation.
core=$(readlink -f "/proc/$(systemctl show -p MainPID --value singforge-core)/exe")
printf '#!/bin/sh\nif [ "$1" = run ]; then exit 75; fi\nexec "%s" "$@"\n' "$core" > failed-update
chmod 755 failed-update
if singforge core update --file "$tmp/failed-update" --sha256 "$(sha256sum failed-update | cut -d' ' -f1)"; then exit 1; fi
test "$before" = "$(readlink /var/lib/singforge/current)"
singforge doctor
singforge backup create --file "$tmp/backup"
singforge uninstall --yes --purge
test ! -e /etc/systemd/system/singforge-core.service
singforge backup restore --file "$tmp/backup" --yes
singforge doctor
test "$(readlink /etc/systemd/system/multi-user.target.wants/singforge-core.service)" = ../singforge-core.service
singforge uninstall --yes --purge
