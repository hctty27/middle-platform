export function requiredEnv(name) {
  const value = __ENV[name];
  if (value === undefined || value === '') {
    throw new Error('Missing required environment variable: ' + name);
  }
  return value;
}

export function optionalEnv(name, fallback) {
  const value = __ENV[name];
  return value === undefined || value === '' ? fallback : value;
}

export function baseUrl() {
  const value = requiredEnv('BASE_URL').replace(/\/+$/, '');
  if (!/^https:\/\/[^/]+$/i.test(value)) {
    throw new Error('BASE_URL must be an HTTPS origin without a path');
  }
  return value;
}

export function requireTestEnv(name) {
  const value = requiredEnv(name);
  if (value !== 'test') {
    throw new Error(name + ' must be "test" when targeting production');
  }
  return value;
}

function positiveInteger(name, fallback, maximum) {
  const value = Number(optionalEnv(name, String(fallback)));
  if (!Number.isInteger(value) || value < 1 || value > maximum) {
    throw new Error(name + ' must be an integer between 1 and ' + maximum);
  }
  return value;
}

function commonOptions() {
  return {
    summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
    thresholds: {
      http_req_failed: ['rate<0.01'],
      http_req_duration: ['p(95)<500', 'p(99)<1000'],
      dropped_iterations: ['count==0'],
    },
  };
}

export function rampingOptions(defaultRate, maximumRate) {
  const maxRate = positiveInteger('MAX_RPS', defaultRate || 5, maximumRate || 200);
  const preAllocatedVUs = positiveInteger('PREALLOCATED_VUS', 20, 2000);
  const maxVUs = positiveInteger('MAX_VUS', 100, 5000);
  if (maxVUs < preAllocatedVUs) {
    throw new Error('MAX_VUS must be greater than or equal to PREALLOCATED_VUS');
  }

  const targets = [5, 10, 20, maxRate].filter(function (value, index, all) {
    return value <= maxRate && all.indexOf(value) === index;
  });
  const stages = targets.map(function (target, index) {
    return { target: target, duration: index === 0 ? '2m' : '3m' };
  });
  stages.push({ target: maxRate, duration: optionalEnv('HOLD_DURATION', '5m') });
  stages.push({ target: 0, duration: '1m' });

  return Object.assign(commonOptions(), {
    scenarios: {
      load: {
        executor: 'ramping-arrival-rate',
        startRate: 1,
        timeUnit: '1s',
        preAllocatedVUs: preAllocatedVUs,
        maxVUs: maxVUs,
        stages: stages,
      },
    },
  });
}

export function constantRateOptions(defaultRate, defaultDuration) {
  const rate = positiveInteger('MAX_RPS', defaultRate || 1, 200);
  const preAllocatedVUs = positiveInteger('PREALLOCATED_VUS', 20, 2000);
  const maxVUs = positiveInteger('MAX_VUS', 100, 5000);
  if (maxVUs < preAllocatedVUs) {
    throw new Error('MAX_VUS must be greater than or equal to PREALLOCATED_VUS');
  }

  return Object.assign(commonOptions(), {
    scenarios: {
      steady: {
        executor: 'constant-arrival-rate',
        rate: rate,
        timeUnit: '1s',
        duration: optionalEnv('DURATION', defaultDuration || '5m'),
        preAllocatedVUs: preAllocatedVUs,
        maxVUs: maxVUs,
      },
    },
  });
}
