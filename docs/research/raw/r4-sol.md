**1. Blocking defects — judgement**

None that warrants delaying steps 0 and 1. The remaining protocol problems can be investigated in the explicitly planned executable model. They would block treating this as an implementation specification.

**Verified:** NewsBlur documents [self-hosted MCP](https://github.com/samuelclay/NewsBlur#self-hosted-mcp-server), [native apps connecting to self-hosted instances](https://www.newsblur.com/alternative/self-hosted-rss-reader), and a [Mac app](https://blog.newsblur.com/2026/03/05/newsblur-v14-for-ios-and-mac/). Current documents [counts-free reading, Waterline and Ambient Closure](https://www.currentreader.app/docs/faq). Those support the proposed experiments; they do not establish that either product works acceptably on your NAS.

**2. Earlier blocking findings**

Using the findings recorded in `validation.md`. “Resolved” below means corrected in the research, not demonstrated in running software.

| Earlier finding | Status now — judgement unless marked verified |
|---|---|
| Claimed market gap overlooked NewsBlur; calm-reading differentiation overstated; counts rejected | **Resolved.** Competitor premises verified above; counts are offered and demand remains qualified. |
| Publisher-time ordering and watermark failure modes | **Resolved as a proposed direction.** Server ingest ordering and per-entry state replace them; implementation remains unproved. |
| Missing bootstrap, tombstones, cursor expiry and outbox recovery | **Properly open for step 1.** Relevant scenarios are listed. |
| Range replay could not represent skipped entries | **Properly open.** Exceptions address the original omission; replay and revision semantics still need modelling. |
| Old timestamps promoted stale writes | **Resolved in the proposal.** Old values remain old; expiry and slow-clock behaviour have vectors. |
| Offline Google Reader writes defeated native decisions | **Partly addressed, properly within step 1.** Passive `read` tackles one case; see remaining cases below. |
| Starring displaced Later; marking unread left Done effective | **Resolved in the proposed state mapping.** Independent `kept` and clearing Done address these cases. |
| Restore reused revisions and could duplicate notifications | **Properly open, not solved.** Generation rotation and vector 10 establish targets; the proposed delivery mechanism is insufficient by itself. |
| Agents could subscribe, create webhooks, trigger extraction, or export notification text | **Resolved as default constraints.** Enforcement and indirect effects remain unproved. |
| Universal undo, caller-controlled provenance, unspecified annotation rendering, citation overclaims | **Resolved in the stated contract.** Conditional revert, generated provenance, inert text and passage-versus-support distinctions are explicit. |
| Compiled rules trusted hostile predicates or agent labels | **Addressed.** Action restrictions and separate namespaces close the identified direct routes; compiler inputs still need scrutiny below. |
| Signed proxy URLs treated as SSRF protection; IPv6 and redirect credentials omitted | **Addressed in the design.** The [Miniflux advisory](https://github.com/miniflux/v2/security/advisories/GHSA-xwh2-742g-w3wp) and [Go’s zoned-address behaviour](https://pkg.go.dev/net/netip#Prefix.Contains) support those corrections. |
| Miniflux borrowing/porting overclaims and milestone dependency errors | **Resolved in the research.** Copying requires a spike; speculative porting claims are removed; modelling precedes schema. |
| Miniflux delta sync, private OpenAI connectivity, Gemini regional terms | **Resolved and verified:** [`changed_after`](https://miniflux.app/docs/api.html), [Secure MCP Tunnel](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels), [regional data terms](https://ai.google.dev/gemini-api/terms). |

**3. Threat model — judgement**

It is now honest as a statement of intended constraints: it names external-agent tools, compromised components, notification timing and opt-ins as limitations. It is not evidence that those constraints hold.

A remaining **client-mediated selection channel** needs explicit treatment. An injected agent can choose between attacker-authored entries or source URLs according to a secret, then surface the chosen entry/link. If opening the alert or brief automatically loads that entry’s remote images or previews, the attacker can observe the selection. Requiring a URL to appear in a source does not prevent this.

Human-directed browsing can be explicitly excluded from the guarantee. Automatic previews, offline media downloads and image-proxy requests caused by agent-selected content must remain inside invariant 2’s audit. Merely making `read_entry` fetch-free is insufficient.

I found no additional autonomous exit through the eight listed tools under their stated default constraints.

**4. Non-blocking items**

For step 1, extend the attack cases:

- **Restore before notification acceptance:** the restored database contains neither notification nor `op_id`. Holding restored pending rows cannot catch its replay. Generation checking currently covers cursors and `base_rev`, not the notification request.
- **Fresh versus stale compatibility writes:** Google Reader supplies no `base_rev` or action timestamp. Test a fresh read after native deliberate unread, plus stale unread/star/unstar operations. Record the unavoidable compatibility tradeoff.
- **Bootstrap/range ordering:** bootstrap may already contain a newer per-register state when an older range replays. Model revision comparisons and crashes between bulk chunks explicitly.

For step 0, exclude NewsBlur’s [`newsblur_get_original_text`](https://github.com/samuelclay/NewsBlur/blob/main/newsblur_mcp/newsblur_mcp/tools/stories.py) from a stored-content allowlist; it fetches the source website. Keep ntfy publishing constrained to a fixed destination.

Two later-stage factual corrections:

- `sandbox=""` does **not** make links inert; [navigation within the frame remains possible](https://html.spec.whatwg.org/multipage/browsers.html#sandboxing).
- Compiler-visible feed names can be publisher-controlled [channel metadata](https://www.rssboard.org/rss-specification). Treat them as untrusted input too.

**5. Verdict — judgement**

**YES: fit to guide steps 0 and 1.** The model must be allowed to reject the proposed mechanisms, including restore handling and compatibility conflict policy.