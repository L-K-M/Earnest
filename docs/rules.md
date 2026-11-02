# Rules

Rules use a closed JSON tree. Each node has a `type` and only the fields
listed below. IDs identify resources you chose. Keyword terms are the words
you wrote; they match entry titles and bodies. `and` and `or` take at least
two children, and `not` takes one.

| Type | Field | Meaning |
| --- | --- | --- |
| `feed_in` | `ids` | Feed is one of these IDs |
| `folder_in` | `ids` | Feed is in one of these folders |
| `keyword` | `terms` | Title or body contains a term |
| `author` | `value` | Author equals this value |
| `classifier_in` | `terms` | Classifier label is in the closed vocabulary |
| `semantic` | `value` | Reserved semantic query; currently never matches |
| `and`, `or`, `not` | `children` | Combine predicates |

The initial classifier vocabulary contains `press_release`. Agent labels are
a separate namespace and cannot be used as predicates. The classifier and
semantic predicates are reserved for the future pipeline.

For example, a rule for a selected feed and the word funding has this tree:

```json
{"type":"and","children":[{"type":"feed_in","ids":[42]},{"type":"keyword","terms":["funding"]}]}
```

| Action | Permitted predicates |
| --- | --- |
| Add a native label | Any valid predicate |
| Set shelf to later or done | Any valid predicate, with every possible match scoped to selected feeds |
| Notify | Any valid predicate, with a daily budget |
| Webhook | Feed and keyword predicates only, including keyword alone |

An AND is feed scoped when at least one child is scoped. Every branch of an
OR must be scoped. Negating a feed does not scope the result. Folder selection
is not a substitute for choosing feeds for a shelf change.

Article authors can influence keywords and model-derived labels. The action
limits keep that influence proportionate: shelf changes are limited to feeds
you selected, notifications have a budget, and webhooks cannot depend on
model-derived results. Notification and webhook destinations are managed
separately. Agents cannot create rules, and agent-written labels never feed
rules.

Rules are managed by humans at `/v1/rules`. A dry run at
`POST /v1/rules:dry-run` returns matching entry summaries from the past seven
days without writing. Enabled rules are queued in the ingest transaction and
run asynchronously on newly ingested entries. Failed jobs retry with backoff;
a repeatedly failing rule is disabled and its reason appears on the rule.
Enabling it clears the reason.

Notifications use the Rules pipeline principal and identify the rule and
matched text in their server-generated provenance. The default daily budget is
three notifications per rule; set `budget` on a notify action to change it.
Each rule and entry pair is deduplicated. Now feeds produce alerts; other feeds
produce digests. Webhook actions queue `entry.matched` with entry and feed IDs.
Label and shelf actions are recorded in activity with the rule name.
