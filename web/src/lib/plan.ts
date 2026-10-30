import type { PlanView } from '../api/types'

/** The plan the editor loaded: draft first, then the applied plan, then the
 * automatic suggestion. Mirrors what the backend considers editable for a day. */
export function basePlanID(view: PlanView | null | undefined): number {
  if (!view) return 0
  return view.draft?.id ?? view.applied?.id ?? view.auto?.id ?? 0
}

/** Revision of the plan the editor is based on; used as the optimistic lock
 * when saving a draft. Must point at the edited plan, not at the automatic
 * suggestion, or an applied manual plan can never be edited again. */
export function baseRevision(view: PlanView | null | undefined): number {
  if (!view) return 0
  return view.draft?.revision ?? view.applied?.revision ?? view.auto?.revision ?? 0
}
