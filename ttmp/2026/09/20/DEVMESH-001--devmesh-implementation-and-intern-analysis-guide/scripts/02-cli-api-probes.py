#!/usr/bin/env python3
"""Black-box review probes; build temporary binaries and use isolated Unix sockets.
No Docker access and no writes outside a temporary directory. Run from repo root.
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

with tempfile.TemporaryDirectory(prefix='dm-review-') as td:
    td = pathlib.Path(td)
    env = dict(os.environ, GOWORK='off')
    for binary in ('devmesh', 'devmeshd'):
        subprocess.run(['go', 'build', '-o', str(td / binary), './cmd/' + binary], env=env, check=True)
    occupied = socket.socket()
    occupied.bind(('127.0.0.1', 0))
    occupied.listen()
    addr = '127.0.0.1:' + str(occupied.getsockname()[1])
    sock = str(td / 'api.sock')
    log = open(td / 'daemon.log', 'w')
    p = subprocess.Popen([str(td/'devmeshd'), 'serve', '--socket', sock, '--state', str(td/'state.json'), '--docker-enabled=false', '--http-enabled=true', '--http-addr', addr], stdout=log, stderr=log)
    def api(method, path, body=None):
        c = UnixHTTP(sock)
        c.request(method, path, body=None if body is None else json.dumps(body), headers={'Content-Type':'application/json'})
        r = c.getresponse()
        code, data = r.status, r.read()
        c.close()
        return code, json.loads(data)
    try:
        for _ in range(100):
            try:
                health = api('GET', '/v1/health')
                break
            except (OSError, http.client.HTTPException):
                time.sleep(.05)
        else:
            raise RuntimeError('daemon did not start')
        time.sleep(.2)
        print('P13 occupied HTTP listener: health=' + json.dumps(health) + ' process_alive=' + str(p.poll() is None))
        payload = {'name':'web.review','kind':'http','http_host':'web.test','source':'docker','backend':{'host':'127.0.0.1','port':49900}}
        code, r = api('POST', '/v1/registrations', payload)
        print('P14 public Docker-source registration: status=%d token_present=%s expiry_present=%s' % (code, 'lease_token' in r, 'expires_at' in r))
        print('P15 HTTP registration URL=' + r['frontend']['url'] + ' configured_listener=' + addr)
        result = subprocess.run([str(td/'devmesh'),'services','resolve','web.review','--socket',sock,'--format','json'], text=True, capture_output=True, check=True)
        print('P16 CLI HTTP resolution=' + result.stdout.strip())
        _, info = api('GET','/v1/services/web.review/inspect')
        payload['owner_key'] = info['owner_key']
        payload['backend']['port'] = 49901
        code, _ = api('POST', '/v1/registrations', payload)
        _, info = api('GET','/v1/services/web.review/inspect')
        print('P17 copy inspected owner without token: status=%d new_backend=%d' % (code, info['backend']['port']))
    finally:
        p.terminate()
        try:
            p.wait(timeout=10)
        except subprocess.TimeoutExpired:
            p.kill(); p.wait()
        log.close()
        occupied.close()
    print('Control SIGTERM: exit=%s socket_removed=%s' % (p.returncode, not pathlib.Path(sock).exists()))
    print('Listener failure diagnostic: ' + '\n'.join(line for line in (td/'daemon.log').read_text().splitlines() if 'http_proxy_listen_failed' in line))
