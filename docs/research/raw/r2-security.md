# Adversarial review — security red team

Scope: `tech-stack.md` Security + Reachability, `llm.md` Safety, `api.md` notification + MCP design, plus dependencies. Threat model: hostile feed author, compromised blog, malicious redirect, prompt-injected agent, LAN/tailnet neighbours, push providers. All seven files read. ✔ = verified against primary sources 2026-11-01; ~ = judgement.

## Blocking

**B1. The image proxy is the exact SSRF that shipped in the prior art ✔**
*tech-stack.md:78 — "Image proxy as an open proxy | Signed URLs only: `/img/{hmac}/{url}` … Never SVG."*

A signature proves the server issued the URL, not where it then connects; a feed author controls the URL, so the server signs and fetches whatever they choose. Miniflux had this exact bug: **GHSA-xwh2-742g-w3wp** (fixed 2.2.16) — "the proxy validates the URL scheme and HMAC signature, but does not restrict target hosts/IPs", so "requests to loopback/private/link-local addresses are allowed and fetched by the server." <https://github.com/miniflux/v2/security/advisories/GHSA-xwh2-742g-w3wp>. Miniflux 2.3.4 (2026-10-20), which the draft borrows, now blocks reserved ranges: the stated control is the *unfixed* design.

*Fix:* drop "signed URLs" as a control. Use the fetcher's outbound client with the same resolved-IP check per connection and redirect; stream caps via `io.LimitReader`; no data: URIs in the policy; sniff `Content-Type` with `nosniff`.

**B2. Agent tokens may hold `webhooks:write` — a complete exfiltration chain ✔**
*api.md:109 — scopes include `webhooks:write`; "Agent tokens cannot hold `admin` or manage tokens." llm.md:115 — tier 3: webhooks leave the system and are "**Never an LLM alone**."*

Direct contradiction, and the tier table is prose, not the authorisation layer. A token with `feeds:write` + `webhooks:write` + `notify:send` gives an injected agent: subscribe to an attacker page (or `http://192.168.1.1/`, B3); the fetcher stores it as an entry; register a webhook at an attacker host; wait for `entry.matched`; the NAS POSTs the entry body off-box.

*Fix:* remove `webhooks:write` from every agent-token scope set; treat webhook URLs as an SSRF sink (same IP check, no redirects, rate limit); enforce tiers in the service layer.

**B3. The SSRF allowlist is incomplete, hand-rolled and rebindable ✔**
*tech-stack.md:77 — "reject private, loopback, link-local and `100.64.0.0/10`, connect to the pinned IP, re-check every redirect."*

Missing: all of IPv6 (`::1`, `fc00::/7`, `fe80::/10`, IPv4-mapped `::ffff:0:0/96`, NAT64 `64:ff9b::/96`), plus `0.0.0.0/8`, `198.18.0.0/15`, `240.0.0.0/4`; scheme and port allowlists; and sinks: `/v1/discover`, `/v1/opml:import` (one request → hundreds of fetches), full-text extraction, favicons, push and webhook endpoints. MCP's guidance: "**Avoid implementing IP validation manually**." <https://modelcontextprotocol.io/specification/2026-07-28/basic/security_best_practices>. "Connect to the pinned IP" is not implementable with Go's default transport, which re-resolves per connection and pools, so as written it is TOCTOU.

*Fix:* one `*http.Client` with a custom `DialContext` that resolves, checks *every* returned address, and dials the checked IP with SNI/Host set to the hostname — that dial-time check is also the rebinding control, so add a fetcher conformance case (tech-stack.md:146) whose resolver returns a public IP first and `127.0.0.1` second. No cross-host reuse; schemes `http,https`, ports 80/443 only; refuse IPv6 literals; deny-list as a table with a test per range.

## Major

**M1. Feed content does control rule actions ✔**
*llm.md:49 — "feed content cannot rewrite it."*

True of the rule, false of the match. Rules compile to `label = press_release AND topic ~ "funding"` (llm.md:42), and the label is model-computed from the article (llm.md:51,56). Tier 1 "Hold, tag, move to Done" is triggered by "Your compiled rules" (llm.md:113), so a hostile article chooses whether the rule fires; and `~` is undefined — an embedding match would make the whole predicate model-derived from attacker text.

*Fix:* define `~` as lexical or drop it; every tier ≥1 action must carry a deterministic term (keyword, feed or source); validate classifier output against a closed label vocabulary, dropping out-of-vocabulary labels.

**M2. Annotations are the second-order injection channel ✔**
*llm.md:62-64 — one annotation store shared by the internal pipeline and external agents. api.md:89 — `annotations:write` lets any agent write `kind` = summary, label, score, note.*

Citations are checked mechanically, but only for the internal summariser (llm.md:105); an agent can write `kind=summary` with fabricated ¶2/¶4 citations and ux.md:135 renders it identically. Nothing marks annotations untrusted, so agent B trusts agent A's output.

*Fix:* restrict `summary` and `label` kinds to the internal principal, or apply the citation check to all writers; carry `principal`, `model` and `derived_from_entry_ids`; render agent annotations with provenance; label them untrusted in tool results.

**M3. MCP handles: cursors and undo handles are the new session ✔**
*api.md:147-149,171 — "Cross-call state goes in explicit handles passed as arguments, which suits cursors."*

The 2026-07-28 revision added "State handle hijacking": servers "**MUST NOT** treat possession of a state handle as authentication" and "**SHOULD** bind handles server-side to the authenticated user". <https://modelcontextprotocol.io/specification/2026-07-28/basic/security_best_practices>. `c_1045` is sequential.

