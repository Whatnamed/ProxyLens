/**
 * run-accounting-reconciliation.mjs
 * 
 * 批量分析 Stage A3 的 12 个会话与长连接/基线会话的 Global Accounting 与 /traffic 近似积分
 */

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { analyzeSessionAccounting } from '../analyze-accounting.mjs';

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

async function main() {
  const results = [];

  for (const t of trials) {
    const matchedDirs = allDirs.filter(d => d.startsWith(t.name + '-')).sort();
    if (matchedDirs.length === 0) continue;
    const sessionDir = path.join(captureBase, matchedDirs[matchedDirs.length - 1]);
    const res = await analyzeSessionAccounting(sessionDir);
    results.push({
      trial: t.name,
      interval: t.interval,
      route: t.route,
      rep: t.rep,
      ...res
    });
  }

  // 写入 JSON 结果
  const outPath = path.join(projectRoot, 'tmp/discovery/work-package-a/accounting-summary.json');
  fs.writeFileSync(outPath, JSON.stringify(results, null, 2));

  console.log('========================================================================================================================');
  console.log('STAGE R2: HIERARCHICAL ACCOUNTING & RECONCILIATION SUMMARY (ALL 12 TRIALS)');
  console.log('========================================================================================================================');
  console.log('Trial Name        | Intv   | Global Up (B) | Known App Up| Unique Obs Up | Res Up (%) | Global Down(B)| Unique Obs Dn | Res Down(%)| /traffic Diff Up/Down');
  console.log('------------------+--------+---------------+-------------+---------------+------------+---------------+---------------+------------+-----------------------');

  for (const r of results) {
    const trial = r.trial.padEnd(17);
    const intv = r.interval.padEnd(6);
    const gUp = String(r.globalCounters.globalUploadDelta).padStart(13);
    const kAppUp = String(r.hierarchicalAttribution.knownApplicationUpload).padStart(11);
    const uObsUp = String(r.hierarchicalAttribution.uniqueObservedUpload).padStart(13);
    const resUpPct = (r.residual.residualUploadPct.toFixed(1) + '%').padStart(10);

    const gDown = String(r.globalCounters.globalDownloadDelta).padStart(13);
    const uObsDown = String(r.hierarchicalAttribution.uniqueObservedDownload).padStart(13);
    const resDownPct = (r.residual.residualDownloadPct.toFixed(1) + '%').padStart(10);

    const diffUp = r.trafficRateIntegration.differenceVsGlobalPctUp !== null ? r.trafficRateIntegration.differenceVsGlobalPctUp.toFixed(1) : 'N/A';
    const diffDown = r.trafficRateIntegration.differenceVsGlobalPctDown !== null ? r.trafficRateIntegration.differenceVsGlobalPctDown.toFixed(1) : 'N/A';
    const trafficDiff = `${r.trafficRateIntegration.differenceVsGlobalPctUp > 0 ? '+' : ''}${diffUp}% / ${r.trafficRateIntegration.differenceVsGlobalPctDown > 0 ? '+' : ''}${diffDown}%`.padStart(21);

    console.log(`${trial} | ${intv} | ${gUp} | ${kAppUp} | ${uObsUp} | ${resUpPct} | ${gDown} | ${uObsDown} | ${resDownPct} | ${trafficDiff}`);
  }
  console.log('========================================================================================================================\n');

  // 按 interval 聚合分析残差与流量
  const intvStats = {};
  for (const r of results) {
    if (!intvStats[r.interval]) {
      intvStats[r.interval] = {
        gUp: 0, kAppUp: 0, uObsUp: 0, resUp: 0,
        gDown: 0, uObsDown: 0, resDown: 0,
        intTrafficUp: 0, intTrafficDown: 0
      };
    }
    const s = intvStats[r.interval];
    s.gUp += r.globalCounters.globalUploadDelta;
    s.kAppUp += r.hierarchicalAttribution.knownApplicationUpload;
    s.uObsUp += r.hierarchicalAttribution.uniqueObservedUpload;
    s.resUp += r.residual.residualUpload;
    s.gDown += r.globalCounters.globalDownloadDelta;
    s.uObsDown += r.hierarchicalAttribution.uniqueObservedDownload;
    s.resDown += r.residual.residualDownload;
    s.intTrafficUp += r.trafficRateIntegration.estimatedUploadBytes;
    s.intTrafficDown += r.trafficRateIntegration.estimatedDownloadBytes;
  }

  console.log('AGGREGATED INTERVAL HIERARCHICAL ACCOUNTING & APPROXIMATE RECONCILIATION:');
  for (const [intv, s] of Object.entries(intvStats)) {
    const resUpPct = ((s.resUp / s.gUp) * 100).toFixed(1);
    const resDownPct = ((s.resDown / s.gDown) * 100).toFixed(1);
    const diffUpPct = (((s.intTrafficUp - s.gUp) / s.gUp) * 100).toFixed(1);
    const diffDownPct = (((s.intTrafficDown - s.gDown) / s.gDown) * 100).toFixed(1);

    console.log('------------------------------------------------------------------------------------------------------------------------');
    console.log(`Interval: ${intv.padEnd(8)} | Global Up: ${s.gUp.toLocaleString()}B, Unique Obs Up: ${s.uObsUp.toLocaleString()}B -> Residual Up: ${resUpPct.padStart(5)}% | Approx /traffic Int Diff Up: ${diffUpPct.padStart(5)}%`);
    console.log(`                  | Global Down: ${s.gDown.toLocaleString()}B, Unique Obs Down: ${s.uObsDown.toLocaleString()}B -> Residual Down: ${resDownPct.padStart(5)}% | Approx /traffic Int Diff Down: ${diffDownPct.padStart(5)}%`);
  }
  console.log('------------------------------------------------------------------------------------------------------------------------\n');
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
