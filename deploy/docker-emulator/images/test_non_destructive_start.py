import pathlib
import unittest


class NonDestructiveStartPatchTest(unittest.TestCase):
    def test_normal_deploy_does_not_add_wipe_data(self):
        patch_path = pathlib.Path(__file__).with_name("patches") / "non_destructive_start.patch"
        patch = patch_path.read_text(encoding="utf-8")

        self.assertIn('-        wipe_arg = "-wipe-data" if not self.is_initialized() else ""', patch)
        self.assertIn('+        wipe_arg = ""', patch)
        self.assertNotIn('+        wipe_arg = "-wipe-data"', patch)
        self.assertIn('+            expected_device = self.device.lower().replace(" ", "_")', patch)
        self.assertIn('re.escape(expected_device)', patch)
        self.assertIn('+    def _ensure_avd_reference(self) -> None:', patch)
        self.assertIn('+            reference.write(f"path={self.path_emulator}\\n")', patch)
        self.assertEqual(patch.count('self._ensure_avd_reference()'), 2)
        self.assertIn('+    def _remove_stale_avd_locks(self) -> None:', patch)
        self.assertIn('+            if b"qemu-system" in command and expected_name in command:', patch)
        self.assertIn('+                raise RuntimeError(f"emulator \'{self.name}\' is already running")', patch)
        self.assertIn('+                if not file_name.endswith(".lock"):', patch)
        self.assertIn('+        self._remove_stale_avd_locks()', patch)

        dockerfile = pathlib.Path(__file__).with_name("Dockerfile").read_text(encoding="utf-8")
        self.assertIn("ANDROID_AVD_HOME=${WORK_PATH}/.android/avd", dockerfile)


if __name__ == "__main__":
    unittest.main()
