"""Make route drift, fixture omissions or unsafe QPS targets fail CI."""
import json
from pathlib import Path
import re
import unittest

from test_api_coverage import registered_routes

ROOT=Path(__file__).resolve().parents[2]
CATALOG=ROOT/"tests/load/endpoint-qps-catalog.json"
SCRIPT=ROOT/"tests/load/endpoint-qps.js"
PYTHON=ROOT/"tests/load/endpoint-qps.py"
WORKFLOW=ROOT/".github/workflows/load-test.yml"

class TestEndpointQPS(unittest.TestCase):
    def test_every_registered_http_route_has_an_independent_case(self):
        entries=json.loads(CATALOG.read_text())
        self.assertEqual(len(entries),52)
        self.assertEqual({x["route"] for x in entries},registered_routes())
        self.assertEqual(len({x["id"] for x in entries}),len(entries))
        js=SCRIPT.read_text()
        implemented=set(re.findall(r"case '([^']+)':",js))
        self.assertEqual(implemented,{x["id"] for x in entries})
        for item in entries:
            self.assertIn(item["group"],("read","write"))
            self.assertIn(item["mode"],("steady","delete","duplicate","reject"))
            self.assertIn(item["expected"],(200,201,202,204,409))
            if item["route"].startswith("DELETE "):
                self.assertEqual(item["mode"],"delete")

    def test_runner_only_no_external_control(self):
        js=SCRIPT.read_text()
        py=PYTHON.read_text()
        workflow=WORKFLOW.read_text()
        self.assertIn("http://127.0.0.1:8280",js)
        self.assertIn("http://127.0.0.1:8081",js)
        self.assertIn("RUNNER_ENVIRONMENT",py)
        self.assertIn("github-hosted",py)
        self.assertIn("workflow_dispatch:",workflow)
        self.assertIn("ubuntu-24.04",workflow)
        self.assertNotIn("mp.skybluek.com",js+py+workflow)
        self.assertNotIn("https://",js)
        self.assertIn("seed_delete",py)
        self.assertIn("api-coverage.js",py)
    def test_high_qps_diagnostics_are_loopback_only_and_do_not_skip_hmac(self):
        diagnostic_js=(ROOT/"tests/load/qps-diagnostics.js").read_text()
        diagnostic_py=(ROOT/"tests/load/qps-diagnostics.py").read_text()
        workflow=WORKFLOW.read_text()
        mock=(ROOT/"tests/load/mock.go").read_text()
        self.assertIn("http://127.0.0.1:8290",diagnostic_js)
        self.assertIn("http://127.0.0.1:8211",diagnostic_js)
        self.assertIn("http://127.0.0.1:8201",diagnostic_js)
        self.assertIn("http://127.0.0.1:8081",diagnostic_js)
        self.assertNotIn("https://",diagnostic_js)
        self.assertIn("crypto.hmac('sha256'",diagnostic_js)
        self.assertIn("diag_http_409",diagnostic_js)
        endpoint_js=SCRIPT.read_text()
        endpoint_py=PYTHON.read_text()
        self.assertLess(endpoint_js.index("if (!kind)"), endpoint_js.index("randomStamp()"))
        self.assertIn("endpoint_http_429",endpoint_js)
        self.assertIn("| 401 | 409 | 429 | 503 | 5xx |",endpoint_py)
        self.assertIn('"application_rate_limit_429"',endpoint_py)
        self.assertIn('"preallocated_vus":vus',endpoint_py)
        self.assertIn("RUNNER_ENVIRONMENT",diagnostic_py)
        self.assertIn("qps-diagnostics.py",workflow)
        self.assertIn("[HIGH-READ-QPS]",workflow)
        self.assertIn("GET /load/bench/ping",mock)


if __name__=="__main__":
    unittest.main()
