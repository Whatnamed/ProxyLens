/**
 * summarize-matrix.mjs
 * 
 * 扫描并汇总 Stage A3 的 12 个 Capture-Rate Matrix trial 结果
 */

import fs from 'fs';
import path from 'path';
import { execSync } from 'child_process';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

const captureBase = path.join(projectRoot, 'tmp/discovery/work-package-a/capture');
const trials = [
  { name: 'direct-default-r1', interval: 'default', route: 'direct', rep: 1 },
  { name: 'direct-default-r2', interval: 'default', route: 'direct', rep: 2 },
  { name: 'proxy-default-r1',  interval: 'default', route: 'proxy',  rep: 1 },
  { name: 'proxy-default-r2',  interval: 'default', route: 'proxy',  rep: 2 },
  { name: 'direct-500-r1',     interval: '500',     route: 'direct', rep: 1 },
  { name: 'direct-500-r2',     interval: '500',     route: 'direct', rep: 2 },
  { name: 'proxy-500-r1',      interval: '500',     route: 'proxy',  rep: 1 },
  { name: 'proxy-500-r2',      interval: '500',     route: 'proxy',  rep: 2 },
  { name: 'direct-250-r1',     interval: '250',     route: 'direct', rep: 1 },
  { name: 'direct-250-r2',     interval: '250',     route: 'direct', rep: 2 },
  { name: 'proxy-250-r1',      interval: '250',     route: 'proxy',  rep: 1 },
  { name: 'proxy-250-r2',      interval: '250',     route: 'proxy',  rep: 2 },
];

const allDirs = fs.readdirSync(captureBase).filter(d => {
  try {
    return fs.statSync(path.join(captureBase, d)).isDirectory();
  } catch {
    return false;
  }
});

const summary = [];

for (const t of trials) {
  const matchedDirs = allDirs.filter(d => d.startsWith(t.name + '-')).sort();
  if (matchedDirs.length === 0) {
    console.error('Missing directory for trial:', t.name);
    continue;
  }
  const sessionDir = path.join(captureBase, matchedDirs[matchedDirs.length - 1]);
  const groundTruthFile = path.join(sessionDir, 'ground-truth.ndjson');
  
  const cmd = `node "${path.join(projectRoot, 'tools/discovery/analyze-capture-rate.mjs')}" "${sessionDir}" "${groundTruthFile}" --expected-route "${t.route}" --json`;
  const out = execSync(cmd, { encoding: 'utf8' });
  const result = JSON.parse(out);
  
  summary.push({
    trial: t.name,
    intervalName: t.interval,
    routeName: t.route,
    repeat: t.rep,
    sessionDir: sessionDir,
    ...result
  });
}

fs.writeFileSync(path.join(captureBase, 'matrix-summary.json'), JSON.stringify(summary, null, 2));

console.log('========================================================================================================================');
console.log('STAGE A3: DIRECT / PROXY CAPTURE-RATE MATRIX RESULTS (ALL 12 TRIALS, N=600)');
console.log('========================================================================================================================');
console.log('Trial Name        | Intv (ms) | Route  | Rep | Eligible | Matched | Missed | Ambiguous | Capture Rate | 1 Frame | 2 Frames | 3+ Frames');
console.log('------------------+-----------+--------+-----+----------+---------+--------+-----------+-------------+---------+----------+----------');

summary.forEach(s => {
  const intv = s.intervalName.padEnd(9);
  const route = s.routeName.padEnd(6);
  const rate = ((s.matched / s.eligibleConnectedCount) * 100).toFixed(1) + '%';
  const p1 = s.presenceDist.oneFrame;
  const p2 = s.presenceDist.twoFrames;
  const p3 = s.presenceDist.threePlusFrames;
  console.log(s.trial.padEnd(17) + ' | ' + intv + ' | ' + route + ' | ' + s.repeat + '   | ' + String(s.eligibleConnectedCount).padStart(8) + ' | ' + String(s.matched).padStart(7) + ' | ' + String(s.missed).padStart(6) + ' | ' + String(s.ambiguous).padStart(9) + ' | ' + rate.padStart(12) + ' | ' + String(p1).padStart(7) + ' | ' + String(p2).padStart(8) + ' | ' + String(p3).padStart(9));
});
console.log('========================================================================================================================\n');

// 汇总 Route x Interval
const routeInterval = {};
const intervalBins = {};

summary.forEach(s => {
  const key = s.routeName + ' @ ' + s.intervalName;
  if (!routeInterval[key]) {
    routeInterval[key] = { eligible: 0, matched: 0, missed: 0, ambiguous: 0, presence: { oneFrame: 0, twoFrames: 0, threePlusFrames: 0 } };
  }
  routeInterval[key].eligible += s.eligibleConnectedCount;
  routeInterval[key].matched += s.matched;
  routeInterval[key].missed += s.missed;
  routeInterval[key].ambiguous += s.ambiguous;
  routeInterval[key].presence.oneFrame += s.presenceDist.oneFrame;
  routeInterval[key].presence.twoFrames += s.presenceDist.twoFrames;
  routeInterval[key].presence.threePlusFrames += s.presenceDist.threePlusFrames;

  if (!intervalBins[s.intervalName]) {
    intervalBins[s.intervalName] = [
      { label: '<100ms', n: 0, matched: 0, missed: 0 },
      { label: '100–250ms', n: 0, matched: 0, missed: 0 },
      { label: '250–500ms', n: 0, matched: 0, missed: 0 },
      { label: '500–1000ms', n: 0, matched: 0, missed: 0 },
      { label: '>=1000ms', n: 0, matched: 0, missed: 0 },
    ];
  }
  s.durationBins.forEach((b, idx) => {
    intervalBins[s.intervalName][idx].n += b.n;
    intervalBins[s.intervalName][idx].matched += b.matched;
    intervalBins[s.intervalName][idx].missed += b.missed;
  });
});

console.log('AGGREGATED ROUTE x INTERVAL SUMMARY (N=100 per cell):');
console.log('------------------------------------------------------------------------------------------------------------------------');
for (const [key, stats] of Object.entries(routeInterval)) {
  const rate = ((stats.matched / stats.eligible) * 100).toFixed(1) + '%';
  console.log(key.padEnd(20) + ': N=' + String(stats.eligible).padStart(3) + ' | Matched=' + String(stats.matched).padStart(3) + ' | Missed=' + String(stats.missed).padStart(3) + ' | Ambiguous=' + stats.ambiguous + ' | Rate: ' + rate.padStart(6) + ' | Presence [1 frame: ' + String(stats.presence.oneFrame).padStart(2) + ', 2 frames: ' + String(stats.presence.twoFrames).padStart(2) + ', 3+ frames: ' + String(stats.presence.threePlusFrames).padStart(2) + ']');
}
console.log('------------------------------------------------------------------------------------------------------------------------\n');

for (const [intv, bins] of Object.entries(intervalBins)) {
  console.log('DURATION BINS FOR INTERVAL: ' + intv + ' (N=' + bins.reduce((a,b)=>a+b.n, 0) + ')');
  bins.forEach(b => {
    const rate = b.n > 0 ? ((b.matched / b.n) * 100).toFixed(1) + '%' : 'N/A';
    console.log('  ' + b.label.padEnd(12) + ': Total N=' + String(b.n).padStart(3) + ' | Matched=' + String(b.matched).padStart(3) + ' | Missed=' + String(b.missed).padStart(3) + ' | Capture Rate: ' + rate);
  });
  console.log('');
}
