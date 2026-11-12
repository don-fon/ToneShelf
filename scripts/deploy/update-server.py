"""Apply a verified release to the documented Linux/systemd installation (root)."""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shutil
import sqlite3
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request

SERVICE = 'music-player'
APP = Path('/opt/music-player')
CONFIG = Path('/etc/music-player/config.json')


def run(args):
    return subprocess.run([str(arg) for arg in args], check=True, capture_output=True, text=True)


def unpack(archive, digest, releases):
    if hashlib.sha256(archive.read_bytes()).hexdigest() != digest:
        raise ValueError('Archive SHA-256 mismatch')
    stage = Path(tempfile.mkdtemp(prefix='.incoming-', dir=releases))
    try:
        with tarfile.open(archive, 'r:gz') as bundle:
            names = set()
            for item in bundle.getmembers():
                name = PurePosixPath(item.name)
                if (not item.isfile() or name.is_absolute() or '..' in name.parts
                        or '\\' in item.name or item.name in names
                        or str(name) != item.name
                        or not (item.name in ('music-player', 'manifest.json') or item.name.startswith('web/'))):
                    raise ValueError('Invalid archive member: ' + item.name)
                names.add(item.name)
                target = stage / item.name
                target.parent.mkdir(parents=True, exist_ok=True)
                with bundle.extractfile(item) as source, target.open('wb') as dest:
                    shutil.copyfileobj(source, dest)
        manifest = json.loads((stage / 'manifest.json').read_text(encoding='utf-8'))
        if not re.fullmatch(r'[A-Za-z0-9-]{1,100}', manifest['release']):
            raise ValueError('Invalid release ID')
        if set(manifest['files']) != names - {'manifest.json'} or not {'music-player', 'web/index.html'} <= names:
            raise ValueError('Incomplete manifest')
        for name, expected in manifest['files'].items():
            if hashlib.sha256((stage / name).read_bytes()).hexdigest() != expected:
                raise ValueError('File SHA-256 mismatch: ' + name)
        machine = {'x86_64': ('amd64', 62), 'aarch64': ('arm64', 183)}.get(platform.machine())
        header = (stage / 'music-player').read_bytes()[:20]
        if (not machine or manifest['arch'] != machine[0] or len(header) < 20
                or header[:6] != b'\x7fELF\x02\x01' or int.from_bytes(header[18:20], 'little') != machine[1]):
            raise ValueError('Release architecture does not match this Linux server')
        for file in stage.rglob('*'):
            file.chmod(0o755 if file.is_dir() or file.name == 'music-player' else 0o644)
        stage.chmod(0o755)
        release = releases / manifest['release']
        if release.exists():
            raise ValueError('Release already exists')
        stage.rename(release)
        return release
    except BaseException:
        shutil.rmtree(stage)
        raise


def verify_database(database, idle=False):
    with closing(sqlite3.connect(database.as_uri() + '?mode=ro', uri=True)) as db:
        if db.execute('PRAGMA integrity_check').fetchall() != [('ok',)] or db.execute('PRAGMA foreign_key_check').fetchall():
            raise RuntimeError('Database integrity check failed')
        if idle and db.execute("SELECT COUNT(*) FROM upload_jobs WHERE state IN ('receiving','queued','processing')").fetchone()[0]:
            raise RuntimeError('Pending upload/background tasks exist; wait for them to finish before updating')


def healthy(base):
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for route, status in (('/api/health', 200), ('/', 200), ('/api/current-user', 401)):
        try:
            with client.open(base + route, timeout=2) as response:
                if response.status != status:
                    return False
                body = response.read()
                if route == '/api/health' and json.loads(body).get('status') != 'ok':
                    return False
                if route == '/' and b'<html' not in body.lower():
                    return False
        except urllib.error.HTTPError as error:
            if error.code != status:
                return False
    return True


def wait_healthy(base, probe=healthy):
    for _ in range(45):
        try:
            if probe(base):
                return
        except (OSError, ValueError):
            pass
        time.sleep(1)
    raise RuntimeError('Health/static-page/authentication checks did not pass')


def current_release(app):
    current = app / 'current'
    if not current.is_symlink() or current.resolve().parent != (app / 'releases').resolve():
        raise ValueError('current must link to an existing release inside releases/')
    return current.resolve(strict=True)


