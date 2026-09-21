#!/usr/bin/env python3
"""Minimal long-running consumer for the devctl/devmesh example.

The process demonstrates that DATABASE_URL is produced by resolving a stable
frontend immediately before exec. It deliberately does not publish a service
of its own: the example is about consuming a Docker-owned database endpoint.
"""
import os
import signal
import sys
import time


def stop(_signal, _frame):
    raise SystemExit(0)


signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)

print("consumer started with DATABASE_URL=" + os.environ["DATABASE_URL"], file=sys.stderr, flush=True)
while True:
    time.sleep(60)
