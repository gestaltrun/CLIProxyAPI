# GLM Coding Plan core provider

Core implementation issue: [gestaltrun/CLIProxyAPI#1](https://github.com/gestaltrun/CLIProxyAPI/issues/1)

Product delivery coordination: [gestaltrun/deepseek-harness-gestalt#649](https://github.com/gestaltrun/deepseek-harness-gestalt/issues/649). The core pull request closes only the core repository issue. The product repository tracks integration separately and will pin the reviewed core commit when it updates its gitlink.

## Decision

Implement GLM Coding Plan as a narrow CLIProxyAPI provider using a user-supplied Coding Plan API key. The provider has no OAuth login or refresh lifecycle and does not represent a PayG key as a Coding Plan credential.

The implementation is an independent MIT implementation of observable upstream protocol behavior. `Wei-Shaw/sub2api@98d86915becae9fe9491a91ffc6defd5235c8d2b` is the behavioral reference; its LGPL-covered source expression, comments, internal data types, tests, scheduler, and persistence design are not copied. The core baseline is `gestaltrun/CLIProxyAPI@7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974` (`v7.2.155`).

## Module boundary

The GLM module owns only provider-specific facts:

- Coding Plan auth parsing and non-secret endpoint/team attributes.
- CN and international Coding Plan endpoint resolution.
- Dynamic model discovery for one auth.
- GLM reasoning translation through the canonical thinking provider seam.
- Coding Plan quota request construction, response parsing, snapshot freshness, and poll lifecycle.
- Projection of quota availability into the existing auth manager.

The existing OpenAI-compatible executor owns generic request translation, streaming, token counting, per-auth proxy selection, and HTTP execution. The existing auth manager and registry own credential selection, cooldown state, model registration, and retry orchestration.

The GLM module does not own a database, account scheduler, Composite router, billing implementation, Web console, PostgreSQL, Redis, or a second protocol gateway.

## Provider contract

### Credential

A GLM Coding Plan auth contains a secret API key and explicit non-secret attributes:

- site: `cn` or `international`;
- account mode: always `coding` for this provider;
- optional model prefix and per-auth proxy through existing host fields;
- optional team organization and project identifiers.

No access token, refresh token, expiry, OAuth callback, or browser login is synthesized. A refresh call for this static credential returns the unchanged auth or the repository's established static-key result.

### Endpoints and authentication

| Operation | CN | International | Authentication |
|---|---|---|---|
| Chat Completions base | `https://open.bigmodel.cn/api/coding/paas/v4` | `https://api.z.ai/api/coding/paas/v4` | `Authorization: Bearer <key>` |
| Models | `<coding-base>/models` | `<coding-base>/models` | `Authorization: Bearer <key>` |
| Quota | `https://open.bigmodel.cn/api/monitor/usage/quota/limit` | `https://api.z.ai/api/monitor/usage/quota/limit` | `Authorization: <key>` |

A team quota request appends `type=2`, sets `bigmodel-organization`, and sets `bigmodel-project` only when configured. Team behavior is request-shape tested until approved live credentials exist.

Custom upstream bases do not imply permission to send their key to an official quota host. The resolver must either recognize the selected official site or report quota observation as unsupported for that base.

## Model catalog

`ModelsForAuth` reads the selected Coding Plan `/models` response and returns a deduplicated non-empty catalog. It distinguishes configuration, authentication, upstream status, malformed or oversized response, and empty-catalog failures.

A static list is not evidence of models granted to a key. If a later implementation adds a fallback catalog, the response and management surface must distinguish fallback or stale data from a successful live catalog.

## Reasoning policy

The GLM policy is implemented as a provider applier after canonical `ThinkingConfig` normalization.

- GLM-5.3 preserves `low`, maps medium/high to `high`, and maps xhigh/max equivalents to `max`.
- Older GLM models map low/medium/high to `high` and xhigh/max equivalents to `max`.
- The Anthropic-compatible GLM-5.3 request uses `thinking.type=enabled` and `output_config.effort=low|high|max` when that exit protocol is selected.

The generic translator remains provider-neutral.

## Quota state

The quota parser consumes `data.limits` entries:

- `type=TOKENS_LIMIT` is authoritative for token windows;
- `unit=3` identifies the five-hour window;
- `unit=6` identifies the weekly window;
- `percentage` is used utilization and is not silently clamped;
- `nextResetTime` accepts a supported time string, Unix seconds, or Unix milliseconds;
- `CREDIT_LIMIT` is a labeled fallback only when no token-limit entry exists.

A successful snapshot records both windows when present, the plan level when present, observation time, and reset times. A 401 or 403 records invalid current credential observation without deleting the last successful snapshot; retained values are explicitly stale. Missing or malformed windows do not become zero usage.

The poller coalesces concurrent refreshes for one auth, follows the service lifecycle, and stops all timers and workers on cancellation. Quota polling is separate from credential refresh because the Coding Plan API key is static.

## Error and retry ownership

Provider code classifies errors for existing auth-manager behavior instead of implementing a second scheduler:

- 401/403: invalid or forbidden credential; no OAuth refresh attempt;
- 402: entitlement or payment failure;
- 429: rate or quota response, preserving the existing bounded `Retry-After` contract;
- ordinary deterministic 400: no credential rotation;
- account-specific model-not-found: rotation only when the existing failover contract identifies it as account-specific;
- transport and retryable 5xx: existing retry and credential-selection paths.

Secret API keys are excluded from errors, logs, model metadata, quota snapshots, and management responses. Team identifiers are emitted only where required for the upstream request.

## Implementation slices

1. Add the auth/config data and official-site endpoint resolver with request-construction tests.
2. Add per-auth dynamic model discovery and registry integration.
3. Add the GLM thinking provider applier and reasoning matrix tests.
4. Add quota parsing, stale snapshot state, poll lifecycle, and auth availability projection.
5. Add one assembled fake-upstream path through the real auth manager and OpenAI-compatible executor, then update user configuration documentation.

Each slice must remain independently reviewable. Shared helpers belong in the package that owns the policy; `internal/runtime/executor/` remains limited to executors and executor tests.

## Test plan

### Keyless tests

- Parse CN and international Coding Plan auths without creating OAuth fields.
- Reject missing keys, invalid site values, and PayG mode in this provider.
- Resolve exact inference, model, personal quota, and team quota URLs.
- Verify Bearer inference/model authentication and raw quota authentication.
- Verify team query and headers with and without project ID.
- Exercise successful, empty, malformed, oversized, unauthorized, and non-2xx model catalogs.
- Cover GLM-5.3 and older GLM reasoning for low, medium, high, xhigh, and max.
- Parse unit 3 and unit 6 independent of response order.
- Parse numeric and string utilization plus seconds, milliseconds, and time-string resets.
- Prefer token limits over credit limits and label credit-only fallback.
- Preserve the last successful snapshot after 401/403 and expose stale state.
- Coalesce concurrent quota refreshes for one auth.
- Stop polling deterministically through cancellation without wall-clock sleep assertions.
- Prove ordinary 400 does not rotate credentials and model-not-found uses only the existing explicit failover path.
- Prove errors, logs, snapshots, and model metadata contain no API key.

### Assembled test

Run an OpenAI-compatible request through the real auth manager and existing OpenAI-compatible executor into a fake GLM server. Assert the Coding Plan path and Bearer header, then verify the translated response. Use a separate fake quota route to assert raw Authorization and stale-snapshot behavior.

### Required implementation evidence

Run focused tests for every owning package and:

```bash
go build -o test-output ./cmd/server && rm test-output
```

Live CN and international personal-key tests are optional environment-gated evidence. Team support must not be reported as live-tested until an approved team credential is used in a dedicated secret-safe lane.
