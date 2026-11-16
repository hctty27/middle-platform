# Nginx upstream runtime templates

These files target the single-host deployment at `/data/middle-platform`. They use Nginx Open Source passive health checks and controlled `down`/reload operations for rolling deployments. They do not configure TLS, domains, firewall rules, or replace an existing virtual host.

## Instance groups

| Upstream | Supervisor programs | Local ports |
|---|---|---|
| `config_center` | `config-center-1..2` | 8201–8202 |
| `sdk_core` | `sdk-core-1..5` | 8211–8215 |
| `sdk_service` | `sdk-service-1..5` | 8221–8225 |

All Go instances bind to loopback. Nginx is the only public ingress. The app routes are `/api/config/`, `/api/core/`, and `/api/service/`; `config-center-access` is intentionally not part of this 2/5/5 deployment.

## One-time installation

1. Ensure Nginx is installed and managed by systemd as `nginx.service`.
2. Install the upstream definition into the Nginx HTTP include directory (commonly `/etc/nginx/conf.d/`):

```bash
sudo install -m 0644 /data/middle-platform/ops/nginx/middle-platform-upstreams.conf /etc/nginx/conf.d/middle-platform-upstreams.conf
```

3. Merge the three locations from `middle-platform-locations.conf` into the existing application `server {}` block. Keep your existing TLS, domain, access-control, and static admin configuration. Do not create a second catch-all virtual host if one already exists.

4. Add `worker_shutdown_timeout 75s;` to the top-level main context of `/etc/nginx/nginx.conf`, outside `events {}` and `http {}`. If the directive already exists, update it instead of adding a duplicate. This bounds graceful worker draining during Nginx reload.

5. Initialize the runtime upstream files as the SSH deployment user:

```bash
sudo /data/middle-platform/ops/nginx/install-runtime.sh deploy
```

Replace `deploy` with the actual SSH deployment account. The installer preserves existing runtime files and never overwrites a previously drained state. It creates the runtime directory, files and shared `.publish.lock` owned by that account; Monit uses the same lock to avoid racing a release.

6. Check the Nginx configuration and reload it:

```bash
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx
```

The `include` paths in the upstream template are absolute and point to `/data/middle-platform/run/nginx/*.conf`.

## Deployment permissions

The SSH deployment user must be able to write the runtime upstream directory and the shared publish lock file. The server must provide `flock` (normally from util-linux) for deployment/Monit mutual exclusion. Configure passwordless sudo for the exact paths reported by `command -v nginx` and `command -v systemctl`. For example, after verifying those paths, a sudoers drop-in may contain:

```
deploy ALL=(root) NOPASSWD: /usr/sbin/nginx -t, /usr/sbin/nginx -T, /usr/bin/systemctl reload nginx
```

Replace the user and executable paths with the actual values on the server, then validate with `visudo -cf /etc/sudoers.d/middle-platform-deploy`. The helper always uses `sudo -n`; it will fail before replacing application files if the Nginx configuration is not active, the runtime include is not loaded, the expected peer count is wrong, or the sudo permission is missing. Do not grant unrestricted sudo to the deployment account.

## Passive health checks and request retry

The upstreams use `max_fails=1 fail_timeout=10s`. Nginx Open Source observes real proxied traffic, temporarily avoids an upstream after a communication failure, and retries it after the fail window. It does not actively poll `/health/ready`; active upstream health checks and slow start require NGINX Plus.

The proxy snippet retries only transport/protocol failures and HTTP 502/504. It deliberately omits `non_idempotent`, so Nginx does not replay a POST after it has been sent upstream. HTTP 503 is passed through (for example, when the shared MySQL dependency is unavailable) rather than marking every app instance unhealthy.

## Rolling deployment behavior

For each selected Supervisor instance, in configured order:

1. Verify Nginx is active, its effective configuration loads the expected upstream include, and at least one peer remains available.
2. Mark only that instance `down` in its runtime upstream file, run `nginx -t`, and gracefully reload Nginx.
3. Wait for old Nginx workers to exit. The configured `worker_shutdown_timeout 75s` bounds this drain; if the wait fails, deployment stops and the instance stays drained.
4. Restart only that Supervisor program.
5. Require Supervisor `RUNNING` and three consecutive HTTP 200 responses from its local `/health/ready` endpoint.
6. Remove `down`, validate and reload Nginx to return the instance to traffic.

If restart or readiness fails, deployment halts and leaves that instance drained so Nginx does not send it traffic. Remaining instances are not restarted. The script does not perform automatic binary rollback; inspect logs, fix the cause, then restore the instance after it passes Ready.

A full release drains and restores 12 instances individually. It does not stop the entire upstream group. This is rolling availability, not a mathematical zero-error guarantee: clients can still see failures caused by application bugs, shared MySQL outages, Nginx reload errors, or all peers failing.

## Manual operations

```bash
# Validate one group member
/data/middle-platform/ops/nginx/upstream-control.sh validate sdk-core 8211

# Drain / restore one instance (normally done by the publisher)
sudo -u deploy /data/middle-platform/ops/nginx/upstream-control.sh drain sdk-core 8211
sudo -u deploy /data/middle-platform/ops/nginx/upstream-control.sh enable sdk-core 8211
```

Use the actual deployment user instead of `deploy`. Always inspect `run/nginx/*.conf` and run `nginx -t` after manual changes.
