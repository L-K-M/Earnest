import type {Command} from './palette/Palette'
import {viewScope, type RangeScope, type Subscription, type View} from './reader'

// The bulk mark-read action is a palette command, not a toolbar button
// (docs/research/decisions.md). It marks a view read through the newest entry
// loaded in it, so articles that arrive afterwards stay unread.
export function markRestCommand(
  view: View,
  subscriptions: Subscription[],
  newestLoaded: number,
  markRange: (scope: RangeScope, throughSeq: number) => Promise<void>,
  confirm: (message: string) => boolean = message => window.confirm(message),
): Command {
  const scope = newestLoaded > 0 ? viewScope(view, subscriptions) : null
  return {
    id: 'mark-rest-read',
    title: `Mark the rest of ${view.title} read`,
    key: 'Ctrl+K, Enter',
    disabled: scope === null,
    run: async () => {
      if (!scope) return
      if (!confirm(`Mark everything in ${view.title} read, up to the newest article loaded here? Articles that arrive later stay unread.`)) return
      await markRange(scope, newestLoaded)
    },
  }
}
