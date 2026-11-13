import {effectiveTaskRoute, findModel, type LLMRoute, type LLMSettingsDraft, type LLMTaskDraft, type ModelDraft} from './model'

const emptyRoute = (): LLMRoute => ({provider: '', model: ''})
const disable = (task: LLMTaskDraft): LLMTaskDraft => ({...task, mode: 'off', ...emptyRoute()})

// Global metadata can serve several destinations. Removing it disables only
// routes that actually resolve to it, preserving provider-specific overrides.
export function withoutModel(draft: LLMSettingsDraft, removed: ModelDraft): LLMSettingsDraft {
  const usesRemoved = (route: LLMRoute) => findModel(draft.models, route.provider, route.model) === removed
  return {
    ...draft,
    models: draft.models.filter(model => model !== removed),
    tasks: draft.tasks.map(task => usesRemoved(effectiveTaskRoute(task, draft.defaults)) ? disable(task) : task),
    defaults: {
      text: usesRemoved(draft.defaults.text) ? emptyRoute() : draft.defaults.text,
      embedding: usesRemoved(draft.defaults.embedding) ? emptyRoute() : draft.defaults.embedding,
    },
  }
}
