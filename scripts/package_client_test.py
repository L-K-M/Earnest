#!/usr/bin/env python3
"""Exercise packaging gates with disposable Git repositories and fake tools.

These are orchestration tests, not evidence of real signing, notarization,
Gatekeeper acceptance, Android cryptography, or device compatibility.
"""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import zipfile


SCRIPT = Path(__file__).with_name("package-client.sh")
CERTIFICATE = b"fixture-public-certificate"

TOOLS = r'''#!/usr/bin/env python3
import hashlib, json, os, pathlib, sys, zipfile
p = pathlib.Path
name = p(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ['FAKE_LOG'], 'a') as log:
    log.write(json.dumps({'tool': name, 'args': args}) + '\n')
def enabled(key): return os.environ.get(key) == '1'
def value(key): return args[args.index(key) + 1]
if name == 'uname':
    print('Darwin')
elif name == 'security':
    print('1) FAKE "Developer ID Application: Fixture (ABCDEFGHIJ)"')
elif name == 'xcodegen':
    pass
elif name == 'xcodebuild':
    if enabled('FAKE_BUILD_FAIL'): sys.exit(42)
    if p('Sources/Untracked.swift').exists() or p('Sources/local-secret.txt').exists():
        sys.exit('Uncommitted sources entered the build')
    app = p(value('-archivePath')) / 'Products/Applications/Earnest.app'
    (app / 'Contents/MacOS').mkdir(parents=True)
    (app / 'Contents/MacOS/Earnest').write_bytes(b'fixture-binary')
    settings = dict(item.split('=', 1) for item in args if '=' in item)
    metadata = {'CFBundleIdentifier': 'ch.lkmc.Earnest',
                'CFBundleShortVersionString': settings['MARKETING_VERSION'],
                'CFBundleVersion': settings['CURRENT_PROJECT_VERSION']}
    if enabled('FAKE_BAD_VERSION'): metadata['CFBundleVersion'] = '999'
    (app / 'Contents/Info.plist').write_text(json.dumps(metadata))
elif name == 'lipo':
    print('arm64' if enabled('FAKE_BAD_ARCH') else 'arm64 x86_64')
elif name == 'codesign':
    if '--display' in args:
        team = 'ZZZZZZZZZZ' if enabled('FAKE_BAD_TEAM') else 'ABCDEFGHIJ'
        print('TeamIdentifier=' + team)
        print('Authority=Developer ID Application: Fixture')
        print('flags=0x0' if enabled('FAKE_NO_RUNTIME') else 'flags=0x10000(runtime)')
elif name == 'ditto':
    if '-c' in args:
        source, destination = map(p, args[-2:])
        with zipfile.ZipFile(destination, 'w') as archive:
            for file in source.rglob('*'):
                if file.is_file() and not (enabled('FAKE_LOST_TICKET') and file.name == '.ticket'):
                    archive.write(file, p(source.name) / file.relative_to(source))
    else:
        source, destination = map(p, args[-2:])
        with zipfile.ZipFile(source) as archive: archive.extractall(destination)
elif name == 'xcrun':
    if args[:1] == ['--find']:
        print('/fixture/notarytool')
    elif args[:2] == ['notarytool', 'submit']:
        print(json.dumps({'status': 'Invalid' if enabled('FAKE_NOTARY_INVALID') else 'Accepted', 'id': 'fixture-submission'}))
    elif args[:2] == ['stapler', 'staple']:
        (p(args[-1]) / 'Contents/.ticket').write_text('fixture-ticket')
    elif args[:2] == ['stapler', 'validate']:
        if not (p(args[-1]) / 'Contents/.ticket').exists(): sys.exit('missing stapled ticket')
    else:
        sys.exit('unexpected xcrun call')
elif name == 'plutil':
    print(json.loads(p(args[-1]).read_text())[value('-extract')])
elif name == 'spctl':
    if enabled('FAKE_GATEKEEPER_FAIL'): sys.exit('Gatekeeper rejected the fixture')
elif name == 'java':
    print('openjdk version "21.0.1"', file=sys.stderr)
elif name == 'keytool':
    if '-storepass:env' not in args: sys.exit('password was not passed by environment name')
    p(value('-file')).write_bytes(b'fixture-public-certificate')
elif name == 'apksigner':
    if enabled('FAKE_BAD_APK_SIGNATURE'): sys.exit('invalid APK signature')
    if '--print-certs' in args:
        cert = b'wrong-certificate' if enabled('FAKE_BAD_APK_CERT') else b'fixture-public-certificate'
        print('Signer #1 certificate SHA-256 digest: ' + hashlib.sha256(cert).hexdigest())
elif name == 'aapt2':
    data = json.loads(p(args[-1]).read_text())
    if enabled('FAKE_BAD_APK_VERSION'): data['versionCode'] = '999'
    print("package: name='ch.lkmc.earnest' versionCode='%s' versionName='%s'" % (data['versionCode'], data['versionName']))
else:
    sys.exit('unexpected fake tool: ' + name)
'''

