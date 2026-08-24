/**
 * audit-capture-integrity.mjs
 * 
 * Stage F3: 遍历 capture matrix 全部 12 个 trials，执行机械化全量门禁审计
 * 
 * 强制门禁规则:
 * 1. manifest 必须存在且 evidenceQuality.isHealthySession === true
 * 2. groundTruthTotal === 50 且 eligibleConnectedCount === 50 (动态分母验证)
 * 3. matched > 0 (防止 0 matched 导致假 PASS)
 * 4. windowViolations === 0
 * 5. oneToOneConflicts === 0
 * 6. ambiguous === 0
 * 7. routeVerification.routeMismatches === 0
 * 8. routeVerification.routeUnknown === 0
 */

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { analyzeCaptureSession } from '../analyze-capture-rate.mjs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

export async function auditCaptureIntegrity(options = {}) {
  const matrixDir = options.matrixDir || path.join(projectRoot, 'tmp/discovery/work-package-a/capture');
  const expectedRequestsPerTrial = options.expectedRequestsPerTrial || 50;

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

  if (!fs.existsSync(matrixDir)) {
    throw new Error(`Matrix directory not found: ${matrixDir}`);
  }

  const allEntries = fs.readdirSync(matrixDir);

  const trialResults = [];
  let validTrialsCount = 0;
  let invalidTrialsCount = 0;
  let totalEligibleCount = 0;
  let totalMatchedCount = 0;

  for (const t of trialPrefixes) {
    const matchingDirs = allEntries.filter(e => e.startsWith(t.prefix)).sort();
    if (matchingDirs.length === 0) {
      invalidTrialsCount++;
      trialResults.push({ name: t.prefix, isHealthy: false, error: 'Directory not found' });
      continue;
    }

    const matchedDirName = matchingDirs[matchingDirs.length - 1];
    const sessionDir = path.join(matrixDir, matchedDirName);
    const gtFile = path.join(sessionDir, 'ground-truth.ndjson');
    const manifestPath = path.join(sessionDir, 'manifest.json');

    if (!fs.existsSync(sessionDir) || !fs.existsSync(gtFile) || !fs.existsSync(manifestPath)) {
      invalidTrialsCount++;
      trialResults.push({ name: matchedDirName, isHealthy: false, error: 'Files missing' });
      continue;
    }

    let manifest = null;
    try {
      manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
    } catch {}

    const isManifestHealthy = (manifest?.evidenceQuality?.isHealthySession === true);

    const res = await analyzeCaptureSession(sessionDir, gtFile, { expectedRoute: t.route });

    const isDenominatorExact = (res.groundTruthTotal === expectedRequestsPerTrial) && (res.eligibleConnectedCount === expectedRequestsPerTrial);
    const hasMatchedConns = (res.matched > 0);
    const isRouteClean = (res.routeVerification.routeMismatches === 0) && (res.routeVerification.routeUnknown === 0);
    const isMappingClean = (res.windowViolations === 0) && (res.oneToOneConflicts === 0) && (res.ambiguous === 0);

    const isHealthy = isManifestHealthy && isDenominatorExact && hasMatchedConns && isRouteClean && isMappingClean;

    if (isHealthy) {
      validTrialsCount++;
    } else {
      invalidTrialsCount++;
    }

    totalEligibleCount += res.eligibleConnectedCount;
    totalMatchedCount += res.matched;

    trialResults.push({
      trial: t.prefix,
      dirName: matchedDirName,
      expectedRoute: t.route.toUpperCase(),
      intervalMs: res.requestedIntervalMs,
      groundTruthTotal: res.groundTruthTotal,
      eligibleConnectedCount: res.eligibleConnectedCount,
      matched: res.matched,
      missed: res.missed,
      ambiguous: res.ambiguous,
      oneToOneConflicts: res.oneToOneConflicts,
      windowViolations: res.windowViolations,
      routeVerification: res.routeVerification,
      isManifestHealthy,
      isDenominatorExact,
      hasMatchedConns,
      isHealthy,
      captureRateEligible: res.captureRateEligible,
      presenceDist: res.presenceDist
    });
  }

  return {
    auditDate: new Date().toISOString(),
    totalTrials: trialPrefixes.length,
    validTrials: validTrialsCount,
    invalidTrials: invalidTrialsCount,
    totalEligible: totalEligibleCount,
    totalMatched: totalMatchedCount,
    allGatesPassed: (invalidTrialsCount === 0) && (validTrialsCount === trialPrefixes.length) && (totalEligibleCount === trialPrefixes.length * expectedRequestsPerTrial),
    trials: trialResults
  };
}

// CLI 执行
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const outDir = path.join(projectRoot, 'tmp/discovery/work-package-b1');
  fs.mkdirSync(outDir, { recursive: true });

  console.log('================================================================');
  console.log('STAGE F3: CAPTURE MATRIX MECHANICAL INTEGRITY GATE AUDIT');
  console.log('================================================================');

  auditCaptureIntegrity()
    .then(report => {
      report.trials.forEach(t => {
        const exp = (t.expectedRoute || '').padEnd(6);
        const matchStr = `${t.matched}/${t.eligibleConnectedCount}`.padStart(5);
        const denomStatus = t.isDenominatorExact ? 'OK' : 'FAIL';
        const manStatus = t.isManifestHealthy ? 'OK' : 'FAIL';
        console.log(`Trial: ${t.trial.padEnd(18)} | Exp: ${exp} | Matched: ${matchStr} | Denom: ${denomStatus} | Manifest: ${manStatus} | WinViol: ${t.windowViolations} | 1-to-1 Conf: ${t.oneToOneConflicts} | RouteMismatch: ${t.routeVerification?.routeMismatches ?? 0} | Gate: ${t.isHealthy ? 'PASS' : 'FAIL'}`);
      });

      const reportPath = path.join(outDir, 'r1-capture-integrity.json');
      fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));

      console.log('----------------------------------------------------------------');
      console.log(`AUDIT RESULT: Total=${report.totalTrials}, Valid=${report.validTrials}, Invalid=${report.invalidTrials}, TotalEligible=${report.totalEligible}`);
      console.log(`ALL GATES PASSED: ${report.allGatesPassed ? 'YES (100% AUDIT PASS)' : 'NO (GATE FAILED)'}`);
      console.log(`Integrity Report written to: ${reportPath}`);
      console.log('================================================================\n');

      if (!report.allGatesPassed) {
        process.exit(1);
      }
    })
    .catch(err => {
      console.error('[FATAL AUDIT ERROR]', err);
      process.exit(1);
    });
}
