function metric(data, name) {
  return data.metrics && data.metrics[name] ? data.metrics[name].values : null;
}

export function summaryOutputs(data) {
  const runId = (__ENV.LOADTEST_RUN_ID || String(Date.now())).replace(/[^A-Za-z0-9_-]/g, '-');
  const scenario = (__ENV.LOADTEST_SCENARIO || 'load').replace(/[^A-Za-z0-9_-]/g, '-');
  const report = {
    generatedAt: new Date().toISOString(),
    runId: runId,
    scenario: scenario,
    baseUrl: __ENV.BASE_URL || '',
    metrics: {
      http_req_duration: metric(data, 'http_req_duration'),
      http_req_failed: metric(data, 'http_req_failed'),
      http_reqs: metric(data, 'http_reqs'),
      iterations: metric(data, 'iterations'),
      dropped_iterations: metric(data, 'dropped_iterations'),
      checks: metric(data, 'checks'),
    },
    thresholds: {},
  };

  Object.keys(data.metrics || {}).forEach(function (name) {
    const thresholds = data.metrics[name].thresholds;
    if (thresholds) {
      report.thresholds[name] = thresholds;
    }
  });

  const duration = report.metrics.http_req_duration || {};
  const failed = report.metrics.http_req_failed || {};
  const rps = report.metrics.http_reqs || {};
  const output = [
    'Load test: ' + scenario,
    'Run ID: ' + runId,
    'HTTP RPS: ' + (rps.rate === undefined ? 'n/a' : rps.rate.toFixed(2)),
    'HTTP failed rate: ' + (failed.rate === undefined ? 'n/a' : (failed.rate * 100).toFixed(3) + '%'),
    'HTTP p95: ' + (duration['p(95)'] === undefined ? 'n/a' : duration['p(95)'].toFixed(2) + ' ms'),
    'HTTP p99: ' + (duration['p(99)'] === undefined ? 'n/a' : duration['p(99)'].toFixed(2) + ' ms'),
    '',
  ].join('\n');

  const filename = 'reports/' + scenario + '-' + runId + '.summary.json';
  return {
    [filename]: JSON.stringify(report, null, 2),
    stdout: output,
  };
}
