# Deployment examples

These systemd units are starting points, not a complete production deployment.
Adjust users, paths, TLS and network policy for your environment.

## Hub

1. Build and install the binaries:

   ```sh
   nix-shell --run 'make build'
   sudo install -d -o root -g root /opt/emergency-alert-system/bin
   sudo install -m 0755 bin/hub bin/beacon /opt/emergency-alert-system/bin/
   sudo install -m 0644 README.md THIRD_PARTY_NOTICES.md /opt/emergency-alert-system/
   ```

2. Create the service account and state directory:

   ```sh
   sudo useradd --system --home /var/lib/emergency-alert-system --shell /usr/sbin/nologin emergency
   sudo install -d -o emergency -g emergency -m 0750 /var/lib/emergency-alert-system
   ```

3. Put the admin token in an environment file readable only by the service:

   ```sh
   sudo install -d -o root -g emergency -m 0750 /etc/emergency-alert-system
   printf 'HUB_ADMIN_TOKEN=%s\n' "$(openssl rand -hex 32)" | \
     sudo tee /etc/emergency-alert-system/hub.env >/dev/null
   sudo chmod 0640 /etc/emergency-alert-system/hub.env
   ```

4. Install and start the unit:

   ```sh
   sudo install -m 0644 deploy/hub.service /etc/systemd/system/hub.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now hub.service
   ```

5. Terminate TLS with a reverse proxy and restrict `/admin` and `/api/admin/`
   to the operator network. The hub does not provide TLS itself.

   The example unit passes `-trust-proxy`, which makes rate limiting use the
   first address in `X-Forwarded-For` (or `X-Real-IP`). Only enable this when
   the hub is reachable solely through a trusted proxy that overwrites those
   headers. If the hub is exposed directly, remove `-trust-proxy`.

## Beacon

The beacon is intended to run on a separate host or at least a separate network
path. It only needs read access to a snapshot directory.

```sh
sudo install -d -o root -g root -m 0755 /srv/emergency-snapshot
sudo rsync -a --delete /var/lib/emergency-alert-system/snapshot/ /srv/emergency-snapshot/
sudo install -m 0644 deploy/beacon.service /etc/systemd/system/beacon.service
sudo systemctl daemon-reload
sudo systemctl enable --now beacon.service
```

Refresh the snapshot by copying a new one and leaving `index.html` as the last
file written. `rsync` without `--delete` is safer if you want to keep the old
page until the new files are in place; the beacon serves whichever complete
`index.html` is present.

Verify both services:

```sh
curl -fsS http://127.0.0.1:8080/api/health | jq
curl -fsS http://127.0.0.1:8081/api/health | jq
```
