import mixed from './mixed.js';
import { rampingOptions } from '../lib/config.js';
import { summaryOutputs } from '../lib/report.js';

export const options = rampingOptions(5, 200);

export default function () {
  mixed();
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
