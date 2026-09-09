import pathlib
import unittest


class NonDestructiveStartPatchTest(unittest.TestCase):
    def test_normal_deploy_does_not_add_wipe_data(self):
        patch_path = pathlib.Path(__file__).with_name("patches") / "non_destructive_start.patch"
        patch = patch_path.read_text(encoding="utf-8")

        self.assertIn('-        wipe_arg = "-wipe-data" if not self.is_initialized() else ""', patch)
        self.assertIn('+        wipe_arg = ""', patch)
        self.assertNotIn('+        wipe_arg = "-wipe-data"', patch)


if __name__ == "__main__":
    unittest.main()
