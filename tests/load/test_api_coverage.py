"""Fail CI when a registered HTTP route lacks a runner-only test case."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]
ROUTERS = (
    "config-center/internal/api/router.go",
    "config-center-access/internal/api/router.go",
    "sdk-core/internal/api/router.go",
    "sdk-core/internal/api/order.go",
    "sdk-core/internal/api/admin_channel.go",
    "sdk-core/internal/api/admin_node.go",
    "sdk-core/internal/api/admin_order.go",
    "sdk-core/internal/api/admin_service.go",
    "sdk-service/internal/api/router.go",
)
CHI = re.compile(r'\br\.(Get|Post|Put|Patch|Delete)\("(/[^"]+)"')
SERVEMUX = re.compile(r'"((?:GET|POST|PUT|PATCH|DELETE) /(?:api|health|internal)/[^"]+)"')
COVERED = re.compile(r"\b(?:test|get|write)\('((?:GET|POST|PUT|PATCH|DELETE) [^']+)'")

def registered_routes():
    result = set()
    for name in ROUTERS:
        source = (ROOT / name).read_text(encoding="utf-8")
        result.update(method.upper() + " " + path for method, path in CHI.findall(source))
        result.update(SERVEMUX.findall(source))
    return result

def tested_routes():
    code = (ROOT / "tests/load/api-coverage.js").read_text(encoding="utf-8")
    return set(COVERED.findall(code))

class TestAPICoverage(unittest.TestCase):
    def test_every_registered_route_has_runner_scenario(self):
        actual, covered = registered_routes(), tested_routes()
        self.assertTrue(actual, "route discovery unexpectedly empty")
        self.assertEqual(actual, covered, "Go HTTP routes and k6 coverage drifted")

    def test_no_external_target_in_full_api_suite(self):
        code = (ROOT / "tests/load/api-coverage.js").read_text(encoding="utf-8")
        self.assertIn("http://127.0.0.1:8280", code)
        self.assertIn("http://127.0.0.1:8081", code)
        self.assertNotIn("https://", code)
        self.assertNotIn("mp.skybluek.com", code)

if __name__ == "__main__":
    unittest.main()
