#!/usr/bin/env python3
"""Build a pinned native connector and reproducible runtime/source archives."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import struct
import subprocess
import zipfile

VERSION = '1.1.0'
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

def listing_sources(manifest):
    folder = ROOT / 'publish'
    descriptor = folder / 'connector.json'
    config = json.loads(descriptor.read_text(encoding='utf-8'))
    allowed = {'connector_kind', 'server_key', 'display_name', 'display_name_cn',
               'description', 'version', 'provider_name', 'category_code', 'clients', 'auth_type'}
    if not isinstance(config, dict) or set(config) - allowed:
        raise SystemExit('CLI listing connector.json contains unsupported fields')
    limits = {'server_key': 128, 'display_name': 255, 'display_name_cn': 255,
              'description': 4000, 'version': 64, 'provider_name': 255, 'category_code': 64}
    for field, limit in limits.items():
        if field in config and (not isinstance(config[field], str) or len(config[field]) > limit):
            raise SystemExit('Invalid CLI listing string field: ' + field)
    if not re.fullmatch(r'[a-z0-9][a-z0-9_-]{1,127}', config.get('server_key', '')):
        raise SystemExit('CLI listing server_key must be a 2-128 character lowercase identifier')
    if config.get('connector_kind') != 'cli':
        raise SystemExit('CLI listing connector_kind must be cli')
    if config.get('clients') != ['desktop'] or config.get('auth_type') != 'oauth':
        raise SystemExit('This runtime requires desktop-only OAuth listing configuration')
    if config.get('version') != VERSION or manifest.get('version') != VERSION:
        raise SystemExit('CLI listing, runtime manifest and build versions must match')
    if (folder / '.mcp.json').exists() or (folder / 'credential-config.json').exists():
        raise SystemExit('CLI listing sources must not contain MCP or API-key configuration')
    entries = []
    icons = 0
    for path in folder.rglob('*'):
        if not path.is_file():
            continue
        name = path.relative_to(folder).as_posix()
        if name in {'connector.json', 'README.md', 'README_EN.md'}:
            if path.stat().st_size > 64 * 1024:
                raise SystemExit('CLI listing descriptor and README files must be at most 64 KiB')
        elif path.parent == folder / 'assets' and path.stem == 'icon' and path.suffix.lower() in {'.png', '.jpg', '.jpeg', '.svg'}:
            icons += 1
            if path.stat().st_size > 2 * 1024**2:
                raise SystemExit('CLI listing icon must be at most 2 MiB')
        else:
            continue
        entries.append((path, name))
    if icons > 1:
        raise SystemExit('CLI listing sources must contain at most one icon')
    return entries

UNIX_LAUNCHER = '''#!/bin/sh
set -eu
case "$(uname -s)" in
  Darwin) platform=darwin ;;
  Linux) platform=linux ;;
  *) printf '%s\\n' '{"code":"E-0003","message":"Unsupported desktop operating system."}' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) printf '%s\\n' '{"code":"E-0003","message":"Unsupported desktop processor architecture."}' >&2; exit 1 ;;
esac
entry=$0
while [ -L "$entry" ]; do
  folder=$(CDPATH= cd -P "$(dirname "$entry")" && pwd)
  link=$(readlink "$entry")
  case "$link" in /*) entry=$link ;; *) entry=$folder/$link ;; esac
done
folder=$(CDPATH= cd -P "$(dirname "$entry")" && pwd)
program=$folder/${platform}_${arch}/dify-qwen
if [ ! -x "$program" ]; then
  printf '%s\\n' '{"code":"E-0004","message":"Connector files are missing or not executable. Please reinstall."}' >&2
  exit 1
fi
exec "$program" "$@"
'''


def binary_format(path, target):
    data = path.read_bytes()
    if target.startswith('windows_'):
        offset = struct.unpack_from('<I', data, 0x3c)[0]
        assert data[:2] == b'MZ' and data[offset:offset+4] == b'PE\0\0'
        assert struct.unpack_from('<H', data, offset+4)[0] == 0x8664
    elif target.startswith('linux_'):
        assert data[:6] == b'\x7fELF\x02\x01'
        assert struct.unpack_from('<H', data, 18)[0] == (183 if target.endswith('_arm64') else 62)
    else:
        assert data[:4] == b'\xcf\xfa\xed\xfe'
        assert struct.unpack_from('<I', data, 4)[0] == (0x100000c if target.endswith('_arm64') else 0x1000007)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--go', default='go')
    parser.add_argument('--official-cli', type=Path, help='Optional verified offline binary for a single --target build')
    parser.add_argument('--target', action='append', help='Build only this supported GOOS_GOARCH; default builds all five')
    parser.add_argument('--output', type=Path, default=ROOT / 'dist')
    args = parser.parse_args()
    catalog = json.loads((ROOT / 'src/upstream-catalog.json').read_text())
    targets = args.target or list(catalog['platforms'])
    if not targets or len(targets) != len(set(targets)) or any(t not in catalog['platforms'] for t in targets):
        raise SystemExit('Targets must be distinct platform identifiers from the pinned upstream catalog')
    if args.official_cli and len(targets) != 1:
        raise SystemExit('Offline builds require exactly one --target')
    manifest = json.loads((ROOT / 'package/.qwenwork-plugin/plugin.json').read_text(encoding='utf-8'))
    listing = listing_sources(manifest)
    args.output.mkdir(parents=True, exist_ok=True)
    package = ROOT / 'package'
    binaries = package / 'bin'
    if binaries.exists():
        shutil.rmtree(binaries)
    binaries.mkdir()
    files = [(f, f.relative_to(package).as_posix()) for f in package.rglob('*')
             if f.is_file() and f.relative_to(package).parts[0] not in {'bin', 'vendor'}]
    records = {}
    base_env = dict(os.environ, CGO_ENABLED='0', GOCACHE=str(ROOT / '.build-cache'), GOPATH=str(ROOT / '.build-gopath'))
    for target in targets:
        goos, arch = target.split('_')
        suffix = '.exe' if goos == 'windows' else ''
        adapter = binaries / target / ('dify-qwen' + suffix)
        adapter.parent.mkdir(parents=True)
        env = dict(base_env, GOOS=goos, GOARCH=arch)
        subprocess.run([args.go, 'build', '-trimpath', '-ldflags=-s -w -buildid=', '-o', str(adapter), './src'], cwd=ROOT, env=env, check=True)
        adapter.chmod(0o755)
        binary_format(adapter, target)
        files.append((adapter, adapter.relative_to(package).as_posix()))
        records[target] = {'adapter_sha256': digest(adapter), 'official_cli_sha256': catalog['platforms'][target]['sha256'],
                           'format_verified': True, 'client_acceptance': 'not yet verified'}
        if goos == 'windows':
            launcher = binaries / 'dify-qwen.exe'
            subprocess.run([args.go, 'build', '-trimpath', '-ldflags=-s -w -buildid=', '-o', str(launcher), './cmd/launcher'], cwd=ROOT, env=env, check=True)
            launcher.chmod(0o755)
            binary_format(launcher,target)
            files.append((launcher, 'bin/dify-qwen.exe'))
            records[target]['launcher_sha256'] = digest(launcher)
    launcher = binaries / 'dify-qwen'
    with launcher.open('w',encoding='utf-8',newline='\n') as stream:
        stream.write(UNIX_LAUNCHER)
    launcher.chmod(0o755)
    files.append((launcher, 'bin/dify-qwen'))
    if args.official_cli:
        target = targets[0]
        if digest(args.official_cli) != catalog['platforms'][target]['sha256']:
            raise SystemExit('Offline official CLI does not match the pinned release SHA-256')
        vendor = package / 'vendor' / target / ('difyctl.exe' if target.startswith('windows_') else 'difyctl')
        vendor.parent.mkdir(parents=True, exist_ok=True)
        if args.official_cli.resolve() != vendor.resolve():
            shutil.copyfile(args.official_cli,vendor)
        vendor.chmod(0o755)
        files.append((vendor,vendor.relative_to(package).as_posix()))
    host = subprocess.check_output([args.go,'env','GOHOSTOS','GOHOSTARCH'],text=True).split()
    host_target = '_'.join(host)
    if host_target in targets:
        entry = binaries / ('dify-qwen.exe' if host[0] == 'windows' else 'dify-qwen')
        assert subprocess.check_output([entry,'--version'],text=True).strip() == VERSION
    mode = 'offline' if args.official_cli else 'online'
    label = 'multiplatform' if not args.target else '-'.join(targets)
    installer = args.output / f'dify-qwen-cli-desktop-{label}-{mode}-{VERSION}.zip'
    archive(installer, files)
    with zipfile.ZipFile(installer) as zipped:
        names = zipped.namelist()
        assert len(names) == len(set(names)) and len(names) <= 2048
        assert sum(item.file_size for item in zipped.infolist()) <= 512 * 1024**2
        assert installer.stat().st_size <= 50 * 1024**2
        for item in zipped.infolist():
            path = PurePosixPath(item.filename)
            assert not path.is_absolute() and '..' not in path.parts and '\\' not in item.filename and not item.flag_bits & 1
            assert not stat.S_ISLNK(item.external_attr >> 16)
            assert not any(part in {'.git','.env','.DS_Store','__pycache__'} for part in path.parts)
            if item.filename.startswith('bin/'):
                assert item.external_attr >> 16 & 0o111
        assert json.loads(zipped.read('.qwenwork-plugin/plugin.json'))['version'] == VERSION
        for skill in manifest['skills']:
            assert skill in names and zipped.read(skill).strip()
    listing_zip = args.output / f'dify-qwen-listing-config-{VERSION}.zip'
    archive(listing_zip,listing)
    sources = [(f,'package/'+name) for f,name in files if not name.startswith(('vendor/','bin/'))]
    for folder in ('src','cmd','internal','tests','.github'):
        sources.extend((f,f.relative_to(ROOT).as_posix()) for f in (ROOT/folder).rglob('*')
                       if f.is_file() and '__pycache__' not in f.parts)
    sources.extend((f,'publish/'+name) for f,name in listing)
    sources.extend((ROOT/name,name) for name in ('go.mod','README.md','build.py','.gitignore'))
    for name in ('test-results.json','test-results.txt'):
        if (ROOT/name).is_file():
            sources.append((ROOT/name,name))
    source_zip = args.output / f'dify-qwen-source-and-tests-{VERSION}.zip'
    archive(source_zip,sources)
    record = {'connector_version': VERSION,'official_cli_version': catalog['version'],'distribution': mode,
              'go': subprocess.check_output([args.go,'version'],text=True).strip(),'platforms': records,
              'unix_launcher_sha256': digest(launcher),'runtime_zip_entries': len(files),
              'runtime_zip_bytes': installer.stat().st_size,'listing_zip_sha256': digest(listing_zip),
              'listing_zip_bytes': listing_zip.stat().st_size,'live_client_acceptance': 'not yet verified'}
    (args.output/'BUILD-RECORD.json').write_text(json.dumps(record,indent=2)+'\n')
    (args.output/'SHA256SUMS.txt').write_text(''.join(digest(f)+'  '+f.name+'\n' for f in sorted(args.output.iterdir())
                                                  if f.is_file() and f.name!='SHA256SUMS.txt'))
    print(json.dumps(record,indent=2))
    print(installer)


if __name__ == '__main__':
    main()
