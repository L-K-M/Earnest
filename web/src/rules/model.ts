import type {components} from '../api.generated'

export type Predicate = components['schemas']['RulePredicate']
export type Action = components['schemas']['RuleAction']
export const classifierVocabulary = ['press_release', 'news', 'opinion', 'tutorial', 'research', 'review', 'event'] as const

export function feedScoped(predicate: Predicate): boolean {
  if (predicate.type === 'feed_in') return Boolean(predicate.ids?.length)
  if (predicate.type === 'and') return Boolean(predicate.children?.some(feedScoped))
  if (predicate.type === 'or') return Boolean(predicate.children?.length && predicate.children.every(feedScoped))
  return false
}

function webhookSafe(predicate: Predicate): boolean {
  if (predicate.type === 'feed_in' || predicate.type === 'keyword') return true
  if (['and', 'or', 'not'].includes(predicate.type)) return Boolean(predicate.children?.length && predicate.children.every(webhookSafe))
  return false
}

export function actionRestriction(predicate: Predicate, action: string): string {
  if (action === 'set_shelf' && !feedScoped(predicate)) return 'Shelf changes need feeds you chose in every possible match. Folders and negated feeds do not scope a rule.'
  if (action === 'webhook' && !webhookSafe(predicate)) return 'Webhooks allow only feeds you chose and keywords you wrote. Remove folders, authors, classifier labels and semantic matches.'
  return ''
}

export function predicateError(predicate: Predicate, pipeline: {classify: string; semantic: string}): string {
  let nodes = 0
  function visit(p: Predicate): string {
    if (++nodes > 256) return 'A rule can contain at most 256 predicates.'
    const fields: Record<string, string> = {feed_in:'ids', folder_in:'ids', keyword:'terms', classifier_in:'terms', author:'value', semantic:'value', and:'children', or:'children', not:'children'}
    const field = Object.hasOwn(fields,p.type) ? fields[p.type] : undefined
    if (!field) return 'Unknown predicate. Choose a supported condition.'
    if (Object.keys(p).some(key => key !== 'type' && key !== field && p[key as keyof Predicate] != null)) return 'The predicate contains fields that do not belong to its type.'
    if (field === 'ids' && (!p.ids?.length || p.ids.some(id => !Number.isSafeInteger(id) || id <= 0))) return 'Choose at least one feed or folder.'
    if (field === 'terms' && (!p.terms?.length || p.terms.some(term => !term.trim()))) return 'Enter at least one term.'
    if (field === 'value' && !p.value?.trim()) return 'Enter an author or semantic description.'
    if (p.type === 'classifier_in') {
      if (pipeline.classify !== 'enabled') return 'Classification is disabled. Remove this condition to run the rule.'
      if (p.terms?.some(term => !(classifierVocabulary as readonly string[]).includes(term))) return 'Choose a classifier label from the vocabulary.'
    }
    if (p.type === 'semantic' && pipeline.semantic !== 'enabled') return 'Semantic matching is disabled. Remove this condition to run the rule.'
    if (field !== 'children') return ''
    if (p.type === 'not' ? p.children?.length !== 1 : (p.children?.length ?? 0) < 2) return p.type === 'not' ? 'Not needs one condition.' : 'All and Any need at least two conditions.'
    for (const child of p.children ?? []) {
      const error = visit(child)
      if (error) return error
    }
    return ''
  }
  return visit(predicate)
}

export function newPredicate(type: string): Predicate {
  if (type === 'feed_in' || type === 'folder_in') return {type, ids:[]}
  if (type === 'keyword' || type === 'classifier_in') return {type, terms:[]}
  if (type === 'author' || type === 'semantic') return {type, value:''}
  return {type, children:Array.from({length:type === 'not' ? 1 : 2}, () => ({type:'keyword', terms:[]}))}
}
