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
        self.assertEqual(implemented - {"core-payment-notify"}, {x["id"] for x in entries})
        # First-time payment is a distinct workload case sharing the same HTTP route
        # as the existing catalogued duplicate-callback contract, not a new API.
        self.assertIn("core-payment-notify", implemented)
        workload=json.loads((ROOT/"tests/load/target-workload.json").read_text())
        self.assertEqual(workload["scale"], 0.25)
        self.assertEqual(workload["full_target_qps"]["payment"], 2000)
        self.assertEqual(workload["categories"]["payment"], ["core-payment-notify"])
        self.assertIn("core-payment-notify", SCRIPT.read_text())
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
        self.assertIn("ENDPOINT_BUSINESS_ONLY", workflow)
        setup=(ROOT/"tests/load/setup.sh").read_text()
        service_config=(ROOT/"sdk-service/configs/config.yaml").read_text()
        self.assertIn("SDK_SERVICE_CACHE_TTL", workflow)
        self.assertIn("SDK_SERVICE_CACHE_TTL", setup)
        self.assertIn("LOAD_PROFILE", setup)
        self.assertIn("ENDPOINT_BUSINESS_ONLY", setup)
        self.assertIn("sdk-service-cache-ttl.txt", setup)
        self.assertIn("configCache:", service_config)
        self.assertIn("  ttl: 0s", service_config)
        self.assertIn("configCache:", setup)
        self.assertIn("ttl: 1s", setup)
        self.assertIn('"/admin/" not in c["route"]', py)
        catalog=json.loads(CATALOG.read_text())
        read=[entry for entry in catalog if entry["group"] == "read"]
        non_admin=[entry for entry in read if "/admin/" not in entry["route"].split(" ", 1)[1]]
        self.assertEqual(len(non_admin),13)
        self.assertTrue(all("/admin/" not in item["route"] for item in non_admin))
        self.assertEqual(len(read)-len(non_admin),19)
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
        recharge=(ROOT/"tests/load/recharge-qps.py").read_text()
        self.assertIn('LOAD_SCENARIO="payment"', recharge)
        self.assertIn('"first_recharge"', recharge)
        self.assertIn('"duplicate_payment_callback"', recharge)
        self.assertIn("payment_status=1", recharge)
        self.assertIn("20, 50, 100, 250", recharge)
        self.assertIn("50, 200, 500", recharge)
        self.assertIn("recharge-qps.py", workflow)
        self.assertIn("payment_exit", workflow)
        self.assertIn("RUNNER_ENVIRONMENT", recharge)
        self.assertNotIn("mp.skybluek.com", recharge)
        self.assertNotIn("https://", recharge)


if __name__=="__main__":
    unittest.main()
