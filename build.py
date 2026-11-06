#!/usr/bin/env python3
"""Build a pinned native connector and reproducible runtime/source archives."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import zipfile

VERSION = '0.2.0'
UPSTREAM_VERSION = '1.18.0'
UPSTREAM_SHA256 = 'c9dd3e4ac2e5e5afa7e3ae6c2ab0794994c834438b25791fc8de13c614c13b85'
ROOT = Path(__file__).resolve().parent

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()

def archive(path, entries):
    with zipfile.ZipFile(path, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as z:
        for src, name in sorted(entries, key=lambda pair: pair[1]):
            if src.is_symlink():
                raise ValueError('Symlinks cannot be packaged')
            info = zipfile.ZipInfo(name, (2026, 11, 6, 0, 0, 0))
            info.create_system = 3
            info.external_attr = (stat.S_IFREG | (0o755 if src.stat().st_mode & 0o111 else 0o644)) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, src.read_bytes())

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--go', default='go')
    p.add_argument('--official-cli', required=True, type=Path)
    p.add_argument('--output', type=Path, default=ROOT / 'dist')
    a = p.parse_args()
    if digest(a.official_cli) != UPSTREAM_SHA256:
        raise SystemExit('Official CLI does not match the pinned darwin-arm64 SHA-256')
    a.output.mkdir(parents=True, exist_ok=True)
    pkg = ROOT / 'package'
    vendor = pkg / 'vendor/darwin_arm64/difyctl'
    vendor.parent.mkdir(parents=True, exist_ok=True)
    if a.official_cli.resolve() != vendor.resolve():
        shutil.copyfile(a.official_cli, vendor)
    vendor.chmod(0o755)
    adapter = pkg / 'bin/darwin_arm64/dify-qwen'
    adapter.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOOS='darwin', GOARCH='arm64', CGO_ENABLED='0',
               GOCACHE=str(ROOT / '.build-cache'), GOPATH=str(ROOT / '.build-gopath'))
    subprocess.run([a.go, 'build', '-trimpath', '-ldflags=-s -w -buildid=', '-o', str(adapter), './src'],
                   cwd=ROOT, env=env, check=True)
    adapter.chmod(0o755)
    actual = subprocess.check_output([adapter, '--version'], text=True).strip()
    assert actual == VERSION
    files = [(f, f.relative_to(pkg).as_posix()) for f in pkg.rglob('*') if f.is_file()]
    installer = a.output / f'dify-qwen-cli-desktop-arm64-{VERSION}.zip'
    archive(installer, files)
    with zipfile.ZipFile(installer) as z:
        names = z.namelist()
        assert len(names) == len(set(names)) and len(names) < 2048
        assert sum(i.file_size for i in z.infolist()) < 512 * 1024**2
        assert installer.stat().st_size < 50 * 1024**2
        for i in z.infolist():
            path = PurePosixPath(i.filename)
            assert not path.is_absolute() and '..' not in path.parts and not i.flag_bits & 1
            assert not stat.S_ISLNK(i.external_attr >> 16)
            assert not any(part in {'.git', '.env', '.DS_Store', '__pycache__'} for part in path.parts)
        manifest = json.loads(z.read('.qwenwork-plugin/plugin.json'))
        assert manifest['version'] == VERSION
        for skill in manifest['skills']:
            assert skill in names and z.read(skill).strip()
        for name in ('bin/darwin_arm64/dify-qwen', 'vendor/darwin_arm64/difyctl'):
            assert z.getinfo(name).external_attr >> 16 & 0o111
    sources = [(f, 'package/' + name) for f, name in files if not name.startswith(('vendor/', 'bin/'))]
    for folder in ('src', 'tests', 'publish'):
        sources.extend((f, f.relative_to(ROOT).as_posix()) for f in (ROOT / folder).rglob('*')
                       if f.is_file() and '__pycache__' not in f.parts)
    sources.extend((ROOT / name, name) for name in ('go.mod', 'README.md', 'build.py', '.gitignore'))
    source_zip = a.output / f'dify-qwen-source-and-tests-{VERSION}.zip'
    archive(source_zip, sources)
    record = {'connector_version': VERSION, 'official_cli_version': UPSTREAM_VERSION,
              'official_cli_sha256': digest(vendor), 'adapter_sha256': digest(adapter),
              'go': subprocess.check_output([a.go, 'version'], text=True).strip(),
              'runtime_zip_entries': len(files), 'runtime_zip_bytes': installer.stat().st_size,
              'target': 'darwin/arm64', 'live_client_acceptance': 'not yet verified'}
    (a.output / 'BUILD-RECORD.json').write_text(json.dumps(record, indent=2) + '\n')
    sums = [digest(f) + '  ' + f.name for f in sorted(a.output.iterdir())
            if f.is_file() and f.name != 'SHA256SUMS.txt']
    (a.output / 'SHA256SUMS.txt').write_text('\n'.join(sums) + '\n')
    print(json.dumps(record, indent=2))
    print(installer)

if __name__ == '__main__':
    main()
