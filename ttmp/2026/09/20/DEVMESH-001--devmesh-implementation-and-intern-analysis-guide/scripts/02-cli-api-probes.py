#!/usr/bin/env python3
"""Black-box v2 verification probes using isolated temporary binaries/sockets.
Run from repository root. No Docker access and no writes outside a temp directory.
"""
import http.client
import json
import os
import pathlib
import socket
import subprocess
import tempfile
import time


class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('devmesh', timeout=3)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


def free_loopback_addr():
    s = socket.socket()
    s.bind(('127.0.0.1', 0))
    addr = '127.0.0.1:' + str(s.getsockname()[1])
    s.close()
    return addr


with tempfile.TemporaryDirectory(prefix='dm-review-') as td:
    td = pathlib.Path(td)
    env = dict(os.environ, GOWORK='off')
    for binary in ('devmesh', 'devmeshd'):
        subprocess.run(['go', 'build', '-o', str(td / binary), './cmd/' + binary], env=env, check=True)

    # P13: an occupied required listener must fail daemon startup before the
    # Unix API becomes reachable.
    occupied = socket.socket()
    occupied.bind(('127.0.0.1', 0))
    occupied.listen()
    occupied_addr = '127.0.0.1:' + str(occupied.getsockname()[1])
    failed_sock = str(td / 'failed.sock')
    failed = subprocess.run(
        [str(td / 'devmeshd'), 'serve', '--socket', failed_sock, '--state', str(td / 'failed-state.json'),
         '--docker-enabled=false', '--http-enabled=true', '--http-addr', occupied_addr],
        text=True, capture_output=True, timeout=10,
    )
    print('P13 occupied HTTP listener rejected=%s socket_created=%s' %
          (failed.returncode != 0, pathlib.Path(failed_sock).exists()))
    occupied.close()

    sock = str(td / 'api.sock')
    addr = free_loopback_addr()
    log = open(td / 'daemon.log', 'w')
    p = subprocess.Popen(
        [str(td / 'devmeshd'), 'serve', '--socket', sock, '--state', str(td / 'state.json'),
         '--docker-enabled=false', '--http-enabled=true', '--http-addr', addr],
        stdout=log, stderr=log,
    )

    def api(method, path, body=None):
        c = UnixHTTP(sock)
        c.request(method, path, body=None if body is None else json.dumps(body),
                  headers={'Content-Type': 'application/json'})
        r = c.getresponse()
        code, data = r.status, r.read()
        c.close()
        return code, json.loads(data) if data else {}

    try:
        for _ in range(100):
            try:
                health = api('GET', '/v1/health')
                break
            except (OSError, http.client.HTTPException):
                time.sleep(.05)
        else:
            raise RuntimeError('healthy daemon did not start')
        print('healthy daemon=' + json.dumps(health))

        # P14: callers cannot select the lease-free Docker source.
        docker_payload = {'name': 'web.review', 'kind': 'http', 'http_host': 'web.test',
                          'source': 'docker', 'backend': {'host': '127.0.0.1', 'port': 49900}}
        code, result = api('POST', '/v1/registrations', docker_payload)
        print('P14 public Docker-source rejected=%s code=%d' % (code == 400, code))

        # P15/P16: manual HTTP registration returns a usable high-port URL and
        # CLI resolve preserves it as endpoint.
        payload = {'name': 'web.review', 'kind': 'http', 'http_host': 'web.test',
                   'source': 'manual', 'backend': {'host': '127.0.0.1', 'port': 49900}}
        code, result = api('POST', '/v1/registrations', payload)
        print('P15 HTTP registration status=%d url=%s configured_listener=%s' %
              (code, result['frontend']['url'], addr))
        cli = subprocess.run(
            [str(td / 'devmesh'), 'services', 'resolve', 'web.review', '--socket', sock, '--format', 'json'],
            text=True, capture_output=True, check=True,
        )
        print('P16 CLI HTTP resolution=' + cli.stdout.strip())

        # P17: owner_key is an unknown rejected field, even if copied from
        # inspect output; no unauthenticated replacement occurs.
        _, info = api('GET', '/v1/services/web.review/inspect')
        payload['owner_key'] = info['owner_key']
        payload['backend']['port'] = 49901
        code, _ = api('POST', '/v1/registrations', payload)
        _, after = api('GET', '/v1/services/web.review/inspect')
        print('P17 public owner replay rejected=%s backend_unchanged=%s' %
              (code == 400, after['backend']['port'] == 49900))
    finally:
        p.terminate()
        try:
            p.wait(timeout=10)
        except subprocess.TimeoutExpired:
            p.kill()
            p.wait()
        log.close()
    print('Control SIGTERM: exit=%s socket_removed=%s' % (p.returncode, not pathlib.Path(sock).exists()))