*Fix:* 128-bit random handles bound to the issuing principal; reject a handle from any other principal; expiry on undo handles.

**M4. One sanitiser, three parsers ✔**
*tech-stack.md:79,147,157 — sanitise once on the server, render in WKWebView, Android WebView and a sandboxed iframe.*

bluemonday's Limitations call it "a fast **non-validating**, forward only, token-based parser", and warn that its unawareness of valid structure "can allow these elements to bypass some of the safety mechanisms built into the WhatWG HTML parser standard". <https://github.com/microcosm-cc/bluemonday> — mutation XSS by another name; the three renderers use three parsers. "Sanitiser tests from known XSS payload lists" (tech-stack.md:147) tests neither the renderers nor mXSS.

*Fix:* forbid `<template>`, `<noscript>`, `<svg>`, `<math>`, data: URIs and the `style` attribute; store the serialised-after-reparse form and re-sanitise per renderer; golden-corpus each renderer. Iframe: `sandbox=""` (no `allow-scripts`, `allow-same-origin`, `allow-top-navigation`) on a separate origin, plus a CSP on the SPA. Android: `javaScriptEnabled=false`, `allowFileAccess=false`, **`allowContentAccess=false`** (defaults to true), `WebViewAssetLoader` not a custom scheme, and scheme+host allowlisting in `shouldOverrideUrlLoading` and on any exported deep-link activity (Android warns any app can send a WebView activity an arbitrary URI). <https://developer.android.com/privacy-and-security/risks/insecure-webview-native-bridges>, <https://mas.owasp.org/MASTG/knowledge/android/MASVS-PLATFORM/MASTG-KNOW-0018/>.

**M5. Notifications: provenance-shaped attacker text, and bodies leave the tailnet ✔**
*api.md:138-143 — "`reason` and a known sender are required. The UI always shows them."*

`reason` is caller-supplied free text rendered inside a "WHY YOU GOT THIS / Rule: … Matched: …" block (ux.md:118-129) that reads as server-generated provenance; an injected agent, or a hostile article via a compiled rule, controls it. Push bodies are stored by third parties (APNs, FCM, autopush, ntfy), and the draft never says whether ntfy is self-hosted: ntfy's docs state "the topic is essentially a password", and its public server is "open for everyone … to read and write to any topic" (<https://docs.ntfy.sh/publish/>). **UnifiedPush requires RFC 8291 end-to-end encryption**, and "some applications fake it at this moment" (<https://unifiedpush.org/users/faq/>) — "share one encoder" (tech-stack.md:125) is safe only if it really does aes128gcm. UnifiedPush's guidance to application servers — "rejects push endpoints pointing to private IPs … doesn't follow redirections for push requests" (<https://unifiedpush.org/news/20250513_push_security_privacy/>) — is an omitted SSRF control.

*Fix:* server-generate `reason` from the rule/match record; bodies carry the entry title only, with content fetched on tap; self-hosted ntfy with `auth-default-access: deny-all`, or a ≥128-bit random topic as a credential; implement the UnifiedPush endpoint checks.

**M6. Funnel can publish the whole API by port collision ✔**
*tech-stack.md:96-98 — a second listener exposing only `/mcp`, `/.well-known/*`, `/oauth/*`, `/websub/*` behind Funnel.*

Verified: "**The same port number cannot be used for Serve … and Funnel … at the same time; if the last command was `funnel`, the port will be completely public.**" And "only `http://127.0.0.1` is currently supported for proxies." <https://tailscale.com/docs/features/tailscale-funnel>. One stray `tailscale funnel` on the main port publishes the whole native API, `/v1/tokens` included.

*Fix:* bind the public listener to a distinct loopback-only port; document that Funnel must never target the main listener; assert it at startup.

**M7. MCP spec: two omissions and one stale hedge ✔**
*api.md:147-154.* Claims check out against <https://modelcontextprotocol.io/specification/2026-07-28/changelog>; omissions:
- "Confirm exactly which revision it serves" is resolvable: go-sdk **v1.7.0** "brings full support for protocol version `2026-07-28`"; **v1.8.0** exists (2026-09-04). It serves `2026-07-28` over HTTP "only when `StreamableHTTPOptions.Stateless = true`"; otherwise clients silently negotiate down to 2025-11-25. <https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0>
- Missing: `Mcp-Method`/`Mcp-Name` are now required headers; `CacheableResult` now **requires** `ttlMs` and `cacheScope`; `cacheScope: "public"` on user data behind a shared intermediary is a caching bug.
- Missing: "MCP servers **MUST NOT** accept any tokens that were not explicitly issued for the MCP server" — public-listener tokens must not work on the tailnet listener, or vice versa.

## Minor

- **tech-stack.md:36** — bluemonday's "last release 2023" is wrong: v1.0.27, 2024-07-04 (<https://proxy.golang.org/github.com/microcosm-cc/bluemonday/@v/list>). It is BSD-3-Clause, so README.md:70's licence note is incomplete.
- **tech-stack.md:143-150** — tests but no supply-chain controls, though the prior art has them: Miniflux 2.3.0 "Pin third-party GitHub Actions to immutable commit SHAs to reduce supply-chain risks", and 2.2.19 adds HMAC-SHA256 auth and constant-time token comparison. Minimum here: `govulncheck` in CI, pinned action SHAs and image digests, Renovate, ≥128-bit token entropy, SHA-256 at rest, constant-time comparison, a log-scrubbing test.
