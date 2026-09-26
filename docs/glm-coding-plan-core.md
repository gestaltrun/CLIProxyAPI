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
- Projection of quota and credential observation status into the existing management surface without changing inference eligibility.

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

Discovery and quota probes read the API key, site, organization, and project from the synthesized auth attributes and fall back to the auth file fields. At startup the core manager registers models for auths loaded by the file token store, which carry only the file fields until the watcher replaces them with synthesized auths, so both versions must resolve the same credentials. A missing API key is a `configuration` failure. Discovery failures are logged at debug level with the auth ID and the error, which never contains the key.

A static list is not evidence of models granted to a key. If a later implementation adds a fallback catalog, the response and management surface must distinguish fallback or stale data from a successful live catalog.

### Static management definitions

`GET /v0/management/model-definitions/glm` returns the built-in GLM definitions (`glm-5-turbo`, `glm-5.1`, `glm-5.2`, `glm-5.3`, `glm-5.3-flash`) for management clients that label models before a key is enrolled. They carry display metadata and reasoning levels only; `ModelsForAuth` remains the source of models routed for a key.

Every channel of `GET /v0/management/model-definitions/{channel}` adds two integer fields to each model that has a known window:

| Field | Meaning | Source |
| --- | --- | --- |
| `context_window` | Default context budget a client should plan for | Codex channel: `context_window` from the Codex client catalog. Other channels, and Codex models whose catalog entry omits it: `context_length`. |
| `max_context_window` | Largest context the model accepts | Codex channel: `max_context_window` from the Codex client catalog. Other channels: `context_length`. Never smaller than `context_window`. |

For example, Codex `gpt-6-astra` reports `context_window: 272000` and `max_context_window: 872000`; every GLM definition reports `202752` for both. Existing fields such as `context_length` and `max_completion_tokens` are unchanged.

## Reasoning policy

The GLM policy is implemented as a provider applier after canonical `ThinkingConfig` normalization.

- GLM-5.3 preserves `low`, maps medium/high to `high`, and maps xhigh/max equivalents to `max`.
- Older GLM models map low/medium/high to `high` and xhigh/max equivalents to `max`.
- Anthropic client requests use the existing translator and exit through the OpenAI-compatible Coding Plan Chat Completions endpoint, where GLM reasoning is expressed as `reasoning_effort`. A native Anthropic GLM executor and `output_config.effort` are outside this delivery.

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

Quota polling is observation-only. Usage-endpoint authentication and quota percentage do not modify `Auth.Unavailable`, `Quota.Exceeded`, cooldown deadlines, or inference status. The inference response path and existing scheduler remain the only authorities for request eligibility because a key may be allowed to infer while lacking permission to read the usage endpoint. The management API exposes observation status, credential observation, freshness, and retained stale values for diagnosis. `GET /v0/management/auth-files` includes each GLM file's `auth_index` and quota envelope. `POST /v0/management/auth-files/quota?name=` probes one file-backed credential on demand and returns the same envelope.

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
