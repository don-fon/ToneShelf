"""Local tests; no cloud credentials, remote host, or real systemd required."""
import hashlib
from contextlib import closing
import importlib.util
import io
import json
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
import types
import unittest
from unittest.mock import patch


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).parent / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


client = load('update_client', 'update.py')
server = load('update_server', 'deploy/update-server.py')


class UpdateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for name in ('stdout', 'stderr'):
            output = patch.object(sys, name, io.StringIO())
            output.start()
            self.addCleanup(output.stop)

    def bundle(self, changes=None):
        header = bytearray(20)
        header[:6] = b'\x7fELF\x02\x01'
        header[18:20] = (62).to_bytes(2, 'little')
        files = {'music-player': bytes(header), 'web/index.html': b'<html>ok</html>'}
        manifest = {'release': 'test-release', 'arch': 'amd64',
                    'files': {name: hashlib.sha256(data).hexdigest() for name, data in files.items()}}
        files['manifest.json'] = json.dumps(manifest).encode()
        files.update(changes or {})
        archive = self.root / 'release.tar.gz'
        with tarfile.open(archive, 'w:gz') as bundle:
            for name, data in files.items():
                item = tarfile.TarInfo(name)
                item.size = len(data)
                bundle.addfile(item, io.BytesIO(data))
        return archive, hashlib.sha256(archive.read_bytes()).hexdigest()

    def test_unpack_integrity_and_architecture(self):
        releases = self.root / 'releases'
        releases.mkdir()
        archive, digest = self.bundle()
        with patch.object(server.platform, 'machine', return_value='x86_64'):
            with self.assertRaisesRegex(ValueError, 'SHA-256'):
                server.unpack(archive, 'wrong', releases)
            release = server.unpack(archive, digest, releases)
        self.assertEqual((release / 'web/index.html').read_bytes(), b'<html>ok</html>')
        with patch.object(server.platform, 'machine', return_value='aarch64'):
            with self.assertRaisesRegex(ValueError, 'architecture'):
                server.unpack(archive, digest, releases)
        self.assertFalse(list(releases.glob('.incoming-*')))

    def test_rejects_tampering_and_path_traversal_before_activation(self):
        releases = self.root / 'releases'
        releases.mkdir()
        for changes in ({'web/index.html': b'tampered'}, {'../escape': b'bad'},
                        {'web/../escape': b'bad'}, {'config.json': b'bad'}):
            archive, digest = self.bundle(changes)
            with patch.object(server.platform, 'machine', return_value='x86_64'):
                with self.assertRaises(ValueError):
                    server.unpack(archive, digest, releases)
            self.assertEqual(list(releases.iterdir()), [])
        self.assertFalse((self.root / 'escape').exists())

    def fixture(self):
        app, data = self.root / 'app', self.root / 'data'
        app.mkdir()
        data.mkdir()
        database = data / 'music.db'
        with closing(sqlite3.connect(database)) as db, db:
            db.execute('CREATE TABLE upload_jobs(state TEXT)')
            db.execute('CREATE TABLE tracks(title TEXT)')
            db.execute("INSERT INTO tracks VALUES ('preserved song')")
        (data / 'audio').write_bytes(b'preserved audio')
        config = self.root / 'config.json'
        config.write_text('{}')
        release = app / 'releases/new'
        release.mkdir(parents=True)
        old = app / 'releases/old'
        old.mkdir()
        return app, config, data, database, release, old

    def activation(self, fail=None, busy=False):
        app, config, data, database, release, old = self.fixture()
        if busy:
            with closing(sqlite3.connect(database)) as db, db:
                db.execute("INSERT INTO upload_jobs VALUES ('receiving')")
        current = [old]
        commands = []

        def command(args):
            args = [str(arg) for arg in args]
            commands.append(args)
            if args[0] == 'cp':
                if fail == 'backup':
                    raise OSError('copy failed')
                shutil.copytree(args[-2], args[-1])
            if args[:3] == ['systemctl', 'show', '-p']:
                return types.SimpleNamespace(stdout='0' if args[3] == 'MainPID' else 'inactive')
            return types.SimpleNamespace(stdout='')

        def check(base):
            if fail == 'health':
                # Simulate a migration that makes using the old binary unsafe.
                with closing(sqlite3.connect(database)) as db, db:
                    db.execute('CREATE TABLE migrated(value TEXT)')
                raise RuntimeError('unhealthy')

        with patch.object(server, 'current_release', side_effect=lambda _: current[0]), \
             patch.object(server, 'switch_release', side_effect=lambda _, target: current.__setitem__(0, target)):
            if fail or busy:
                with self.assertRaises((OSError, RuntimeError)):
                    server.activate(app, config, data, database, release, 'http://localhost', command, check)
            else:
                server.activate(app, config, data, database, release, 'http://localhost', command, check)
        return current[0], old, release, data, database, commands

    def test_success_preserves_library_and_creates_consistent_backup(self):
        current, _, release, data, _, _ = self.activation()
        self.assertEqual(current, release)
        backup = data.parent / 'data-backups/new'
        self.assertTrue((backup / 'complete.json').exists())
        self.assertEqual((backup / 'data/audio').read_bytes(), b'preserved audio')
        with closing(sqlite3.connect(backup / 'data/music.db')) as db:
            self.assertEqual(db.execute('SELECT title FROM tracks').fetchone(), ('preserved song',))

    def test_backup_failure_restarts_old_release_without_switching(self):
        current, old, _, data, _, commands = self.activation('backup')
        self.assertEqual(current, old)
        self.assertIn(['systemctl', 'start', server.SERVICE], commands)
        self.assertFalse((data.parent / 'data-backups/new/complete.json').exists())

    def test_busy_upload_aborts_and_restarts_original_service(self):
        current, old, _, _, _, commands = self.activation(busy=True)
        self.assertEqual(current, old)
        self.assertIn(['systemctl', 'start', server.SERVICE], commands)
        self.assertFalse(any(cmd[0] == 'cp' for cmd in commands))

    def test_failed_new_release_stops_without_unsafe_database_downgrade(self):
        current, _, release, data, database, commands = self.activation('health')
        self.assertEqual(current, release)
        self.assertEqual(commands[-1], ['systemctl', 'stop', server.SERVICE])
        backup = data.parent / 'data-backups/new/data/music.db'
        with closing(sqlite3.connect(database)) as db:
            self.assertTrue(db.execute("SELECT name FROM sqlite_master WHERE name='migrated'").fetchone())
        with closing(sqlite3.connect(backup)) as db:
            self.assertFalse(db.execute("SELECT name FROM sqlite_master WHERE name='migrated'").fetchone())

    def test_workbench_remote_exit_code_is_required_and_propagated(self):
        for payload in ({'code': 1, 'message': 'error'}, {'exit_code': 9, 'stderr': 'failed'}):
            with patch.object(client, 'run', return_value=types.SimpleNamespace(stdout=json.dumps(payload))):
                with self.assertRaises(RuntimeError):
                    client.remote(['workbench'], 'i-test', 'true')
        with patch.object(client, 'run', return_value=types.SimpleNamespace(stdout='{"exit_code":0}')) as run:
            client.remote(['workbench'], 'i-test', 'true')
            self.assertEqual(run.call_args.args[0][:4], ['workbench', 'exec', '-i', 'i-test'])

    def test_upload_failure_never_runs_remote_update(self):
        args = types.SimpleNamespace(workbench='workbench', instance_id='i-test', region='test', profile=None)
        with patch.object(client, 'remote') as remote, patch.object(client, 'run', side_effect=OSError('upload failed')):
            with self.assertRaises(OSError):
                client.deploy(args, 'release', self.root / 'archive')
        self.assertEqual(remote.call_count, 1)  # Only staging mkdir; never activate.


if __name__ == '__main__':
    unittest.main()
