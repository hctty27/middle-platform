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

    def test_main_nonce_high_qps_is_runner_only_and_reports_http_classes(self):
        script=SCRIPT.read_text()
        py=PYTHON.read_text()
        workflow=WORKFLOW.read_text()
        monitor=(ROOT/"tests/load/process_monitor.py").read_text()
        self.assertIn("ENDPOINT_RATES", workflow)
        self.assertIn("[NONCE-HIGH-QPS]", workflow)
        self.assertIn("'5000,10000,20000'", workflow)
        self.assertIn("ENDPOINT_VUS_CAP", workflow)
        self.assertIn("RUNNER_ENVIRONMENT", py)
        self.assertIn("config-center-access", py)
        self.assertIn("endpoint_http_409", script)
        self.assertIn("endpoint_http_429", script)
        self.assertIn("endpoint_http_503", script)
        self.assertIn('"http_409":http_409', py)
        self.assertIn('"preallocated_vus":vus', py)
        self.assertIn("config-center-access", monitor)
        self.assertIn("if (!kind)", script)
        self.assertNotIn("mp.skybluek.com", workflow+script+py)


if __name__=="__main__":
    unittest.main()
