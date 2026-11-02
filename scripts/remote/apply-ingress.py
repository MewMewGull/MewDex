"""Apply the explicitly approved single-site candidate, preserving the bind inode."""
import datetime
import fcntl
import hashlib
import json
import os
import pathlib
import subprocess

root = pathlib.Path('/root/MewDex/.state/ingress-20261028')
source = pathlib.Path('/root/Docker/caddy/Caddyfile')
manifest = json.loads((root / 'manifest.json').read_text())
original = (root / 'Caddyfile.original').read_bytes()
candidate = (root / 'Caddyfile.candidate').read_bytes()
expected_block = b'''\n\nmewdex.mewgullnest.top {
    log_skip
    reverse_proxy 172.18.0.1:47834 {
        flush_interval -1
    }
}
'''
if candidate != original + expected_block:
    raise SystemExit('Candidate is outside the approved single-site change.')
if hashlib.sha256(original).hexdigest() != manifest['sourceSha256'] or hashlib.sha256(candidate).hexdigest() != manifest['candidateSha256']:
    raise SystemExit('Retained candidate hashes do not match the reviewed manifest.')

def docker(args, data=None):
    return subprocess.run(['docker', 'exec', '-i', 'caddy-edge', *args], input=data, capture_output=True, timeout=30, check=True)

def adapt(raw):
    return json.loads(docker(['caddy', 'adapt', '--config', '-', '--adapter', 'caddyfile'], raw).stdout)

def active():
    return json.loads(docker(['wget', '-qO-', 'http://127.0.0.1:2019/config/']).stdout)

def report(value):
    value['at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    path = root / 'apply-result.json'
    path.write_text(json.dumps(value, indent=2) + '\n')
    os.chmod(path, 0o600)
    print(json.dumps(value))

before, after = adapt(original), adapt(candidate)
docker(['caddy', 'validate', '--config', '-', '--adapter', 'caddyfile'], candidate)
health = json.loads(docker(['wget', '-qO-', 'http://172.18.0.1:47834/health']).stdout)
if health.get('name') != 'MewDex' or health.get('protocolVersion') != 1:
    raise SystemExit('Private upstream is not the expected MewDex Hub.')

with source.open('r+b') as file:
    fcntl.flock(file, fcntl.LOCK_EX)
    inode = os.fstat(file.fileno()).st_ino
    current = file.read()
    actual = active()
    if current == candidate and actual == after:
        report({'state': 'active', 'alreadyApplied': True, 'host': manifest['host'], 'originalBytesPreserved': True, 'reloaded': True, 'inodePreserved': True})
        raise SystemExit(0)
    if current != original or actual != before:
        raise SystemExit('Live source or active configuration changed; nothing was overwritten.')

    def write(raw):
        file.seek(0)
        file.write(raw)
        file.truncate()
        file.flush()
        os.fsync(file.fileno())
        if source.stat().st_ino != inode or source.read_bytes() != raw:
            raise RuntimeError('Live source changed while applying the candidate.')

    try:
        write(candidate)
        # The shared container bind-mount observes the existing inode.
        try:
            docker(['caddy', 'reload', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'])
        except subprocess.SubprocessError:
            # A lost reload response is not evidence that it failed.
            if active() != after:
                raise
        if active() != after:
            raise RuntimeError('Readback does not match the adapted candidate.')
    except Exception as error:
        restored = False
        if source.stat().st_ino == inode and source.read_bytes() == candidate:
            write(original)
            docker(['caddy', 'reload', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'])
            restored = active() == before
        report({'state': 'failed', 'host': manifest['host'], 'rollbackConfirmed': restored, 'errorType': type(error).__name__})
        raise SystemExit('Ingress apply failed; inspect the private result and rollback status.')

report({'state': 'active', 'host': manifest['host'], 'sourceSha256': manifest['sourceSha256'], 'activeSourceSha256': manifest['candidateSha256'], 'originalBytesPreserved': True, 'addedSiteCount': 1, 'reloaded': True, 'activeMatchesCandidate': True, 'inodePreserved': True, 'upstreamHealthVerified': True})
