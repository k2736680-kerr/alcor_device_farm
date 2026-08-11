import unittest

from sdk_catalog import available_entries


class CatalogParserTest(unittest.TestCase):
    def test_uses_available_revision_instead_of_installed_revision(self):
        output = """
Installed packages:
  Path | Version | Description | Location
  system-images;android-36;google_apis;x86_64 | 10 | old | old
Available Packages:
  Path | Version | Description
  system-images;android-36;google_apis;x86_64 | 12 | current
  system-images;android-35;google_play;x86_64 | 9 | current
Available Updates:
  ID | Installed | Available
"""
        entries = available_entries(output)
        self.assertEqual([35, 36], [entry["api_level"] for entry in entries])
        self.assertEqual("12", entries[1]["revision"])

    def test_rejects_out_of_scope_packages(self):
        output = """
Available Packages:
  Path | Version | Description
  system-images;android-32;google_apis;x86_64 | 1 | too old
  system-images;android-36;default;x86_64 | 1 | wrong type
  system-images;android-36;google_apis;arm64-v8a | 1 | wrong abi
"""
        self.assertEqual([], available_entries(output))


if __name__ == "__main__":
    unittest.main()
