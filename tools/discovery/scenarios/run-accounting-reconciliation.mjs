/**
 * run-accounting-reconciliation.mjs
 * 
 * 批量分析 Stage A3 的 12 个会话与长连接/基线会话的 Global Accounting 与 /traffic 积分
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
  console.log('STAGE A4: GLOBAL ACCOUNTING & TRAFFIC RECONCILIATION SUMMARY (ALL 12 TRIALS)');
  console.log('========================================================================================================================');
  console.log('Trial Name        | Intv   | Global Up (B) | App Up (B)  | Res Up (%) | Global Down(B)| App Down(B) | Res Down(%)| /traffic Diff Up/Down');
  console.log('------------------+--------+---------------+-------------+------------+---------------+-------------+------------+-----------------------');

  for (const r of results) {
    const trial = r.trial.padEnd(17);
    const intv = r.interval.padEnd(6);
    const gUp = String(r.global.uploadDelta).padStart(13);
    const aUp = String(r.attributed.appUpload).padStart(11);
    const resUpPct = (r.residual.appResidualUploadPct.toFixed(1) + '%').padStart(10);

    const gDown = String(r.global.downloadDelta).padStart(13);
    const aDown = String(r.attributed.appDownload).padStart(11);
    const resDownPct = (r.residual.appResidualDownloadPct.toFixed(1) + '%').padStart(10);

    const trafficDiff = `${r.trafficIntegration.discrepancyUpPct > 0 ? '+' : ''}${r.trafficIntegration.discrepancyUpPct.toFixed(1)}% / ${r.trafficIntegration.discrepancyDownPct > 0 ? '+' : ''}${r.trafficIntegration.discrepancyDownPct.toFixed(1)}%`.padStart(21);

    console.log(`${trial} | ${intv} | ${gUp} | ${aUp} | ${resUpPct} | ${gDown} | ${aDown} | ${resDownPct} | ${trafficDiff}`);
  }
  console.log('========================================================================================================================\n');

  // 按 interval 聚合分析残差与流量
  const intvStats = {};
  for (const r of results) {
    if (!intvStats[r.interval]) {
      intvStats[r.interval] = {
        gUp: 0, aUp: 0, resUp: 0,
        gDown: 0, aDown: 0, resDown: 0,
        intTrafficUp: 0, intTrafficDown: 0
      };
    }
    const s = intvStats[r.interval];
    s.gUp += r.global.uploadDelta;
    s.aUp += r.attributed.appUpload;
    s.resUp += r.residual.appResidualUpload;
    s.gDown += r.global.downloadDelta;
    s.aDown += r.attributed.appDownload;
    s.resDown += r.residual.appResidualDownload;
    s.intTrafficUp += r.trafficIntegration.integratedUpload;
    s.intTrafficDown += r.trafficIntegration.integratedDownload;
  }

  console.log('AGGREGATED INTERVAL ACCOUNTING & RECONCILIATION:');
  console.log('------------------------------------------------------------------------------------------------------------------------');
  for (const [intv, s] of Object.entries(intvStats)) {
    const upResidualPct = s.gUp > 0 ? ((s.resUp / s.gUp) * 100).toFixed(1) + '%' : '0.0%';
    const downResidualPct = s.gDown > 0 ? ((s.resDown / s.gDown) * 100).toFixed(1) + '%' : '0.0%';
    const trafficUpDiff = s.gUp > 0 ? (((s.intTrafficUp - s.gUp) / s.gUp) * 100).toFixed(1) + '%' : '0.0%';
    const trafficDownDiff = s.gDown > 0 ? (((s.intTrafficDown - s.gDown) / s.gDown) * 100).toFixed(1) + '%' : '0.0%';

    console.log(`Interval: ${intv.padEnd(8)} | Global Up: ${s.gUp.toLocaleString().padStart(9)}B, App Attributed Up: ${s.aUp.toLocaleString().padStart(9)}B -> Residual Up: ${upResidualPct.padStart(6)} | Traffic Integration Diff Up: ${trafficUpDiff.padStart(6)}`);
    console.log(`                  | Global Down: ${s.gDown.toLocaleString().padStart(7)}B, App Attributed Down: ${s.aDown.toLocaleString().padStart(7)}B -> Residual Down: ${downResidualPct.padStart(6)} | Traffic Integration Diff Down: ${trafficDownDiff.padStart(6)}`);
    console.log('------------------------------------------------------------------------------------------------------------------------');
  }
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
