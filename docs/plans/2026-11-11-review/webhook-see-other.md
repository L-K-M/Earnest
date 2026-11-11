# Webhook redirect outcomes

Written before implementation, following the continuation plan in PR #166.
Baseline: `aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`.

## Candidate

`classifyWebhook` treats HTTP 303 as a definite refusal, so the durable job
is retried and repeated refusals disable its destination. A receiver can
legitimately process a POST before returning 303 with a Location for its
result. Disabling redirect following does not prove the POST was refused.
[RFC 9110 section 15.4.4](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.4.4)
explicitly describes this POST-result behavior. The documented webhook
contract retries only definite failures, never unknown outcomes.

## Verification before a fix

- Ask an independent reviewer to refute reachability and the contract claim,
  including the existing 302 classification test.
- Add a classification regression for 303.
- Add a real local receiver that consumes a POST, records an effect, and
  returns 303 with a Location. Exercise the outbound client and durable
  Postgres job, advance its clock beyond retry backoff, and verify it sends
  once, never follows the Location, and does not disable the webhook.
- Observe the regression fail before changing production code. A failing
  expectation without the contract evidence is not enough to accept a fix.

## Intended minimal change, if confirmed

Treat redirect responses as ambiguous, like a 408 or a lost response.
Preserve success, proven pre-send failures, non-redirect status behavior,
signatures, redirect prohibition, destination authorization and failure
backoff. Do not claim a redirect proves successful delivery. Run webhook
regressions, surrounding service tests, vet and the full Postgres-backed
Go race suite before merge.

## Review round 1: extend the reproduced case before further code changes

The initial 303 fix passed its regressions. Review identified that 302 can
use the same POST-result pattern (RFC 9110 section 15.4.3). The existing
302 test asserted refusal but supplied no evidence that processing could
not have happened. It must not preserve the same duplicate-effect bug.

Extend the receiver regression to 301, 302, 303, 307 and 308, and classify
the entire 3xx response class as unknown: refusing to follow a Location
never proves rejection of the original POST. This is conservative even
when a particular receiver redirected before processing. Observe the
additional failures before extending production handling.

Unknown outcomes deliberately discard the job after one attempt. This can
lose an event that was not processed, just like a lost response or a crash;
retrying could instead repeat an effect. Document that trade-off publicly,
including that active destinations and refusal counters do not acknowledge
delivery. New delivery history, logging and metrics are useful follow-ups,
not a prerequisite for preserving this existing at-most-once contract.
