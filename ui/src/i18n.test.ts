import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { localeTag, translate } from './i18n.js';

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
});
