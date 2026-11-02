"""Prepare a separate public-Hub profile, preserving the device and model home."""
import hashlib
import json
import os
import pathlib

root = pathlib.Path('/root/MewDex/.state/risk-20261028')
source = root / 'device-hub-gateway.json'
target = root / 'device-https-hub-gateway.json'
profile = json.loads(source.read_text())
if profile.get('deviceId') not in ('mewdex-risk-nest-1', 'mewdex-risk-nest-highspeed') or profile.get('hub') or not profile.get('executor'):
    raise SystemExit('Unexpected source device; no profile was changed.')
engine = profile.get('engine', {})
if not engine.get('useHubGateway') or engine.get('proxyUrl') or engine.get('proxyTokenFile'):
    raise SystemExit('Source is not the reviewed Hub-forwarding configuration.')
profile['hubUrl'] = 'https://mewdex.mewgullnest.top'
raw = (json.dumps(profile, indent=2) + '\n').encode()
if target.exists():
    if target.read_bytes() != raw:
        raise SystemExit('A different HTTPS profile exists; it was preserved.')
else:
    with target.open('xb') as file:
        os.chmod(target, 0o600)
        file.write(raw)
print(json.dumps({'deviceId': profile['deviceId'], 'target': str(target), 'hubUrl': profile['hubUrl'], 'sourcePreserved': True, 'tokenCopied': False, 'modelHomeChanged': False, 'sha256': hashlib.sha256(raw).hexdigest()}))
