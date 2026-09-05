import {
  RuntimeSettingsApplyOutcome,
  RuntimeSettingsSnapshot,
  SecretAction,
} from '../../platform/runtimeSettings';

export interface SettingsDraft {
  controllerUrl: string;
  autostartEnabled: boolean;
  secretAction: SecretAction;
  replacementSecret: string;
}

export interface ControllerUrlValidation {
  valid: boolean;
  errorKey?: 'settings.invalidControllerUrl';
}

export function createSettingsDraft(snapshot: RuntimeSettingsSnapshot): SettingsDraft {
  return {
    controllerUrl: snapshot.persistedControllerUrl,
    autostartEnabled: snapshot.autostartEnabled,
    secretAction: 'keep',
    replacementSecret: '',
  };
}

export function resetDraftAfterApply(
  outcome: RuntimeSettingsApplyOutcome,
): SettingsDraft {
  return createSettingsDraft(outcome.settings);
}

export function isSettingsDirty(draft: SettingsDraft, snapshot: RuntimeSettingsSnapshot): boolean {
  return draft.controllerUrl.trim() !== snapshot.persistedControllerUrl.trim()
    || draft.autostartEnabled !== snapshot.autostartEnabled
    || draft.secretAction !== 'keep';
}

export function validateControllerUrl(value: string): ControllerUrlValidation {
  const trimmed = value.trim();
  if (trimmed === '') return { valid: true };
  try {
    const url = new URL(trimmed);
    if (!['http:', 'https:'].includes(url.protocol)
      || !url.hostname
      || url.username
      || url.password
      || url.search
      || url.hash
      || (url.pathname !== '' && url.pathname !== '/')) {
      return { valid: false, errorKey: 'settings.invalidControllerUrl' };
    }
    if (url.port) {
      const port = Number(url.port);
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        return { valid: false, errorKey: 'settings.invalidControllerUrl' };
      }
    }
    return { valid: true };
  } catch {
    return { valid: false, errorKey: 'settings.invalidControllerUrl' };
  }
}

export function controllerOverrideActive(source: string): boolean {
  return source === 'CLI' || source === 'PROXYLENS_CONTROLLER_URL';
}

export function secretOverrideActive(source: string): boolean {
  return source === 'MIHOMO_SECRET';
}

export function secretStatus(snapshot: RuntimeSettingsSnapshot): 'stored' | 'not-stored' | 'environment' {
  if (secretOverrideActive(snapshot.secretSource)) return 'environment';
  return snapshot.credentialStored ? 'stored' : 'not-stored';
}

export function sourceLabelKey(source: string): string {
  switch (source) {
    case 'PERSISTED': return 'settings.sourceSaved';
    case 'PRODUCT_DEFAULT': return 'settings.sourceDefault';
    case 'PROXYLENS_CONTROLLER_URL': return 'settings.sourceEnvironment';
    case 'CLI': return 'settings.sourceProcess';
    case 'MIHOMO_SECRET': return 'settings.sourceEnvironment';
    case 'CREDENTIAL_MANAGER': return 'settings.sourceCredential';
    case 'NONE': return 'settings.sourceNone';
    default: return 'settings.sourceUnavailable';
  }
}

export function setReplacementSecret(draft: SettingsDraft, value: string): SettingsDraft {
  return {
    ...draft,
    replacementSecret: value,
    secretAction: value.length > 0 ? 'replace' : 'keep',
  };
}

export function requestClearSecret(draft: SettingsDraft): SettingsDraft {
  return { ...draft, replacementSecret: '', secretAction: 'clear' };
}
