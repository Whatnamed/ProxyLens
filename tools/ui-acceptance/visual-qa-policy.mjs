import path from 'node:path';

export const VisualQaGateState = Object.freeze({
  disabled: 'disabled',
  active: 'active',
  refusedIncomplete: 'refused-incomplete',
});

// This is the runner-side safety mirror. The Rust gate remains authoritative
// inside Tauri; keeping the same pure cases here makes harness regressions
// executable without starting a desktop process.
export function classifyVisualQaGate({ e2eMode, visualQaFlag }) {
  if (visualQaFlag === undefined) return VisualQaGateState.disabled;
  if (e2eMode === '1' && visualQaFlag === '1') return VisualQaGateState.active;
  return VisualQaGateState.refusedIncomplete;
}

export function validateVisualQaRunnerInputs({
  e2eMode,
  visualQaFlag,
  explicitDbPath,
  fixtureExists,
  canonicalProductionDbPath,
  controllerUrl,
  mihomoSecret,
}) {
  if (classifyVisualQaGate({ e2eMode, visualQaFlag }) !== VisualQaGateState.active) {
    throw new Error('VISUAL_QA_NOT_SAFE: incomplete visual QA gate');
  }
  if (!explicitDbPath || !path.isAbsolute(explicitDbPath)) {
    throw new Error('VISUAL_QA_NOT_SAFE: fixture DB must be absolute');
  }
  if (!fixtureExists) throw new Error('DB_NOT_READY: fixture DB does not exist');
  if (samePath(explicitDbPath, canonicalProductionDbPath)) {
    throw new Error('VISUAL_QA_NOT_SAFE: canonical production database');
  }
  if (controllerUrl) {
    throw new Error('VISUAL_QA_NOT_SAFE: Controller authority is inherited');
  }
  if (mihomoSecret) {
    throw new Error('VISUAL_QA_NOT_SAFE: Secret authority is inherited');
  }
}

export function settingsCommandAllowedInVisualQa(gateState) {
  return gateState === VisualQaGateState.disabled;
}

function samePath(left, right) {
  if (!right) return false;
  return path.resolve(left).toLowerCase() === path.resolve(right).toLowerCase();
}
