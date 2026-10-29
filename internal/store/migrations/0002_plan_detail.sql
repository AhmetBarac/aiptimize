-- Phase 4 stores the explanatory curve and warnings with each plan revision.
ALTER TABLE plans ADD COLUMN curve_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE plans ADD COLUMN warnings_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE plans ADD COLUMN no_plan_reason TEXT NOT NULL DEFAULT '';
