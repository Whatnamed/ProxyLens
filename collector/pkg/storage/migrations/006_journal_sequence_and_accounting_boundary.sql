-- Migration 006: Global Monotonic Journal Sequence & Accounting Boundary

ALTER TABLE event_journal ADD COLUMN journal_sequence INTEGER NULL;

-- 确定性回填历史数据 sequence
UPDATE event_journal
SET journal_sequence = rowid
WHERE journal_sequence IS NULL;

-- 建立唯一索引以保证严格全局单调递增
CREATE UNIQUE INDEX IF NOT EXISTS idx_event_journal_sequence
ON event_journal(journal_sequence);

-- 扩展 accounting_runs 结构以绑定权威不可变源序列边界
ALTER TABLE accounting_runs ADD COLUMN source_journal_sequence_max INTEGER NULL;
ALTER TABLE accounting_runs ADD COLUMN failed_reason TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_accounting_runs_status
ON accounting_runs(status);
