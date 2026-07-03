-- Phase 1.5: AI-verdict columns on the audit log.
--
-- These four columns capture the contextmod AI-escalation decision on the rows
-- written for AI-reviewed messages (LogExternal / EscalateExternal), so the
-- operator can see what the AI classified — including low-confidence,
-- audit-only outcomes that were recorded but never enforced. Fast-path
-- (rule-engine) rows leave all four NULL, which is also how the audit API and
-- the dashboard distinguish an "AI" row from a "Fast path" row (ai_consulted
-- IS NOT NULL vs IS NULL).
--
-- The migration runner re-executes every migration on each boot and tolerates
-- the "duplicate column name" error, so these ADD COLUMN statements are safe to
-- re-run.
ALTER TABLE automod_audit ADD COLUMN ai_category   TEXT;
ALTER TABLE automod_audit ADD COLUMN ai_severity   INTEGER;
ALTER TABLE automod_audit ADD COLUMN ai_confidence REAL;
ALTER TABLE automod_audit ADD COLUMN ai_consulted  INTEGER;
