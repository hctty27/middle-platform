import mixed from './mixed.js';
import { constantRateOptions } from '../lib/config.js';
import { summaryOutputs } from '../lib/report.js';

export const options = constantRateOptions(5, '30m');

export default function () {
  mixed();
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
