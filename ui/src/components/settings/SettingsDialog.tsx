import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocale } from '../../state/AuditContext';
import {
  applyRuntimeSettings,
  getRuntimeSettings,
  RuntimeSettingsSnapshot,
} from '../../platform/runtimeSettings';
import { IconClose, IconSettings } from '../ui/icons';
import {
  controllerOverrideActive,
  createSettingsDraft,
  isSettingsDirty,
  requestClearSecret,
  resetDraftAfterApply,
  secretOverrideActive,
  secretStatus,
  setReplacementSecret,
  SettingsDraft,
  sourceLabelKey,
  validateControllerUrl,
} from './settingsModel';

type DialogPhase = 'loading' | 'ready' | 'error' | 'applying';

export const SettingsDialog: React.FC<{
  onClose: () => void;
}> = ({ onClose }) => {
  const { t } = useLocale();
  const [snapshot, setSnapshot] = useState<RuntimeSettingsSnapshot | null>(null);
  const [draft, setDraft] = useState<SettingsDraft | null>(null);
  const [phase, setPhase] = useState<DialogPhase>('loading');
  const [notice, setNotice] = useState<'applied' | 'pending' | 'failure' | null>(null);
  const [clearConfirm, setClearConfirm] = useState(false);
  const [discardConfirm, setDiscardConfirm] = useState(false);
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const controllerInputRef = useRef<HTMLInputElement | null>(null);

  const loadSnapshot = useCallback(async () => {
    setPhase('loading');
    try {
      const next = await getRuntimeSettings();
      setSnapshot(next);
      setDraft(createSettingsDraft(next));
      setNotice(null);
      setPhase('ready');
    } catch {
      setPhase('error');
    }
  }, []);

  useEffect(() => {
    void loadSnapshot();
  }, [loadSnapshot]);

  useEffect(() => {
    if (phase !== 'ready' || !snapshot || !draft) return;
    const timer = window.setInterval(async () => {
      try {
        const next = await getRuntimeSettings();
        setSnapshot(next);
        if (!isSettingsDirty(draft, snapshot)) {
          setDraft(createSettingsDraft(next));
        }
      } catch {
        // Keep the last safe snapshot visible while the owner is restarting.
      }
    }, 2000);
    return () => window.clearInterval(timer);
  }, [draft, phase, snapshot]);

  useEffect(() => {
    if (phase !== 'ready') return;
    const frame = window.requestAnimationFrame(() => controllerInputRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [phase]);

  const dirty = !!snapshot && !!draft && isSettingsDirty(draft, snapshot);
  const urlValidation = useMemo(
    () => validateControllerUrl(draft?.controllerUrl ?? ''),
    [draft?.controllerUrl],
  );
  const controllerOverridden = controllerOverrideActive(snapshot?.controllerSource ?? '');
  const secretOverridden = secretOverrideActive(snapshot?.secretSource ?? '');
  const applying = phase === 'applying';

  const requestClose = () => {
    if (dirty) {
      setDiscardConfirm(true);
      return;
    }
    onClose();
  };

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        requestClose();
        return;
      }
      if (event.key !== 'Tab' || !dialogRef.current) return;
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(
        'button:not(:disabled), input:not(:disabled), [href], [tabindex]:not([tabindex="-1"])',
      ));
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  });

  const updateDraft = (update: (current: SettingsDraft) => SettingsDraft) => {
    setDraft((current) => (current ? update(current) : current));
    setNotice(null);
  };

  const apply = async () => {
    if (!draft || !snapshot || !dirty || !urlValidation.valid || applying) return;
    setPhase('applying');
    setNotice(null);
    try {
      const outcome = await applyRuntimeSettings({
        controllerUrl: draft.controllerUrl,
        autostartEnabled: draft.autostartEnabled,
        secretAction: draft.secretAction,
        secret: draft.secretAction === 'replace' ? draft.replacementSecret : '',
      });
      setSnapshot(outcome.settings);
      setDraft(resetDraftAfterApply(outcome));
      setNotice(outcome.activation === 'saved-pending-restart' ? 'pending' : 'applied');
      setClearConfirm(false);
      setPhase('ready');
    } catch {
      setNotice('failure');
      setPhase('ready');
    }
  };

  const statusText = (value: boolean) => value ? t('settings.running') : t('settings.notRunning');
  const ownerText = snapshot?.ownerMode === 'installed-task'
    ? t('settings.ownerInstalled')
    : snapshot?.ownerMode === 'direct-supervisor'
      ? t('settings.ownerDirect')
      : t('settings.ownerUnavailable');
  const loginText = snapshot?.taskRegistered && snapshot.taskEnabled ? t('settings.on') : t('settings.off');
  const secretState = snapshot ? secretStatus(snapshot) : 'not-stored';

  return (
    <div
      className="pl-dialog-backdrop"
      onClick={(event) => {
        if (event.target === event.currentTarget && !dirty) onClose();
      }}
      role="presentation"
    >
      <div
        ref={dialogRef}
        className="pl-dialog pl-settings-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="pl-settings-title"
      >
        <div className="pl-dialog__header">
          <div>
            <h2 id="pl-settings-title" className="pl-dialog__title">{t('settings.title')}</h2>
            <div className="pl-settings-dialog__sub">{t('settings.subtitle')}</div>
          </div>
          <button
            className="pl-icon-btn"
            onClick={requestClose}
            aria-label={t('settings.close')}
            title={t('settings.close')}
          >
            <IconClose />
          </button>
        </div>

        {phase === 'loading' && <div className="pl-dialog__body"><div className="pl-settings-status" role="status">{t('settings.loading')}</div></div>}
        {phase === 'error' && (
          <div className="pl-dialog__body">
            <div className="pl-settings-status pl-settings-status--error" role="alert">{t('settings.statusUnavailable')}</div>
            <div className="pl-settings-actions">
              <button className="pl-btn pl-btn--secondary" onClick={() => void loadSnapshot()}>{t('settings.retry')}</button>
            </div>
          </div>
        )}
        {phase !== 'loading' && phase !== 'error' && snapshot && draft && (
          <>
            <div className="pl-dialog__body">
              <section className="pl-settings-section">
                <div className="pl-dialog__section-title">{t('settings.controller')}</div>
                <div className="pl-settings-field">
                  <label htmlFor="pl-settings-controller-url">{t('settings.controllerUrl')}</label>
                  <input
                    ref={controllerInputRef}
                    id="pl-settings-controller-url"
                    className="pl-input pl-input--mono"
                    value={draft.controllerUrl}
                    disabled={controllerOverridden || applying}
                    aria-invalid={!urlValidation.valid}
                    onChange={(event) => updateDraft((current) => ({ ...current, controllerUrl: event.target.value }))}
                  />
                  {!urlValidation.valid && <div className="pl-settings-field__error" role="alert">{t('settings.invalidControllerUrl')}</div>}
                  <div className="pl-settings-field__helper">{t('settings.controllerUrlHelper')}</div>
                  <div className="pl-settings-source">
                    <span>{t('settings.effectiveSource')}</span>
                    <span className="pl-mono">{t(sourceLabelKey(snapshot.controllerSource))}</span>
                  </div>
                  <div className="pl-settings-field__helper pl-mono">{snapshot.effectiveControllerUrl}</div>
                  {controllerOverridden && <div className="pl-settings-field__notice">{t('settings.controllerOverrideHelper')}</div>}
                </div>
              </section>

              <section className="pl-settings-section">
                <div className="pl-dialog__section-title">{t('settings.secret')}</div>
                <div className="pl-settings-source">
                  <span>{t('settings.secretStatus')}</span>
                  <span>{secretState === 'environment' ? t('settings.environmentOverride') : secretState === 'stored' ? t('settings.stored') : t('settings.notStored')}</span>
                </div>
                <div className="pl-settings-source">
                  <span>{t('settings.effectiveSource')}</span>
                  <span className="pl-mono">{t(sourceLabelKey(snapshot.secretSource))}</span>
                </div>
                <label className="pl-settings-field" htmlFor="pl-settings-secret">
                  <span>{t('settings.replaceSecret')}</span>
                  <input
                    id="pl-settings-secret"
                    className="pl-input"
                    type="password"
                    autoComplete="new-password"
                    spellCheck={false}
                    value={draft.replacementSecret}
                    disabled={secretOverridden || applying}
                    placeholder={t('settings.secretPlaceholder')}
                    onChange={(event) => updateDraft((current) => setReplacementSecret(current, event.target.value))}
                  />
                  <span className="pl-settings-field__helper">{secretOverridden ? t('settings.secretOverrideHelper') : t('settings.secretHelper')}</span>
                </label>
                <div className="pl-settings-secret-actions">
                  {!clearConfirm ? (
                    <button
                      type="button"
                      className="pl-btn pl-btn--quiet pl-btn--compact"
                      disabled={secretOverridden || applying}
                      onClick={() => setClearConfirm(true)}
                    >
                      {t('settings.removeSecret')}
                    </button>
                  ) : (
                    <div className="pl-settings-confirm" role="alertdialog" aria-label={t('settings.removeSecretPrompt')}>
                      <span>{t('settings.removeSecretPrompt')}</span>
                      <button type="button" className="pl-btn pl-btn--danger pl-btn--compact" onClick={() => { updateDraft(requestClearSecret); setClearConfirm(false); }}>{t('settings.confirmRemove')}</button>
                      <button type="button" className="pl-btn pl-btn--quiet pl-btn--compact" onClick={() => setClearConfirm(false)}>{t('settings.cancel')}</button>
                    </div>
                  )}
                </div>
              </section>

              <section className="pl-settings-section">
                <div className="pl-dialog__section-title">{t('settings.background')}</div>
                <label className="pl-settings-toggle">
                  <input
                    type="checkbox"
                    checked={draft.autostartEnabled}
                    disabled={!snapshot.installedLayout || applying}
                    onChange={(event) => updateDraft((current) => ({ ...current, autostartEnabled: event.target.checked }))}
                  />
                  <span>{t('settings.autostart')}</span>
                </label>
                <div className="pl-settings-field__helper">{snapshot.installedLayout ? t('settings.autostartHelper') : t('settings.autostartUnavailable')}</div>
                <div className="pl-settings-facts">
                  <div className="pl-settings-fact"><span>{t('settings.owner')}</span><strong>{ownerText}</strong></div>
                  <div className="pl-settings-fact"><span>{t('settings.loginStart')}</span><strong>{loginText}</strong></div>
                  <div className="pl-settings-fact"><span>{t('settings.supervisor')}</span><strong>{statusText(snapshot.supervisorRunning)}</strong></div>
                  <div className="pl-settings-fact"><span>{t('settings.runtime')}</span><strong>{statusText(snapshot.runtimeRunning)}</strong></div>
                </div>
              </section>
            </div>

            <div className="pl-settings-footer">
              <div className="pl-settings-live" aria-live="polite">
                {notice === 'applied' && t('settings.applied')}
                {notice === 'pending' && t('settings.savedPendingRestart')}
                {notice === 'failure' && t('settings.persistenceFailure')}
                {!notice && dirty && <span>{t('settings.unsaved')}</span>}
                {applying && <span>{t('settings.applying')}</span>}
              </div>
              <div className="pl-settings-actions">
                <button type="button" className="pl-btn pl-btn--quiet" onClick={requestClose}>{t('settings.cancel')}</button>
                <button
                  type="button"
                  className="pl-btn pl-btn--primary"
                  disabled={!dirty || !urlValidation.valid || applying || clearConfirm}
                  onClick={() => void apply()}
                >
                  {t('settings.apply')}
                </button>
              </div>
            </div>
          </>
        )}

        {discardConfirm && (
          <div className="pl-settings-discard" role="alertdialog" aria-label={t('settings.discardPrompt')}>
            <span>{t('settings.discardPrompt')}</span>
            <div className="pl-settings-actions">
              <button type="button" className="pl-btn pl-btn--quiet pl-btn--compact" onClick={() => setDiscardConfirm(false)}>{t('settings.keepEditing')}</button>
              <button type="button" className="pl-btn pl-btn--danger pl-btn--compact" onClick={onClose}>{t('settings.discard')}</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export const SettingsTriggerIcon: React.FC = () => <IconSettings />;
