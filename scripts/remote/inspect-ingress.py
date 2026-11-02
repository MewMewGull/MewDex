"""Read ingress structure without printing credentials or complete configuration."""
import hashlib
import json
import pathlib
import re

path = pathlib.Path('/root/Docker/caddy/Caddyfile')
raw = path.read_bytes()
text = raw.decode('utf-8')
headers = [line.strip() for line in text.splitlines() if line and not line[0].isspace() and re.fullmatch(r'[a-zA-Z0-9*._:,/\[\] -]+\s*\{\s*', line)]
imports = [line.split()[1] for line in text.splitlines() if re.match(r'^\s*import\s', line)]
print(json.dumps({'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'lines': len(text.splitlines()), 'siteHeaders': headers, 'imports': imports, 'containsMewDex': bool(re.search('mewdex', text, re.I))}))
