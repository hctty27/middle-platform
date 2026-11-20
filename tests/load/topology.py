"""Generate loopback-only Supervisor and Nginx configs for the disposable runner."""
import os
import pathlib
import sys

root = pathlib.Path(sys.argv[1]).resolve()
mode = sys.argv[2]
counts = {"compact": (2, 1, 1, 1), "cluster": (2, 4, 6, 1)}[mode]
services = (
    ("config-center", "CONFIG_GO_ADDR", 8201, "config"),
    ("sdk-core", "SDK_CORE_ADDR", 8211, "core"),
    ("sdk-service", "SDK_SERVICE_ADDR", 8221, "service"),
    ("sdk-admin", "SDK_ADMIN_ADDR", 8231, "sdk/admin"),
)
supervisor = [
    f"[unix_http_server]\nfile={root}/supervisor.sock\nchmod=0700",
    f"[supervisord]\nlogfile={root}/logs/supervisord.log\npidfile={root}/supervisor.pid\nchildlogdir={root}/logs",
    "[rpcinterface:supervisor]\nsupervisor.rpcinterface_factory=supervisor.rpcinterface:make_main_rpcinterface",
    f"[supervisorctl]\nserverurl=unix://{root}/supervisor.sock",
]
nginx = [
    "worker_processes 1;",
    f"pid {root}/nginx.pid;",
    f"error_log {root}/logs/nginx-error.log warn;",
    "events { worker_connections 2048; }",
    "http {",
    *[f"{kind}_temp_path {root}/{kind}_temp;" for kind in ("client_body", "proxy", "fastcgi", "uwsgi", "scgi")],
    f"access_log {root}/logs/nginx-access.log combined buffer=64k flush=1s;",
]
ports = []
diagnostics = []
profile_enabled = os.environ.get('LOAD_PROFILE') in ('capacity', 'traffic-multi')
for (name, variable, first, prefix), count in zip(services, counts):
    upstream = name.replace("-", "_")
    nginx += [f"upstream {upstream} {{", "least_conn;"]
    for i in range(count):
        port = first + i
        ports.append(port)
        program = f"{name}-{i+1}"
        diagnostic_env = ''
        if profile_enabled:
            diag_port = port + 100
            diagnostics.append(f'{program}\t{diag_port}')
            diagnostic_env = f',LOAD_DIAGNOSTICS_ADDR="127.0.0.1:{diag_port}"'
        supervisor.append(
            f"[program:{program}]\ncommand={root}/bin/{name} -config {root}/config/{name}.yaml\n"
            f"directory={root}\nautostart=true\nautorestart=unexpected\nstartsecs=1\n"
            f"stopasgroup=true\nkillasgroup=true\nredirect_stderr=true\n"
            f"stdout_logfile={root}/logs/{program}.log\nstdout_logfile_maxbytes=10MB\n"
            f"stdout_logfile_backups=1\nenvironment={variable}=\"127.0.0.1:{port}\",GOMEMLIMIT=\"128MiB\"{diagnostic_env}"
        )
        nginx.append(f"server 127.0.0.1:{port} max_fails=1 fail_timeout=10s;")
    nginx += [f"keepalive {64 if name == 'sdk-core' else 16};", "}"]
supervisor.append(
    f"[program:mock]\ncommand={root}/bin/mock\nautostart=true\nautorestart=unexpected\n"
    f"startsecs=1\nredirect_stderr=true\nstdout_logfile={root}/logs/mock.log"
)
ports.append(8290)
nginx += [
    "server {", "listen 127.0.0.1:8280;",
    "proxy_http_version 1.1;", 'proxy_set_header Connection "";',
    "proxy_connect_timeout 2s;", "proxy_read_timeout 65s;",
    "proxy_next_upstream error timeout invalid_header http_502 http_504;",
    "proxy_next_upstream_tries 2;", "proxy_next_upstream_timeout 5s;",
]
for name, _, _, prefix in services:
    upstream = name.replace("-", "_")
    nginx.append(f"location ^~ /api/{prefix}/ {{ proxy_pass http://{upstream}; }}")
nginx += ["}", "}"]
(root / "supervisor.conf").write_text("\n\n".join(supervisor) + "\n")
(root / "nginx.conf").write_text("\n".join(nginx) + "\n")
(root / "ports.txt").write_text("\n".join(map(str, ports)) + "\n")

(root / 'diagnostics.tsv').write_text('\n'.join(diagnostics) + '\n' if diagnostics else '')
