import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { localeMessageKeys, localeTag, translate } from './i18n.js';

const sourceRoot = dirname(fileURLToPath(import.meta.url));

function collectSourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return collectSourceFiles(path);
    return /\.(?:ts|tsx)$/.test(entry.name) && !/\.test\.(?:ts|tsx)$/.test(entry.name) ? [path] : [];
  });
}

function collectStaticTranslationKeys(): Set<string> {
  const keys = new Set<string>();
  for (const path of collectSourceFiles(sourceRoot)) {
    const source = readFileSync(path, 'utf8');
    for (const match of source.matchAll(/\bt\(\s*['"]([^'"]+)['"]/g)) keys.add(match[1]);
    for (const match of source.matchAll(/\btranslate\(\s*['"][^'"]+['"]\s*,\s*['"]([^'"]+)['"]/g)) keys.add(match[1]);
    for (const match of source.matchAll(/\b(?:titleKey|labelKey)\s*(?:=|:)\s*([^;\n]+)/g)) {
      for (const key of match[1].matchAll(/['"]([A-Za-z][\w-]*(?:\.[A-Za-z0-9_-]+)+)['"]/g)) keys.add(key[1]);
    }
  }
  return keys;
}

describe('UI locale', () => {
  it('translates interface copy without changing canonical values', () => {
    assert.equal(translate('en', 'common.apply'), 'Apply');
    assert.equal(translate('zh-CN', 'common.apply'), '应用');
    assert.equal(translate('en', 'history.pageSize', { size: 50 }), '50 / page');
    assert.equal(translate('zh-CN', 'history.pageSize', { size: 50 }), '50 / 页');
    assert.equal(translate('en', 'route.proxy'), 'Proxy');
    assert.equal(translate('zh-CN', 'route.proxy'), '代理');
  });

  it('falls back safely to English for an incomplete future dictionary entry', () => {
    assert.equal(translate('zh-CN', 'common.refresh'), '刷新');
    assert.equal(translate('en', 'missing.future.key'), 'missing.future.key');
  });

  it('uses explicit locale tags for UI formatting', () => {
    assert.equal(localeTag('en'), 'en-GB');
    assert.equal(localeTag('zh-CN'), 'zh-CN');
  });

  it('keeps English and Chinese dictionaries in parity', () => {
    assert.deepEqual(localeMessageKeys('zh-CN').sort(), localeMessageKeys('en').sort());
  });

  it('distinguishes accounting publication incompleteness from monitoring gaps', () => {
    assert.match(translate('en', 'review.comparisonStatus.recent_accounting_incomplete'), /accounting has not yet published all evidence/);
    assert.match(translate('zh-CN', 'review.comparisonStatus.recent_accounting_incomplete'), /证据尚未完成核算发布/);
    assert.match(translate('en', 'review.comparisonStatus.recent_has_monitoring_gaps'), /monitoring gap/);
    assert.match(translate('zh-CN', 'review.comparisonStatus.recent_has_monitoring_gaps'), /监控缺口/);
  });

  it('keeps static UI translation keys backed by the English dictionary', () => {
    const knownKeys = new Set(localeMessageKeys('en'));
    const missingKeys = [...collectStaticTranslationKeys()].filter((key) => !knownKeys.has(key)).sort();
    assert.deepEqual(missingKeys, []);
  });
});
