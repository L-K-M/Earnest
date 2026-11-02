# Adversarial review, round 3 — security angle

Scope: the "no exit" claims (llm.md:110, api.md:241) and tech-stack.md's Security section.
Findings fixed per validation.md are not repeated. "Verified" = checked at the source;
"judgement" = my reasoning.

## Findings

**1. Major — "no exit" / "leak anything" is false: `notify` is an agent-controlled egress channel.**
llm.md:110 ("a dedicated agent with Earnest as its only tool then has no exit"), api.md:241,
README.md:21-23 ("leak anything"). `notify` (api.md:235) sends agent-authored `title` and
`note` (api.md:179-191) to APNs/FCM/ntfy; api.md:205 admits "Bodies leave the tailnet. APNs and FCM store payloads in
transit." The agent chooses what reaches Apple and Google and can encode allowlisted feed
content (private feeds excluded by default only, llm.md:142); api.md:205's mitigation covers
entry content, not the agent-written fields. Under the doc's trifecta framing
(llm.md:103), a 0%-blocked exit.
Change: restate invariant 2 — the only agent-reachable egress is the push payload, which
reaches APNs/FCM/ntfy and must be treated as public. Drop "no exit" and README's "leak
anything", or bound it: caps on `title`/`note`, no entry body interpolated, budget counts
bytes.

**2. Major — annotation rendering is unspecified in all three clients.**
The agent's brief is an annotation (llm.md:21) rendered in all three clients
(ux.md:138-142). The Security section sanitises entry HTML only (tech-stack.md:90-96); nothing binds annotation
`value` (api.md:46, 234) to plain text in any client, or forbids Markdown, HTML, autolinking
or images (llm.md:118 covers the summariser row only). If any client renders agent text
richly, an article-injected agent gets stored XSS in the user's session — a real exit that
invariant 2 says cannot happen.
Change: annotation values are plain text in all three clients; no HTML, Markdown, autolinks or
remote images; links only when the URL is verified in the `derived_from` source entries.

**3. Major — the "deterministic term" fix does not hold; the doc's own example is attacker-firable.**
llm.md:58 requires an acting rule to include "a deterministic term (keyword, feed or source)".
A keyword matches article text, which the attacker writes, and llm.md:53 concedes an article
can choose its own label, so the example at llm.md:44 (`label = press_release AND text has
"funding"`) is firable at will, driving tier-1 shelf changes, tier-2 notifications
(features.md:66) and tier-3 Karakeep/webhook actions (llm.md:133). An agent can also fire a
rule and its webhook by writing a `label` annotation (api.md:234, 175).
Change: only a feed/source match counts as deterministic; content-derived terms (keyword, title,
author) are attacker-controlled; acting rules must be scoped to feeds you chose.

**4. Major — IPv6 zone IDs bypass the deny list (defect introduced by the fix).**
tech-stack.md:82 tests each range with `net/netip`. Verified: `Prefix.Contains` returns false
when the address has an IPv6 zone, "because Prefixes strip zones"
(pkg.go.dev/net/netip#Prefix.Contains, go1.27.3), and `ParseAddr` accepts zoned literals such
as `fe80::1cc0:3e8c:119f:c2e1%ens18`. A URL with host `[fe80::1%eth0]` passes every range in
the table, including `fe80::/10` (only a literal URL or redirect can carry a zone; DNS
cannot), and `net.Dialer.Control` then sees a dialable link-local
address on the container's own link — the hand-rolled IP parsing that bullet warns against.
Change: reject `Zone() != ""` before matching, add `::/96` and `::/128`, test a zoned literal
beside the IPv4-mapped case at tech-stack.md:86, or adopt Smokescreen.

**5. Minor — the iframe CSP claim protects nothing cross-origin.**
tech-stack.md:93: "`iframe sandbox=""` on a separate origin, plus a strict CSP on the app."
Judgement: a cross-origin frame is not governed by the app's CSP, so the article frame needs
its own (iframe `csp` attribute or headers on that origin); and `sandbox=""` without
`allow-popups` makes reader-view links inert unless the parent intercepts clicks or the
sanitiser strips `<a>`. Change: add the frame's CSP and the link decision to tech-stack.md:93.

**6. Minor — the Mac reader view has no navigation policy; Android does.**
tech-stack.md:94 vs :95 ("allowlisted navigation"). With JavaScript off, `meta refresh` and
form submission still navigate and CSP cannot block top-level navigation, so a hostile article
can navigate the reader view off-app.
Change: a `WKNavigationDelegate` policy allowing only the article's custom scheme.

**7. Minor — agent-supplied `category` can mint Android notification channels.**
api.md:189 (`category`, no stated vocabulary) with ux.md:148 ("Channels per user-visible
category. New content is IMPORTANCE_LOW"). Judgement: free-form `category` lets an agent
create arbitrary channels, defeating the importance policy and per-channel mutes;
`notify:urgent` is the only sanctioned escalation (api.md:204). Change: closed, server-defined
category set; agent notifications land in the agent's own channel.

**8. Minor — agent-supplied `dedupe_key` can suppress your notifications.**
api.md:189 (`dedupe_key` caller-supplied), api.md:203 (dedupe window, scope unstated).
Judgement: if the window is global, an agent can guess a rule-generated key and swallow that
notification. Change: namespace dedupe keys per principal and state it.

**9. Minor — `refresh` reachability, and politeness of on-demand extraction.**
api.md:141 lists `POST /v1/subscriptions/{id}/refresh`; the scope table (api.md:171) denies
`feeds:write` but is silent on refresh, which fetches with the feed's stored credentials
(tech-stack.md:73). Per-host concurrency and spacing live in the fetcher (tech-stack.md:68),
not the outbound client (tech-stack.md:79-86), so an agent looping `read_entry` can drive a
burst of extraction requests (tech-stack.md:72) at one host.
Change: refresh requires a human scope; no agent scope may trigger a fetch; on-demand
extraction and image-proxy fetches inherit the fetcher's per-host limiter.

**10. Minor — agent annotations and the internal pipeline are unspecified.**
llm.md:73 marks agent annotations untrusted only "when returned to any other agent".
Judgement: nothing says whether they enter embeddings, clustering, editions or classifier
context at step 9, so an injected annotation could steer editions and search untainted.
Change: exclude them from internal model context, or propagate the taint and flag it in
editions.

## Checked and found sound (verified)

- api.md:210-222 vs the 2026-07-28 changelog
  (modelcontextprotocol.io/specification/2026-07-28/changelog): statelessness,
  `server/discover`, `resultType`, required headers, `ttlMs`/`cacheScope`, deprecated
  Sampling/Roots/Logging, DCR deprecation, `subscriptions/listen`, removed resumability —
  all accurate.
- api.md:222 vs go-sdk v1.7.0 notes: 2026-07-28 served "only when
  `StreamableHTTPOptions.Stateless = true`", else clients negotiate down — accurate.
- tech-stack.md:88 vs GHSA-xwh2-742g-w3wp: Miniflux's proxy "validates the URL scheme and HMAC
  signature, but does not restrict target hosts/IPs" — accurate.
- No egress via `search`, `get_briefing` parameters, `advance_position`, `position` (own-only),
  deep links (internal IDs only), `propose_subscription` (fetches nothing until approved),
  webhook creation (you only), or `triage`/`revert` (own actions).
