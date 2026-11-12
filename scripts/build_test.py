#!/usr/bin/env python3
"""Test build orchestration with disposable trees and fake toolchains.

These checks never launch Finder, install applications, or build real clients.
"""

import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("build.sh")
TOOLS = r'''#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys
p = pathlib.Path
name, args = p(sys.argv[0]).name, sys.argv[1:]
with open(os.environ['FAKE_LOG'], 'a') as log:
    log.write(json.dumps({'tool': name, 'args': args, 'path': sys.argv[0]}) + '\n')
if name == 'uname':
    print(os.environ.get('FAKE_HOST', 'Linux'))
elif name == 'pnpm':
    if args == ['build'] and not os.environ.get('FAKE_NO_WEB_OUTPUT'):
        p('dist/assets').mkdir(parents=True, exist_ok=True)
        p('dist/index.html').write_text('new client')
        p('dist/assets/app.js').write_text('new script')
elif name == 'cp':
    if os.environ.get('FAKE_APK_STAGE_FAIL') and args[-1].endswith('/earnest-debug.apk'):
        sys.exit(42)
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
elif name in ('docker', 'open', 'java'):
    pass
elif name == 'xcodegen':
    if not os.environ.get('FAKE_MAC_BUILD'): sys.exit(42)
elif name == 'xcodebuild':
    app = p(args[args.index('-derivedDataPath') + 1]) / 'Build/Products/Release/Earnest.app'
    app.mkdir(parents=True)
    (app / 'fixture').write_text('fixture app')
elif name == 'ditto':
    import shutil
    destination = p(args[1])
    if not destination.is_relative_to(p(os.environ['FAKE_APPLICATIONS'])):
        sys.exit('refusing a non-fixture install')
    shutil.copytree(args[0], destination)
elif name == 'codesign':
    pass
elif name == 'gradlew':
    if os.environ.get('FAKE_GRADLE_FAIL'): sys.exit(42)
    if not os.environ.get('FAKE_NO_APK'):
        apk = p('app/build/outputs/apk/debug/app-debug.apk')
        apk.parent.mkdir(parents=True, exist_ok=True)
        apk.write_text('fixture apk')
elif name == 'adb':
    if args == ['devices']:
        if os.environ.get('FAKE_ADB_DEVICES_FAIL'): sys.exit(42)
        print('List of devices attached')
        print(os.environ.get('FAKE_ADB_DEVICES', 'phone-1\tdevice'))
    elif len(args) == 5 and args[0] == '-s' and args[2:4] == ['install', '-r']:
        if os.environ.get('FAKE_ADB_INSTALL_FAIL'): sys.exit(42)
        if not p(args[4]).is_file(): sys.exit('APK missing')
        print('Success')
    else:
        sys.exit('unexpected adb arguments: ' + repr(args))
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


class AndroidInstallTests(unittest.TestCase):
    run_build = BuildTests.run_build
    calls = BuildTests.calls

    def setUp(self):
        BuildTests.setUp(self)
        self.tools = self.root / "tools"
        # Expose only fixture tools and basic utilities. A developer's real
        # SDK, adb, Java, or Xcode must never affect these tests.
        for tool in ("basename", "dirname", "mkdir", "rm", "awk", "mktemp"):
            (self.tools / tool).symlink_to(shutil.which(tool))
        (self.tools / "python3").symlink_to(sys.executable)
        for tool in ("adb", "java", "xcodegen", "xcodebuild", "ditto", "codesign"):
            self.fake_tool(self.tools / tool)
        self.sdk = self.root / "sdk with spaces"
        (self.sdk / "platforms").mkdir(parents=True)
        self.sdk_adb = self.sdk / "platform-tools/adb"
        self.fake_tool(self.sdk_adb)
        self.fake_tool(self.root / "clients/android/gradlew")
        (self.root / "clients/mac/App").mkdir(parents=True)
        self.env.update({
            "PATH": str(self.tools),
            "HOME": str(self.root / "home"),
            "ANDROID_SDK_ROOT": str(self.sdk),
            "ANDROID_HOME": "",
            "ANDROID_SERIAL": "",
            "JAVA_HOME": "",
            "FAKE_HOST": "Linux",
        })

    def fake_tool(self, path):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(TOOLS)
        path.chmod(0o755)

    def install_calls(self):
        return [args for args in self.calls("adb") if "install" in args]

    def assert_install_failed(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("android: install FAILED", result.stdout)
        self.assertNotIn("android: installed", result.stdout)

    def test_explicit_android_install_keeps_target_on_linux_and_macos(self):
        for host in ("Linux", "Darwin"):
            with self.subTest(host=host):
                self.log.write_text("")
                result = self.run_build("android", "--install", FAKE_HOST=host)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("targets: android\n", result.stdout)
                self.assertEqual(self.install_calls(), [[
                    "-s", "phone-1", "install", "-r",
                    str(self.root / "dist/earnest-debug.apk"),
                ]])
                self.assertEqual((self.root / "dist/earnest-debug.apk").read_text(), "fixture apk")
                self.assertIn("android: installed -> phone-1", result.stdout)
                self.assertEqual(self.calls("xcodegen"), [])

    def test_bare_install_selects_both_native_apps(self):
        for host in ("Linux", "Darwin"):
            with self.subTest(host=host):
                self.log.write_text("")
                result = self.run_build("--install", FAKE_HOST=host)
                self.assertIn("targets: mac android\n", result.stdout)
                self.assertIn("android: installed -> phone-1", result.stdout)
                self.assertEqual(len(self.install_calls()), 1)
                if host == "Linux":
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn("mac: skipped", result.stdout)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(self.calls("xcodegen"), [["generate"]])
                self.assertEqual(self.calls("pnpm"), [])
                self.assertEqual(self.calls("go"), [])
                self.assertEqual(self.calls("docker"), [])

    def test_bare_install_installs_both_apps_on_macos(self):
        applications = self.root / "Applications"
        applications.mkdir()
        # Redirect every Mac install path in the disposable script. Hide the
        # system registration tool as well, so no host application is touched.
        script = self.root / "scripts/build.sh"
        script.write_text(script.read_text().replace(
            "/Applications", str(applications),
        ).replace(
            "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister",
            str(self.root / "unused-lsregister"),
        ))
        result = self.run_build("--install", FAKE_HOST="Darwin", FAKE_MAC_BUILD="1",
                                FAKE_APPLICATIONS=str(applications))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((applications / "Earnest.app/fixture").read_text(), "fixture app")
        self.assertIn("mac: installed", result.stdout)
        self.assertIn("android: installed -> phone-1", result.stdout)
        self.assertEqual(len(self.install_calls()), 1)
        self.assertEqual(self.calls("open"), [["-R", str(applications / "Earnest.app")]])

    def test_explicit_mac_install_does_not_add_android(self):
        result = self.run_build("mac", "--install", FAKE_HOST="Darwin")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("targets: mac\n", result.stdout)
        self.assertEqual(self.calls("gradlew"), [])
        self.assertEqual(self.calls("adb"), [])

    def test_android_build_without_install_never_uses_adb(self):
        result = self.run_build("android")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.calls("adb"), [])

    def test_sdk_adb_is_preferred_over_path(self):
        result = self.run_build("android", "--install")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = [json.loads(line) for line in self.log.read_text().splitlines()]
        self.assertEqual([call["path"] for call in calls if call["tool"] == "adb"],
                         [str(self.sdk_adb), str(self.sdk_adb)])

    def test_adb_falls_back_to_path(self):
        self.sdk_adb.unlink()
        result = self.run_build("android", "--install")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(len(self.install_calls()), 1)

    def test_missing_adb_reports_install_failure(self):
        self.sdk_adb.unlink()
        (self.tools / "adb").unlink()
        result = self.run_build("android", "--install")
        self.assert_install_failed(result)
        self.assertIn("adb", result.stderr)

    def test_no_devices_skips_install_successfully(self):
        result = self.run_build("android", "--install", FAKE_ADB_DEVICES="")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("android: install skipped", result.stdout)
        self.assertNotIn("android: installed", result.stdout)
        self.assertEqual(self.install_calls(), [])

    def test_unready_devices_fail_with_actionable_message(self):
        for state in ("unauthorized", "offline"):
            with self.subTest(state=state):
                result = self.run_build("android", "--install",
                                        FAKE_ADB_DEVICES="phone-1\t" + state)
                self.assert_install_failed(result)
                self.assertIn("debugging", result.stderr)
                self.assertEqual(self.install_calls(), [])

    def test_multiple_ready_devices_require_selection(self):
        result = self.run_build("android", "--install",
                                FAKE_ADB_DEVICES="phone-1\tdevice\nphone-2\tdevice")
        self.assert_install_failed(result)
        self.assertIn("ANDROID_SERIAL", result.stderr)
        self.assertEqual(self.install_calls(), [])

    def test_android_serial_selects_one_ready_device(self):
        result = self.run_build("android", "--install", ANDROID_SERIAL="phone-2",
                                FAKE_ADB_DEVICES="phone-1\tdevice\nphone-2\tdevice")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.install_calls()[0][:2], ["-s", "phone-2"])

    def test_missing_or_unready_selected_device_does_not_install_elsewhere(self):
        for selected_state in ("", "phone-2\toffline", "phone-2\tunauthorized"):
            with self.subTest(selected_state=selected_state):
                result = self.run_build("android", "--install", ANDROID_SERIAL="phone-2",
                                        FAKE_ADB_DEVICES="phone-1\tdevice\n" + selected_state)
                self.assert_install_failed(result)
                self.assertIn("ANDROID_SERIAL", result.stderr)
                self.assertEqual(self.install_calls(), [])

    def test_device_enumeration_failure_does_not_install(self):
        result = self.run_build("android", "--install", FAKE_ADB_DEVICES_FAIL="1")
        self.assert_install_failed(result)
        self.assertEqual(self.install_calls(), [])

    def test_adb_install_failure_fails_the_run(self):
        result = self.run_build("android", "--install", FAKE_ADB_INSTALL_FAIL="1")
        self.assert_install_failed(result)
        self.assertEqual(len(self.install_calls()), 1)

    def test_build_or_stage_failure_never_installs_stale_apk(self):
        stale = self.root / "dist/earnest-debug.apk"
        for failure in ("FAKE_GRADLE_FAIL", "FAKE_NO_APK", "FAKE_APK_STAGE_FAIL"):
            with self.subTest(failure=failure):
                stale.parent.mkdir(exist_ok=True)
                stale.write_text("stale apk")
                shutil.rmtree(self.root / "clients/android/app", ignore_errors=True)
                result = self.run_build("android", "--install", **{failure: "1"})
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(self.calls("adb"), [])
                self.assertNotIn("android: installed", result.stdout)


if __name__ == "__main__":
    unittest.main()
