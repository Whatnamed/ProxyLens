import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  controllerOverrideActive,
  createSettingsDraft,
  isSettingsDirty,
  requestClearSecret,
  secretOverrideActive,
  secretStatus,
  setReplacementSecret,
  validateControllerUrl,
} from './settingsModel.js';
import { RuntimeSettingsSnapshot } from '../../platform/runtimeSettings.js';

function snapshot(overrides: Partial<RuntimeSettingsSnapshot> = {}): RuntimeSettingsSnapshot {
  return {
    schemaVersion: 2,
    persistedControllerUrl: 'http://persisted.example.test',
    effectiveControllerUrl: 'http://persisted.example.test',
    controllerSource: 'PERSISTED',
    autostartEnabled: true,
    installedLayout: true,
    credentialStored: true,
    effectiveSecretPresent: true,
    secretSource: 'CREDENTIAL_MANAGER',
    ownerMode: 'installed-task',
    taskRegistered: true,
    taskEnabled: true,
    supervisorRunning: true,
    runtimeRunning: true,
    bootstrapState: 'installed',
    ...overrides,
  };
}

describe('Settings model', () => {
  it('starts with keep and never pre-fills a Secret', () => {
    const draft = createSettingsDraft(snapshot());
    assert.equal(draft.secretAction, 'keep');
    assert.equal(draft.replacementSecret, '');
  });

  it('tracks URL, autostart, and explicit Secret actions as dirty state', () => {
    const current = snapshot();
    const draft = createSettingsDraft(current);
    assert.equal(isSettingsDirty(draft, current), false);
    assert.equal(isSettingsDirty({ ...draft, autostartEnabled: false }, current), true);
    assert.equal(isSettingsDirty({ ...draft, controllerUrl: 'http://other.example.test' }, current), true);
    assert.equal(isSettingsDirty(requestClearSecret(draft), current), true);
  });

  it('validates an empty or base Controller URL without connection testing', () => {
    assert.equal(validateControllerUrl('').valid, true);
    assert.equal(validateControllerUrl('http://127.0.0.1:43127').valid, true);
    assert.equal(validateControllerUrl('https://controller.example.test/').valid, true);
    assert.equal(validateControllerUrl('http://user:pass@example.test').valid, false);
    assert.equal(validateControllerUrl('http://example.test/path').valid, false);
    assert.equal(validateControllerUrl('http://example.test?token=no').valid, false);
  });

  it('uses replace only while a replacement value is present', () => {
    const draft = createSettingsDraft(snapshot());
    const replacing = setReplacementSecret(draft, 'synthetic-value');
    assert.equal(replacing.secretAction, 'replace');
    assert.equal(setReplacementSecret(replacing, '').secretAction, 'keep');
  });

  it('disables conflicting controls for effective overrides', () => {
    assert.equal(controllerOverrideActive('PROXYLENS_CONTROLLER_URL'), true);
    assert.equal(controllerOverrideActive('PERSISTED'), false);
    assert.equal(secretOverrideActive('MIHOMO_SECRET'), true);
    assert.equal(secretOverrideActive('CREDENTIAL_MANAGER'), false);
    assert.equal(secretStatus(snapshot({ secretSource: 'MIHOMO_SECRET' })), 'environment');
    assert.equal(secretStatus(snapshot({ credentialStored: false, effectiveSecretPresent: false, secretSource: 'NONE' })), 'not-stored');
  });
});
