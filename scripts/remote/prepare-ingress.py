"""Prepare and validate one isolated Caddy site without changing the live config."""
import argparse
import hashlib
import json
import os
import pathlib
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--expected-source', required=True)
args = parser.parse_args()
source = pathlib.Path('/root/Docker/caddy/Caddyfile')
root = pathlib.Path('/root/MewDex/.state/ingress-20261028')
raw = source.read_bytes()
digest = hashlib.sha256(raw).hexdigest()
if digest != args.expected_source:
    raise SystemExit('Existing ingress changed; prepare again after inspection.')
if b'mewdex.mewgullnest.top' in raw:
    raise SystemExit('A MewDex site already exists; inspect it before changing anything.')
block = b'''\n\nmewdex.mewgullnest.top {
    log_skip
    reverse_proxy 172.18.0.1:47834 {
        flush_interval -1
    }
}
'''
candidate = raw + block
root.mkdir(parents=True, exist_ok=True, mode=0o700)
def retain(path, data):
    if path.exists():
        if path.read_bytes() != data:
            raise SystemExit('A different retained candidate exists; it was preserved.')
    else:
        with path.open('xb') as f:
            os.chmod(path, 0o600)
            f.write(data)
retain(root / 'Caddyfile.original', raw)
retain(root / 'Caddyfile.candidate', candidate)
retain(root / 'mewdex-site.caddy', block.lstrip())
result = subprocess.run(['docker', 'exec', '-i', 'caddy-edge', 'caddy', 'validate', '--config', '-', '--adapter', 'caddyfile'], input=candidate, capture_output=True, timeout=30)
with (root / 'validation.log').open('wb') as f:
    os.chmod(f.name, 0o600)
    f.write(result.stdout + result.stderr)
adapted = subprocess.run(['docker', 'exec', '-i', 'caddy-edge', 'caddy', 'adapt', '--config', '-', '--adapter', 'caddyfile'], input=raw, capture_output=True, timeout=15, check=True)
active = subprocess.run(['docker', 'exec', 'caddy-edge', 'wget', '-qO-', 'http://127.0.0.1:2019/config/'], capture_output=True, timeout=15, check=True)
active_matches_source = json.loads(adapted.stdout) == json.loads(active.stdout)
retain(root / 'active-original.json', active.stdout)
report = {
    'host': 'mewdex.mewgullnest.top', 'livePath': str(source),
    'sourceSha256': digest, 'candidateSha256': hashlib.sha256(candidate).hexdigest(),
    'candidate': str(root / 'Caddyfile.candidate'), 'backup': str(root / 'Caddyfile.original'),
    'originalBytesPreserved': candidate.startswith(raw), 'addedSiteCount': 1,
    'upstream': '172.18.0.1:47834', 'logsDisabledForNewSite': True,
    'validationExitCode': result.returncode, 'liveConfigChanged': False, 'reloaded': False,
    'activeMatchesSource': active_matches_source,
}
(root / 'manifest.json').write_text(json.dumps(report, indent=2) + '\n')
os.chmod(root / 'manifest.json', 0o600)
print(json.dumps(report))
if result.returncode:
    raise SystemExit('Candidate validation failed; private diagnostics retained beside the candidate.')
if not active_matches_source:
    raise SystemExit('Active Caddy configuration differs from the source file; inspect before preparing a reload.')
