#!/usr/bin/env python3
"""Test build orchestration with disposable trees and fake toolchains.

These checks never launch Finder, install applications, or build real clients.
"""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("build.sh")
TOOLS = r'''#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys
p = pathlib.Path
name, args = p(sys.argv[0]).name, sys.argv[1:]
with open(os.environ['FAKE_LOG'], 'a') as log:
    log.write(json.dumps({'tool': name, 'args': args}) + '\n')
if name == 'uname':
    print(os.environ.get('FAKE_HOST', 'Linux'))
elif name == 'pnpm':
    if args == ['build'] and not os.environ.get('FAKE_NO_WEB_OUTPUT'):
        p('dist/assets').mkdir(parents=True, exist_ok=True)
        p('dist/index.html').write_text('new client')
        p('dist/assets/app.js').write_text('new script')
elif name == 'cp':
    if os.environ.get('FAKE_COPY_FAIL') and args[-2] == 'web/dist/.':
        p(args[-1], 'partial.js').write_text('incomplete copy')
        sys.exit(42)
    sys.exit(subprocess.run([os.environ['REAL_CP'], *args]).returncode)
elif name == 'mv':
    if os.environ.get('FAKE_PUBLISH_FAIL') and args[-1] == 'internal/web/dist' and p(args[-2]).name == 'next':
        sys.exit(42)
    if os.environ.get('FAKE_RESTORE_FAIL') and p(args[-2]).name == 'previous':
        sys.exit(42)
    sys.exit(subprocess.run([os.environ['REAL_MV'], *args]).returncode)
elif name == 'go':
    if os.environ.get('FAKE_GO_FAIL'): sys.exit(42)
    out = p(args[args.index('-o') + 1])
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text('fixture binary')
elif name in ('docker', 'open'):
    pass
else:
    sys.exit('unexpected tool: ' + name)
'''


class BuildTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="earnest-build-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "scripts").mkdir()
        shutil.copy2(SCRIPT, self.root / "scripts/build.sh")
        (self.root / "web").mkdir()
        self.embedded = self.root / "internal/web/dist"
        self.embedded.mkdir(parents=True)
        (self.embedded / "index.html").write_text("previous client")
        (self.embedded / "previous.js").write_text("previous script")
        tools = self.root / "tools"
        tools.mkdir()
        for tool in ("uname", "pnpm", "cp", "mv", "go", "docker", "open"):
            path = tools / tool
            path.write_text(TOOLS)
            path.chmod(0o755)
        self.log = self.root / "tools.jsonl"
        self.env = {
            **os.environ,
            "PATH": str(tools) + os.pathsep + os.environ["PATH"],
            "REAL_CP": shutil.which("cp"),
            "REAL_MV": shutil.which("mv"),
            "FAKE_LOG": str(self.log),
            "VERSION": "fixture",
            "REVISION": "fixture",
            "GO": "go",
        }

    def run_build(self, *args, **env):
        return subprocess.run(
            ["/bin/bash", str(self.root / "scripts/build.sh"), *args],
            cwd=self.root,
            env={**self.env, **env},
            capture_output=True,
            text=True,
            timeout=20,
        )

    def calls(self, tool):
        return [json.loads(line)["args"] for line in self.log.read_text().splitlines()
                if json.loads(line)["tool"] == tool]

    def assert_previous_client(self):
        self.assertEqual((self.embedded / "index.html").read_text(), "previous client")
        self.assertEqual((self.embedded / "previous.js").read_text(), "previous script")
        self.assertFalse((self.embedded / "partial.js").exists())
        self.assertEqual(list(self.embedded.parent.glob(".dist-stage.*")), [])

    def test_web_replaces_embedded_assets(self):
        result = self.run_build("web")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.embedded / "index.html").read_text(), "new client")
        self.assertTrue((self.embedded / "assets/app.js").is_file())
        self.assertFalse((self.embedded / "previous.js").exists())
        self.assertEqual(list(self.embedded.parent.glob(".dist-stage.*")), [])

    def test_web_can_stage_without_previous_assets(self):
        shutil.rmtree(self.embedded)
        result = self.run_build("web")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.embedded / "index.html").read_text(), "new client")

    def test_failed_web_copy_preserves_previous_assets(self):
        result = self.run_build("web", FAKE_COPY_FAIL="1")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("web: FAILED", result.stdout)
        self.assert_previous_client()

    def test_failed_web_publish_restores_previous_assets(self):
        result = self.run_build("web", FAKE_PUBLISH_FAIL="1")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_previous_client()

    def test_failed_restore_keeps_recoverable_backup(self):
        result = self.run_build("web", FAKE_PUBLISH_FAIL="1", FAKE_RESTORE_FAIL="1")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        backups = list(self.embedded.parent.glob(".dist-stage.*/previous"))
        self.assertEqual(len(backups), 1)
        self.assertEqual((backups[0] / "index.html").read_text(), "previous client")
        self.assertIn(str(backups[0].relative_to(self.root)), result.stderr)

    def test_missing_web_output_preserves_previous_assets(self):
        result = self.run_build("web", FAKE_NO_WEB_OUTPUT="1")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assert_previous_client()

    def test_docker_target_needs_no_host_binary(self):
        result = self.run_build("docker")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.calls("go"), [])
        self.assertEqual(self.calls("docker")[-1][-2:], ["earnest:local", "."])

    def test_legacy_docker_flag_also_builds_host_binary(self):
        result = self.run_build("--docker")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(len(self.calls("go")), 1)
        self.assertTrue((self.root / "dist/earnest").is_file())
        self.assertEqual(len(self.calls("docker")), 2)

    def test_failures_are_aggregated_without_skipping_later_targets(self):
        result = self.run_build("binary", "docker", FAKE_GO_FAIL="1")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("binary: FAILED", result.stdout)
        self.assertIn("docker: built", result.stdout)

    def test_binary_does_not_reveal_stale_mac_app(self):
        (self.root / "dist/Earnest.app").mkdir(parents=True)
        result = self.run_build("binary", FAKE_HOST="Darwin")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.calls("open"), [[str(self.root / "dist")]])


if __name__ == "__main__":
    unittest.main()
