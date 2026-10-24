# 0013. Usage Summary cascades to a parent NineGuard as an aggregate only

- Status: Accepted
- Date: 2026-10-19
- Related: [0009](0009-upstream-quota-tracking-and-interception.md) (Cascaded Quota)

## Context

NineGuards are chained (`user -> my-nineguard -> comp-nineguard -> 9router`). `/quota` already cascades: it shows the parent's view of this instance's key. Users asked for `/usage` to cascade the same way.

The parent only knows this instance by the one API Key it holds. Every response the parent sees for one user, the child has already seen, so the parent has no extra per-user facts.

Options considered:

1. Local only.
2. Local Usage Summary, plus the parent's Usage Summary for this instance's key, shown as a separate **Upstream Usage** section.
3. Option 2, plus per-user attribution at the parent: the child forwards an origin-key header, the parent stores and filters by it.

## Decision

Option 2.

- The child calls the parent's `GET /v1/usage` with the same key and range, for each Provider whose quota type is `nineguard`, cached like Cascaded Quota.
- The result is shown beside the local summary, labelled as shared by all users of this instance. It is never added into the user's own numbers.
- It is shown to key holders only when Estimated Cost is visible to them (same global switch and per-key override), because it reveals combined usage of everyone on the instance.
- A parent without `/v1/usage` (older version) or an unreachable parent omits the section; it never fails the command.

Option 3 was rejected: a new header, column and trust rule, to reproduce numbers the child already has.

## Consequences

- Users see both "my usage" and "how much this gateway draws from the company gateway", at the parent's prices.
- No per-user identity crosses the chain.
- Adding attribution later (option 3) stays possible without changing this endpoint's shape.
