#!/usr/bin/env python3
"""Install the pinned Linux amd64 upstream binary after checking its archive digest."""
import hashlib, io, json, os, platform, tarfile, urllib.request
from pathlib import Path
root=Path(__file__).resolve().parent.parent
if platform.system()!='Linux' or platform.machine() not in ('x86_64','amd64'):
    raise SystemExit('Only the pinned Linux amd64 artifact is supported by this installer')
deps=json.loads((root/'config/dependencies.json').read_text())
version=deps['github_mcp']
url=f'https://github.com/github/github-mcp-server/releases/download/{version}/github-mcp-server_Linux_x86_64.tar.gz'
with urllib.request.urlopen(url,timeout=60) as r:
    data=r.read(128*1024*1024+1)
if len(data)>128*1024*1024: raise SystemExit('Archive exceeds size limit')
if hashlib.sha256(data).hexdigest()!=deps['github_mcp_linux_amd64_archive_sha256']:
    raise SystemExit('Archive checksum mismatch')
with tarfile.open(fileobj=io.BytesIO(data)) as archive:
    candidates=[x for x in archive.getmembers() if x.isfile() and Path(x.name).name=='github-mcp-server']
    if len(candidates)!=1: raise SystemExit('Unexpected archive layout')
    payload=archive.extractfile(candidates[0]).read()
p=root/'bin/github-mcp-server';p.parent.mkdir(exist_ok=True)
temporary=p.with_suffix('.tmp');temporary.write_bytes(payload);temporary.chmod(0o755)
os.replace(temporary,p)
print('Installed verified GitHub MCP',version)
