/**
 * audit-capture-integrity.mjs
 * 
 * Stage R1: 遍历已有 capture matrix 全部 12 个 trials，
 * 验证：
 * 1. windowViolations === 0
 * 2. oneToOneConflicts === 0
 * 3. ambiguous === 0
 * 4. routeMismatches === 0
 * 5. routeUnknown === 0
 * 
 * 生成机器可读的 r1-capture-integrity.json 报告。
 */

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { analyzeCaptureSession } from '../analyze-capture-rate.mjs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

async function main() {
  const matrixDir = path.join(projectRoot, 'tmp/discovery/work-package-a/capture');
  const outDir = path.join(projectRoot, 'tmp/discovery/work-package-b1');
  fs.mkdirSync(outDir, { recursive: true });

  const trialPrefixes = [
    { prefix: 'direct-default-r1', route: 'direct' },
    { prefix: 'direct-default-r2', route: 'direct' },
    { prefix: 'proxy-default-r1', route: 'proxy' },
    { prefix: 'proxy-default-r2', route: 'proxy' },
    { prefix: 'direct-500-r1', route: 'direct' },
    { prefix: 'direct-500-r2', route: 'direct' },
    { prefix: 'proxy-500-r1', route: 'proxy' },
    { prefix: 'proxy-500-r2', route: 'proxy' },
    { prefix: 'direct-250-r1', route: 'direct' },
    { prefix: 'direct-250-r2', route: 'direct' },
    { prefix: 'proxy-250-r1', route: 'proxy' },
    { prefix: 'proxy-250-r2', route: 'proxy' }
  ];

  const allEntries = fs.readdirSync(matrixDir);

  console.log('================================================================');
  console.log('STAGE R1: CAPTURE MATRIX INTEGRITY CLOSURE AUDIT');
  console.log('================================================================');

  const trialResults = [];
  let validTrialsCount = 0;
  let invalidTrialsCount = 0;

  for (const t of trialPrefixes) {
    // 寻找以 prefix 开头的最新目录
    const matchingDirs = allEntries.filter(e => e.startsWith(t.prefix)).sort();
    if (matchingDirs.length === 0) {
      console.error(`[MISSING] Trial session not found for prefix: ${t.prefix}`);
      invalidTrialsCount++;
      trialResults.push({ name: t.prefix, status: 'MISSING' });
      continue;
    }
    const matchedDirName = matchingDirs[matchingDirs.length - 1];
    const sessionDir = path.join(matrixDir, matchedDirName);
    const gtFile = path.join(sessionDir, 'ground-truth.ndjson');

    if (!fs.existsSync(sessionDir) || !fs.existsSync(gtFile)) {
      console.error(`[MISSING] Trial session files not found: ${matchedDirName}`);
      invalidTrialsCount++;
      trialResults.push({ name: matchedDirName, status: 'MISSING' });
      continue;
    }

    const res = await analyzeCaptureSession(sessionDir, gtFile, { expectedRoute: t.route });

    const isHealthy = 
      res.windowViolations === 0 &&
      res.oneToOneConflicts === 0 &&
      res.ambiguous === 0 &&
      res.routeVerification.routeMismatches === 0 &&
      res.routeVerification.routeUnknown === 0;

    if (isHealthy) {
      validTrialsCount++;
    } else {
      invalidTrialsCount++;
    }

    trialResults.push({
      trial: t.prefix,
      dirName: matchedDirName,
      expectedRoute: t.route.toUpperCase(),
      intervalMs: res.requestedIntervalMs,
      eligibleConnectedCount: res.eligibleConnectedCount,
      matched: res.matched,
      missed: res.missed,
      ambiguous: res.ambiguous,
      oneToOneConflicts: res.oneToOneConflicts,
      windowViolations: res.windowViolations,
      routeVerification: res.routeVerification,
      isHealthy,
      captureRateEligible: res.captureRateEligible,
      presenceDist: res.presenceDist
    });

    console.log(`Trial: ${t.prefix.padEnd(18)} | Exp: ${t.route.toUpperCase().padEnd(6)} | Matched: ${String(res.matched).padStart(2)}/50 | WinViol: ${res.windowViolations} | 1-to-1 Conf: ${res.oneToOneConflicts} | RouteMismatch: ${res.routeVerification.routeMismatches} | Gate: ${isHealthy ? 'PASS' : 'FAIL'}`);
  }

  const integrityReport = {
    auditDate: new Date().toISOString(),
    totalTrials: trialPrefixes.length,
    validTrials: validTrialsCount,
    invalidTrials: invalidTrialsCount,
    allGatesPassed: invalidTrialsCount === 0,
    trials: trialResults
  };

  const reportPath = path.join(outDir, 'r1-capture-integrity.json');
  fs.writeFileSync(reportPath, JSON.stringify(integrityReport, null, 2));

  console.log('----------------------------------------------------------------');
  console.log(`AUDIT RESULT: Total=${trialPrefixes.length}, Valid=${validTrialsCount}, Invalid=${invalidTrialsCount}`);
  console.log(`Integrity Report written to: ${reportPath}`);
  console.log('================================================================\n');

  if (invalidTrialsCount > 0) {
    console.error('[GATE FAILED] Some trials failed the integrity check!');
    process.exit(1);
  }
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