GRADLE = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
if os.environ.get('FAKE_BUILD_FAIL') == '1': sys.exit(42)
args = sys.argv[1:]
with open(os.environ['FAKE_LOG'], 'a') as log:
    log.write(json.dumps({'tool': 'gradlew', 'args': args}) + '\n')
properties = dict(item[2:].split('=', 1) for item in args if item.startswith('-P'))
file = pathlib.Path('app/build/outputs/apk/release/app-release.apk')
file.parent.mkdir(parents=True)
file.write_text(json.dumps({'versionName': properties['earnestVersion'], 'versionCode': properties['earnestVersionCode']}))
'''


class PackageClientTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="earnest-package-test-")
        self.addCleanup(self.temporary.cleanup)
        self.scratch = Path(self.temporary.name)
        self.repo = self.scratch / "repo"
        self.repo.mkdir()
        self.bin = self.scratch / "bin"
        self.bin.mkdir()
        self.log = self.scratch / "tools.jsonl"
        self.output = self.scratch / "packages"
        self.env = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("EARNEST_", "GIT_", "FAKE_"))
        }
        self.env.update({
            "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
            "FAKE_LOG": str(self.log),
            "EARNEST_MAC_SIGN_IDENTITY": "Developer ID Application: Fixture (ABCDEFGHIJ)",
            "EARNEST_MAC_TEAM_ID": "ABCDEFGHIJ",
            "EARNEST_MAC_NOTARY_PROFILE": "fixture-profile",
            "EARNEST_ANDROID_KEYSTORE": str(self.scratch / "fixture.keystore"),
            "EARNEST_ANDROID_STORE_PASSWORD": "fixture-store-password",
            "EARNEST_ANDROID_KEY_ALIAS": "fixture",
            "EARNEST_ANDROID_KEY_PASSWORD": "fixture-key-password",
            "ANDROID_HOME": str(self.scratch / "sdk"),
            "JAVA_HOME": str(self.scratch / "jdk"),
        })
        for name in ("uname", "security", "xcodegen", "xcodebuild", "lipo", "codesign", "ditto", "xcrun", "plutil", "spctl"):
            self.executable(self.bin / name, TOOLS)
        for name in ("java", "keytool"):
            self.executable(self.scratch / "jdk/bin" / name, TOOLS)
        for name in ("apksigner", "aapt2"):
            self.executable(self.scratch / "sdk/build-tools/36.1.0" / name, TOOLS)
        Path(self.env["EARNEST_ANDROID_KEYSTORE"]).write_bytes(b"fixture-keystore-not-a-real-key")
        (self.repo / "scripts").mkdir()
        shutil.copy2(SCRIPT, self.repo / "scripts/package-client.sh")
        self.executable(self.repo / "clients/android/gradlew", GRADLE)
        (self.repo / "clients/mac/App/Sources").mkdir(parents=True)
        (self.repo / "clients/mac/App/project.yml").write_text("name: Fixture\n")
        (self.repo / ".gitignore").write_text("local-secret.txt\n")
        self.git("init", "-q")
        self.git("add", ".")
        self.git("-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "Fixture")
        self.git("-c", "tag.gpgSign=false", "tag", "v1.2.3")
        self.revision = self.git("rev-parse", "HEAD").stdout.strip()

    @staticmethod
    def executable(path, content):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        path.chmod(0o755)

    def git(self, *args):
        return subprocess.run(["git", "-C", str(self.repo), *args], env=self.env, text=True, capture_output=True, check=True)

    def run_package(self, platform="mac", *extra, env=None, tag="v1.2.3"):
        return subprocess.run(
            ["bash", str(self.repo / "scripts/package-client.sh"), platform, tag, str(self.output), *extra],
            env={**self.env, **(env or {})}, capture_output=True, text=True, timeout=30,
        )

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def rejected(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("Verified ", result.stdout)
        self.assertFalse((self.output / "mac").exists())
        self.assertFalse((self.output / "android").exists())

    def test_check_only_does_not_build_or_claim_signing(self):
        result = self.run_package("mac", "--check")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("No build, signing, notarization or device test was performed", result.stdout)
        self.assertFalse(self.output.exists())
        self.assertFalse(any(call["tool"] == "xcodebuild" for call in self.calls()))

    def test_requires_an_exact_existing_source_tag(self):
        self.rejected(self.run_package(tag="v9.9.9"))
        self.rejected(self.run_package(tag="not-a-tag"))
        self.git("-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--allow-empty", "-qm", "Later")
        result = self.run_package()
        self.rejected(result)
        self.assertIn("must name the checked-out commit", result.stderr)

    def test_refuses_uncommitted_tracked_changes(self):
        (self.repo / "clients/mac/App/project.yml").write_text("name: Changed\n")
        result = self.run_package()
        self.rejected(result)
        self.assertIn("tracked changes", result.stderr)

    def test_refuses_shallow_history(self):
        clone = self.scratch / "shallow"
        subprocess.run(["git", "clone", "-q", "--depth=1", self.repo.as_uri(), str(clone)], env=self.env, check=True)
        self.repo = clone
        result = self.run_package()
        self.rejected(result)
        self.assertIn("complete history", result.stderr)

    def test_rejects_missing_or_development_signing_identity(self):
        self.rejected(self.run_package(env={"EARNEST_MAC_SIGN_IDENTITY": ""}))
        result = self.run_package(env={"EARNEST_MAC_SIGN_IDENTITY": "Apple Development: Fixture"})
        self.rejected(result)
        self.assertIn("Developer ID Application", result.stderr)
        self.assertFalse(any(call["tool"] == "xcodebuild" for call in self.calls()))

    def test_rejects_missing_notary_profile(self):
        self.rejected(self.run_package(env={"EARNEST_MAC_NOTARY_PROFILE": ""}))
        self.assertFalse(any(call["tool"] == "xcodebuild" for call in self.calls()))

    def test_rejects_wrong_mac_version_architecture_team_or_runtime(self):
        for flag in ("FAKE_BAD_VERSION", "FAKE_BAD_ARCH", "FAKE_BAD_TEAM", "FAKE_NO_RUNTIME"):
            with self.subTest(flag=flag):
                self.rejected(self.run_package(env={flag: "1"}))

    def test_rejects_notary_invalid_even_with_zero_tool_exit(self):
        result = self.run_package(env={"FAKE_NOTARY_INVALID": "1"})
        self.rejected(result)
        self.assertIn("Notarization returned Invalid", result.stderr)

    def test_checks_stapled_ticket_in_distributed_archive(self):
        self.rejected(self.run_package(env={"FAKE_LOST_TICKET": "1"}))
        self.assertTrue(any(call["tool"] == "ditto" and "-x" in call["args"] for call in self.calls()))

    def test_rejects_gatekeeper_failure(self):
        self.rejected(self.run_package(env={"FAKE_GATEKEEPER_FAIL": "1"}))

    def test_mac_archive_receipt_and_committed_sources_only(self):
        (self.repo / "clients/mac/App/Sources/Untracked.swift").write_text("not committed")
        (self.repo / "clients/mac/App/Sources/local-secret.txt").write_text("not for a release")
        result = self.run_package()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        receipt = json.loads((self.output / "mac/release-mac.json").read_text())
        artifact = self.output / "mac" / receipt["artifact"]
        self.assertEqual(receipt["revision"], self.revision)
        self.assertEqual(receipt["build"], 1)
        self.assertEqual(receipt["version"], "1.2.3")
        self.assertEqual(receipt["sha256"], hashlib.sha256(artifact.read_bytes()).hexdigest())
        self.assertEqual(receipt["signer"], {"team_id": "ABCDEFGHIJ"})
        self.assertEqual(receipt["notarization"]["status"], "Accepted")
        with zipfile.ZipFile(artifact) as archive:
            self.assertIn("Earnest.app/Contents/.ticket", archive.namelist())
        self.assertTrue((self.output / "mac/SHA256SUMS-mac").read_text().startswith(receipt["sha256"]))

    def test_android_requires_all_signing_inputs_outside_the_repository(self):
        for name in ("KEYSTORE", "STORE_PASSWORD", "KEY_ALIAS", "KEY_PASSWORD"):
            with self.subTest(name=name):
                self.rejected(self.run_package("android", env={"EARNEST_ANDROID_" + name: ""}))
        key = self.repo / "local.keystore"
        key.write_bytes(b"not a key")
        result = self.run_package("android", env={"EARNEST_ANDROID_KEYSTORE": str(key)})
        self.rejected(result)
        self.assertIn("outside the repository", result.stderr)

    def test_android_rejects_invalid_signature_certificate_and_version(self):
        for flag in ("FAKE_BAD_APK_SIGNATURE", "FAKE_BAD_APK_CERT", "FAKE_BAD_APK_VERSION"):
            with self.subTest(flag=flag):
                self.rejected(self.run_package("android", env={flag: "1"}))

    def test_android_package_receipt_and_no_password_arguments(self):
        result = self.run_package("android")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        receipt = json.loads((self.output / "android/release-android.json").read_text())
        artifact = self.output / "android" / receipt["artifact"]
        self.assertEqual(receipt["revision"], self.revision)
        self.assertEqual(receipt["sha256"], hashlib.sha256(artifact.read_bytes()).hexdigest())
        self.assertEqual(receipt["signer"], {"certificate_sha256": hashlib.sha256(CERTIFICATE).hexdigest()})
        invocations = json.dumps(self.calls()) + result.stdout + result.stderr
        self.assertNotIn("fixture-store-password", invocations)
        self.assertNotIn("fixture-key-password", invocations)
        gradle = next(call for call in self.calls() if call["tool"] == "gradlew")
        self.assertIn("--no-daemon", gradle["args"])
        self.assertIn("--no-configuration-cache", gradle["args"])
        self.assertIn("-PearnestVersion=1.2.3", gradle["args"])
        self.assertIn("-PearnestVersionCode=1", gradle["args"])

    def test_build_failure_never_produces_a_package(self):
        for platform in ("mac", "android"):
            with self.subTest(platform=platform):
                self.rejected(self.run_package(platform, env={"FAKE_BUILD_FAIL": "1"}))

    def test_existing_output_is_preserved(self):
        existing = self.output / "mac"
        existing.mkdir(parents=True)
        sentinel = existing / "keep.txt"
        sentinel.write_text("earlier release")
        result = self.run_package()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("Verified ", result.stdout)
        self.assertEqual(sentinel.read_text(), "earlier release")
        self.assertEqual(list(existing.iterdir()), [sentinel])


if __name__ == "__main__":
    unittest.main()
