import { invoke } from '@tauri-apps/api/core';

export type SecretAction = 'keep' | 'replace' | 'clear';

export interface RuntimeSettingsSnapshot {
  schemaVersion: number;
  persistedControllerUrl: string;
  effectiveControllerUrl: string;
  controllerSource: string;
  autostartEnabled: boolean;
  installedLayout: boolean;
  credentialStored: boolean;
  effectiveSecretPresent: boolean;
  secretSource: string;
  ownerMode: 'installed-task' | 'direct-supervisor' | 'unavailable' | string;
  taskRegistered: boolean;
  taskEnabled: boolean;
  supervisorRunning: boolean;
  runtimeRunning: boolean;
  bootstrapState: string;
}

export interface RuntimeSettingsApplyRequest {
  controllerUrl: string;
  autostartEnabled: boolean;
  secretAction: SecretAction;
  secret: string;
}

export interface RuntimeSettingsApplyOutcome {
  saved: boolean;
  activation: 'not-required' | 'applied' | 'saved-pending-restart';
  restartRequired: boolean;
  message?: string;
  settings: RuntimeSettingsSnapshot;
}

export function getRuntimeSettings(): Promise<RuntimeSettingsSnapshot> {
  return invoke<RuntimeSettingsSnapshot>('get_runtime_settings');
}

export function applyRuntimeSettings(
  request: RuntimeSettingsApplyRequest,
): Promise<RuntimeSettingsApplyOutcome> {
  return invoke<RuntimeSettingsApplyOutcome>('apply_runtime_settings', { request });
}
