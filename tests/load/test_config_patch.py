import pathlib
import unittest

from config_patch import core_settings


class ConfigPatchTests(unittest.TestCase):
    def test_actual_template_keeps_http_pool(self):
        path = pathlib.Path(__file__).resolve().parents[2] / "sdk-core/configs/config.yaml"
        text = path.read_text()
        changed = core_settings(text, True, 32, 16)
        self.assertIn("  maxOpenConns: 32\n  maxIdleConns: 16\n", changed)
        self.assertIn("  accessEnabled: true\n", changed)
        self.assertEqual(text.split("orderNotify:\n", 1)[1].split("configCache:\n", 1)[0],
                         changed.split("orderNotify:\n", 1)[1].split("configCache:\n", 1)[0])
        self.assertEqual(core_settings(changed, False, 20, 10), text)

    def test_missing_and_duplicate_fail(self):
        fixture = "database:\n  maxOpenConns: 20\n  maxIdleConns: 10\nlog:\n  accessEnabled: false\n"
        with self.assertRaises(RuntimeError):
            core_settings(fixture.replace("  maxIdleConns: 10\n", ""), True, 32, 16)
        with self.assertRaises(RuntimeError):
            core_settings(fixture + "  accessEnabled: true\n", True, 32, 16)
        with self.assertRaises(RuntimeError):
            core_settings(fixture + "database:\n", True, 32, 16)
        with self.assertRaises(ValueError):
            core_settings(fixture, 10, 32, 16)


if __name__ == "__main__":
    unittest.main()