def switch_release(app, release):
    temporary_link = app / '.current-update'
    try:
        temporary_link.symlink_to(release, target_is_directory=True)
        os.replace(temporary_link, app / 'current')
    finally:
        if temporary_link.is_symlink():
            temporary_link.unlink()


def activate(app, config, data, database, release, base, command=run, check=wait_healthy):
    previous = current_release(app)
    backup = data.parent / (data.name + '-backups') / release.name
    if data.is_symlink() or data == Path(data.anchor) or data in backup.parents:
        raise ValueError('Unsafe data/backup directory')
    backup.mkdir(parents=True, mode=0o700)
    backup.parent.chmod(0o700)
    stopped = False
    new_started = False
    try:
        command(['systemctl', 'is-active', '--quiet', SERVICE])
        stopped = True
        command(['systemctl', 'stop', SERVICE])
        # systemctl stop must actually have drained the unit before copying SQLite.
        state = command(['systemctl', 'show', '-p', 'ActiveState', '--value', SERVICE]).stdout.strip()
        pid = command(['systemctl', 'show', '-p', 'MainPID', '--value', SERVICE]).stdout.strip()
        if state not in ('inactive', 'failed') or pid != '0':
            raise RuntimeError('Service has not fully stopped')
        verify_database(database, idle=True)
        size = 0
        for file in data.rglob('*'):
            if file.is_symlink() or not (file.is_file() or file.is_dir()):
                raise ValueError('Data backup requires ordinary files/directories: ' + str(file))
            if file.is_file():
                size += file.stat().st_size
        if shutil.disk_usage(backup).free < size + 256 * 1024 * 1024:
            raise RuntimeError('Not enough space for a complete cold backup')
        command(['cp', '-a', '--', data, backup / 'data'])
        shutil.copy2(config, backup / 'config.json')
        verify_database(backup / 'data' / database.relative_to(data))
        (backup / 'complete.json').write_text(json.dumps({'previous': str(previous), 'data': str(data),
            'release': str(release), 'config': str(config)}, indent=2), encoding='utf-8')
        switch_release(app, release)
        # Once migration can begin, rolling back just the binary is unsafe.
        new_started = True
        command(['systemctl', 'start', SERVICE])
        check(base)
        command(['systemctl', 'is-active', '--quiet', SERVICE])
        print(json.dumps({'updated': True, 'release': str(release), 'previous': str(previous), 'backup': str(backup)}), flush=True)
    except BaseException:
        if stopped:
            if new_started:
                command(['systemctl', 'stop', SERVICE])
                print('New release failed; service stopped. Restore the COMPLETE data backup and old release together: ' + str(backup), flush=True)
            else:
                if current_release(app) != previous:
                    switch_release(app, previous)
                command(['systemctl', 'start', SERVICE])
        raise


def main():
    import fcntl  # Linux-only execution boundary; packaging is portable.
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--sha256', required=True)
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise RuntimeError('Run the update as root through Workbench')
    os.umask(0o077)
    with (APP / '.update.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        cfg = json.loads(CONFIG.read_text(encoding='utf-8'))
        data = Path(cfg.get('dataDir', '/var/lib/music-player'))
        if not data.is_absolute():
            data = CONFIG.parent / data
        data = data.absolute()
        database = (data / cfg.get('database', 'data/music.db')).resolve()
        if not database.is_relative_to(data) or database == data:
            raise ValueError('Database must be inside the data directory')
        if Path(cfg.get('webDir', '')) != APP / 'current/web':
            raise ValueError('config.webDir must be /opt/music-player/current/web')
        addr = cfg.get('addr', '127.0.0.1:8080')
        if not re.fullmatch(r'(127\.0\.0\.1|localhost|\[::1\]):[0-9]{1,5}', addr):
            raise ValueError('Backend must listen on loopback')
        environment = run(['systemctl', 'show', '-p', 'Environment', '--value', SERVICE]).stdout
        environment_files = run(['systemctl', 'show', '-p', 'EnvironmentFiles', '--value', SERVICE]).stdout.strip()
        invocation = run(['systemctl', 'show', '-p', 'ExecStart', '--value', SERVICE]).stdout
        if 'MUSIC_' in environment or environment_files or str(CONFIG) not in invocation or str(APP / 'current/music-player') not in invocation:
            raise ValueError('Unsupported service overrides; use the documented systemd unit/config')
        release = unpack(args.archive.resolve(), args.sha256, APP / 'releases')
        activate(APP, CONFIG, data, database, release, 'http://' + addr)


if __name__ == '__main__':
    main()
