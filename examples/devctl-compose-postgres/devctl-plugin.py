#!/usr/bin/env python3
"""A minimal devctl + devmesh integration example.

The plugin declares a foreground Compose database and a consumer process. It
never starts processes itself: devctl owns supervision. The consumer launcher
resolves its dependency immediately before exec, so the planning plugin does
not hold a devmesh lease or freeze a backend endpoint too early.
"""
import json
import os
import shutil
import sys


SERVICE_NAME = "devctl.example.postgres"
# devctl supplies the directory containing .devctl.yaml as repo_root for this
# standalone example. Service cwd is therefore ".", not a path nested under it.
EXAMPLE_DIR = "."


def emit(frame):
    sys.stdout.write(json.dumps(frame) + "\n")
    sys.stdout.flush()


def response(request_id, output=None, error=None):
    frame = {"type": "response", "request_id": request_id, "ok": error is None}
    if error is not None:
        frame["error"] = error
    else:
        frame["output"] = output if output is not None else {}
    emit(frame)


def main():
    emit({
        "type": "handshake",
        "protocol_version": "v2",
        "plugin_name": "devmesh-compose-postgres",
        "capabilities": {"ops": ["config.mutate", "validate.run", "launch.plan"]},
    })

    for line in sys.stdin:
        if not line.strip():
            continue
        request = json.loads(line)
        request_id = request.get("request_id", "")
        operation = request.get("op", "")
        repo_root = request.get("ctx", {}).get("repo_root", "") or os.getcwd()
        example_dir = os.path.join(repo_root, EXAMPLE_DIR)

        if operation == "config.mutate":
            response(request_id, {"config_patch": {"set": {
                "services.database.devmesh_name": SERVICE_NAME,
                "services.consumer.database_url_env": "DATABASE_URL",
            }, "unset": []}})
        elif operation == "validate.run":
            errors = []
            if not os.path.isfile(os.path.join(example_dir, "compose.yaml")):
                errors.append({"code": "E_CONFIG", "message": "compose.yaml is missing"})
            if not os.path.isfile(os.path.join(example_dir, "run-consumer.sh")):
                errors.append({"code": "E_CONFIG", "message": "run-consumer.sh is missing"})
            if shutil.which("docker") is None:
                errors.append({"code": "E_DEPENDENCY", "message": "docker is not on PATH"})
            if shutil.which(os.environ.get("DEVMESH_BIN", "devmesh")) is None:
                errors.append({"code": "E_DEPENDENCY", "message": "devmesh is not on PATH; set DEVMESH_BIN if needed"})
            response(request_id, {"valid": not errors, "errors": errors, "warnings": []})
        elif operation == "launch.plan":
            # Compose stays in the foreground so devctl owns its logs and
            # lifetime. The consumer's script waits for a devmesh registration
            # before it execs; no dependency graph or plugin-held lease is used.
            response(request_id, {"services": [
                {
                    "name": "database",
                    "cwd": EXAMPLE_DIR,
                    "command": ["docker", "compose", "-f", "compose.yaml", "up"],
                },
                {
                    "name": "consumer",
                    "cwd": EXAMPLE_DIR,
                    "command": ["bash", "--noprofile", "--norc", "-lc", "exec ./run-consumer.sh"],
                    "env": {"DEVMESH_SERVICE": SERVICE_NAME, "DEVMESH_WAIT": "45s"},
                },
            ]})
        else:
            response(request_id, error={"code": "E_UNSUPPORTED", "message": "unsupported op: " + operation})


if __name__ == "__main__":
    main()
