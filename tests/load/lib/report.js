function metric(data, name) {
  return data.metrics && data.metrics[name] ? data.metrics[name].values : null;
}

export function summaryOutputs(data) {
  const runId = (__ENV.LOADTEST_RUN_ID || String(Date.now())).replace(/[^A-Za-z0-9_-]/g, '-');
  const scenario = (__ENV.LOADTEST_SCENARIO || 'load').replace(/[^A-Za-z0-9_-]/g, '-');
  const metrics = {};
  const thresholds = {};

  Object.keys(data.metrics || {}).forEach(function (name) {
    metrics[name] = data.metrics[name].values || null;
    if (data.metrics[name].thresholds) {
      thresholds[name] = data.metrics[name].thresholds;
    }
  });

  const report = {
    generatedAt: new Date().toISOString(),
    runId: runId,
    scenario: scenario,
    baseUrl: __ENV.BASE_URL || '',
    loadProfile: {
      maxRps: Number(__ENV.MAX_RPS || 0),
      duration: __ENV.DURATION || null,
      mixIncludesSdkService: __ENV.MIX_INCLUDE_SERVICE === 'true',
    },
    metrics: metrics,
    thresholds: thresholds,
  };

  const duration = metrics.http_req_duration || {};
  const failed = metrics.http_req_failed || {};
  const rps = metrics.http_reqs || {};
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
