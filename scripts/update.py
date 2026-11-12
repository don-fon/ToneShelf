"""Build and update an existing Linux deployment through Workbench CLI."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tarfile
import uuid
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parent.parent


def run(args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, cwd=ROOT, **kwargs)


def remote(cli, instance, command):
    result = run([*cli, 'exec', '-i', instance, '-c', command, '--timeout', '1800', '--output', 'json'],
                 capture_output=True, text=True, encoding='utf-8')
    payload = json.loads(result.stdout)
    # CLI success alone is insufficient: inspect the remote command's exit code.
    if not isinstance(payload, dict) or type(payload.get('exit_code')) is not int:
        raise RuntimeError('Workbench returned no remote exit_code: ' + result.stdout[:1000])
    print(payload.get('output', ''), end='')
    print(payload.get('stderr', ''), end='', file=sys.stderr)
    if payload['exit_code'] != 0:
        raise RuntimeError('Remote command failed, exit_code=' + str(payload['exit_code']))


def package(arch):
    run([sys.executable, '-m', 'unittest', 'discover', '-s', 'scripts', '-p', 'test_update.py'])
    npm = shutil.which('npm')
    if not npm:
        raise RuntimeError('npm is not available from PATH')
    run([npm, 'ci', '--prefer-offline', '--no-audit', '--no-fund'])
    for args in ([npm, 'test'], [npm, 'run', 'typecheck'], [npm, 'run', 'build'],
                 ['go', 'test', './apps/server/...'], ['go', 'vet', './apps/server/...']):
        run(args)
    revision = run(['git', 'rev-parse', '--short=12', 'HEAD'], capture_output=True, text=True).stdout.strip()
    release = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + revision + '-' + uuid.uuid4().hex[:8]
    directory = ROOT / '.cache' / 'releases' / release
    app = directory / 'app'
    app.mkdir(parents=True)
    env = {**os.environ, 'GOOS': 'linux', 'GOARCH': arch, 'CGO_ENABLED': '0'}
    run(['go', 'build', '-trimpath', '-o', app / 'music-player', './apps/server/cmd/music-player'], env=env)
    shutil.copytree(ROOT / 'apps/web/dist', app / 'web')
    files = {p.relative_to(app).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
             for p in sorted(app.rglob('*')) if p.is_file()}
    (app / 'manifest.json').write_text(json.dumps({'release': release, 'revision': revision,
        'arch': arch, 'files': files}, indent=2), encoding='utf-8')
    archive = directory / 'release.tar.gz'
    with tarfile.open(archive, 'w:gz') as bundle:
        for p in sorted(app.rglob('*')):
            if p.is_file():
                bundle.add(p, arcname=p.relative_to(app).as_posix(), recursive=False)
    return release, archive


def deploy(args, release, archive):
    cli = [args.workbench, '-r', args.region]
    if args.profile:
        cli += ['--profile', args.profile]
    stage = '/var/tmp/toneshelf-update-' + release
    # All remote arguments are individually quoted for the Linux shell.
    remote(cli, args.instance_id, 'umask 077 && mkdir -- ' + shlex.quote(stage))
    for local, name in ((archive, 'release.tar.gz'), (ROOT / 'scripts/deploy/update-server.py', 'update-server.py')):
        run([*cli, 'upload', local, stage + '/' + name, '-i', args.instance_id])
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    command = ['python3', stage + '/update-server.py', '--archive', stage + '/release.tar.gz', '--sha256', digest]
    remote(cli, args.instance_id, shlex.join(command))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--instance-id')
    parser.add_argument('--region', default='cn-hangzhou')
    parser.add_argument('--profile')
    parser.add_argument('--workbench', default='workbench', help='CLI executable path')
    parser.add_argument('--arch', choices=['amd64', 'arm64'], default='amd64')
    parser.add_argument('--package-only', action='store_true', help='Build and verify without connecting to ECS')
    args = parser.parse_args()
    if not args.package_only:
        if not args.instance_id:
            parser.error('--instance-id is required unless --package-only is used')
        if not shutil.which(args.workbench):
            parser.error('Workbench not found; use --workbench with its full executable path')
        if run(['git', 'status', '--porcelain'], capture_output=True, text=True).stdout.strip():
            parser.error('Commit or stash worktree changes before deploying')
    release, archive = package(args.arch)
    print('Package: ' + str(archive), flush=True)
    if not args.package_only:
        deploy(args, release, archive)


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.CalledProcessError) as reason:
        print('Update failed: ' + str(reason), file=sys.stderr)
        sys.exit(1)
