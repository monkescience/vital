# Changelog

## [v0.5.0](https://github.com/monkescience/vital/compare/v0.4.0...v0.5.0) (2026-04-18)

### ⚠ BREAKING CHANGES

- drop json/yaml tags from LogConfig ([1aa79c6](https://github.com/monkescience/vital/commit/1aa79c627192e1395bef331dca802018b41c7e9b))
- drop middleware duplicated by chi ([0a32761](https://github.com/monkescience/vital/commit/0a327613548f589ab2715884c314c073906552bc))
- enforce Timeout via http.TimeoutHandler ([74bdc4d](https://github.com/monkescience/vital/commit/74bdc4d3946f14161c0f24fb4c8dbe666ad6d18e))
### Features

- drop middleware duplicated by chi ([0a32761](https://github.com/monkescience/vital/commit/0a327613548f589ab2715884c314c073906552bc))
- enforce Timeout via http.TimeoutHandler ([74bdc4d](https://github.com/monkescience/vital/commit/74bdc4d3946f14161c0f24fb4c8dbe666ad6d18e))
### Bug Fixes

- use request context for ready response write ([e988a99](https://github.com/monkescience/vital/commit/e988a99c48084afee4c7dc10aa7d84dc520072ae))
- return a copy from Registry.Keys ([66c25ce](https://github.com/monkescience/vital/commit/66c25cec02a623f8b3ffc9e822e73d4f92986cab))
- replay shutdown hook errors on repeat Stop calls ([bfd1366](https://github.com/monkescience/vital/commit/bfd136642a5e4345f8f53245680a313e84b64952))
- log shutdown signal before stopping server ([2127030](https://github.com/monkescience/vital/commit/2127030438048709dd3b3cf773ee3dd4b6f3f381))
- log hijacked connections in RequestLogger ([142e0fb](https://github.com/monkescience/vital/commit/142e0fbe83debfb9d0a5f0e92e87bf6e57c7e191))

## [v0.4.0](https://github.com/monkescience/vital/compare/v0.3.0...v0.4.0) (2026-03-29)

### ⚠ BREAKING CHANGES

- remove custom OTel middleware, extract trace context from OTel span directly ([0684e85](https://github.com/monkescience/vital/commit/0684e850723a0a46f179b654a59f51c2011b1b2d))
### Features

- remove custom OTel middleware, extract trace context from OTel span directly ([0684e85](https://github.com/monkescience/vital/commit/0684e850723a0a46f179b654a59f51c2011b1b2d))

## [v0.3.0](https://github.com/monkescience/vital/compare/v0.2.1...v0.3.0) (2026-03-27)

### ⚠ BREAKING CHANGES

- split middleware into per-file layout and remove trace context getters ([013bba6](https://github.com/monkescience/vital/commit/013bba685a74175ce5633ae7b427524d991329bd))

## [v0.2.1](https://github.com/monkescience/vital/compare/v0.2.0...v0.2.1) (2026-03-27)

### Features

- add body size limit middleware ([18710c0](https://github.com/monkescience/vital/commit/18710c02fd56854d85da040a439555a5a6dfb0f0))
### Bug Fixes

- **deps:** update opentelemetry-go monorepo to v1.42.0 (#18) ([3e22020](https://github.com/monkescience/vital/commit/3e2202088afbad61cc1d3e118c227cf518d23b31))
### Performance Improvements

- reduce middleware allocations in OTel instrumentation ([8451122](https://github.com/monkescience/vital/commit/845112290644d5d4437dc6d5760372263a4d9b75))

## [v0.2.0](https://github.com/monkescience/vital/compare/v0.1.0...v0.2.0) (2026-03-26)

### ⚠ BREAKING CHANGES

- **server:** return lifecycle errors and harden shutdown hooks ([f499d5f](https://github.com/monkescience/vital/commit/f499d5fbf321708cd22b8f70e4b41428b83fba5b))
### Features

- **server:** return lifecycle errors and harden shutdown hooks ([f499d5f](https://github.com/monkescience/vital/commit/f499d5fbf321708cd22b8f70e4b41428b83fba5b))
### Bug Fixes

- **http:** correct Hijack flag and Flush header delegation in response wrapper ([abf2cc3](https://github.com/monkescience/vital/commit/abf2cc3fea47d47760ad23916e675c29ef70f966))
- **http:** prevent duplicate WriteHeader delegation in response wrapper ([76ae647](https://github.com/monkescience/vital/commit/76ae6470b6df8357f81df2a26a0d860e8dda3477))
- **otel:** keep trace propagation inbound only ([edb8529](https://github.com/monkescience/vital/commit/edb85293bf44e9e36d7c2168e0d91d740484e6d8))
- **http:** harden panic recovery and JSON responses ([fb4bf4a](https://github.com/monkescience/vital/commit/fb4bf4afaa20a24c78a6a4322286003819b6c646))

## [0.1.0](https://github.com/monkescience/vital/compare/v0.0.1...v0.1.0) (2026-03-13)


### ⚠ BREAKING CHANGES

* add lifecycle probes and split telemetry middleware
* harden middleware behavior and fail OTel setup on init errors
* use functional options pattern for ProblemDetail
* remove deprecated TraceContext and add context to RespondProblem
* remove body parsing utilities

### Features

* add `ContextHandler`, `ProblemDetail` implementations, and trace context middleware ([ffbd510](https://github.com/monkescience/vital/commit/ffbd510c8bc68b3d6e2f4d2bd8175038a540586e))
* add `LogConfig` and `NewHandlerFromConfig` with validation and tests ([4d004d0](https://github.com/monkescience/vital/commit/4d004d00a5c3b3ed5d1d119e8b79526e5dafbba4))
* add extensive health check tests and set content-type in responses ([8ad50e1](https://github.com/monkescience/vital/commit/8ad50e1c5ce7016844ae04f8e9d8a03e171afd67))
* add golangci-lint configuration and refactor health check handlers ([25a2aac](https://github.com/monkescience/vital/commit/25a2aac3e2c0a2b9913068882f15761f215d68b0))
* add health check endpoints and initial module setup ([ca8d69f](https://github.com/monkescience/vital/commit/ca8d69f7c3c3e9043602f836ddf7988c6a267e1d))
* add lifecycle probes and split telemetry middleware ([552c340](https://github.com/monkescience/vital/commit/552c340d573ffa480671458c1b697fef78a73f8a))
* add TLS support to server and extensive unit tests ([f4b9930](https://github.com/monkescience/vital/commit/f4b99301877b37980b358e50a35a1ecdaacbceff))
* **body:** add request body parsing with required validation ([39d43f8](https://github.com/monkescience/vital/commit/39d43f8ed93cc3730addc553bb57277c6fec4dd6))
* enhance health check functionality with context and timeout support ([da1d94b](https://github.com/monkescience/vital/commit/da1d94b5a81c02c49a6a6d1079d4564232de6250))
* implement new HTTP server with configurable options and graceful shutdown ([bc16c74](https://github.com/monkescience/vital/commit/bc16c7410c6744fa09472a6022c894710c49d2e2))
* improve code formatting and add golangci-lint fmt target ([dae404a](https://github.com/monkescience/vital/commit/dae404aae231dae9888356aa87d9fe4821f3580f))
* **otel:** add OpenTelemetry middleware with traces and metrics ([4483a59](https://github.com/monkescience/vital/commit/4483a59edd226e11f57f2268c63959a07fe85bfe))
* refactor health check handler to use functional options for configuration ([80c7cea](https://github.com/monkescience/vital/commit/80c7cea95514ea827f9c3714d1dcf938e19bf59d))
* remove `Host` field and related logic from health checks ([84c9628](https://github.com/monkescience/vital/commit/84c96280546eda8ee7eb8157643d0b8414629233))
* **timeout:** add timeout middleware with ProblemDetail response ([b5c2f4e](https://github.com/monkescience/vital/commit/b5c2f4e60c29de2d13b2b3002a635949fc86062a))
* update Go version to 1.25.4 and enhance health check handler documentation ([79d4c39](https://github.com/monkescience/vital/commit/79d4c39138fb66f642b9fdfdfd523e5b6e2519d4))


### Bug Fixes

* harden middleware behavior and fail OTel setup on init errors ([1f3779b](https://github.com/monkescience/vital/commit/1f3779b6fe72a784b03bee6bee60cf1930c0e692))
* resolve race conditions and linter violations ([8be8946](https://github.com/monkescience/vital/commit/8be8946b02a827872e01ca00a90f3281e30366b6))


### Code Refactoring

* remove body parsing utilities ([0b36df7](https://github.com/monkescience/vital/commit/0b36df7ee6d630216f19c8dce8e0ac7731ec72e8))
* remove deprecated TraceContext and add context to RespondProblem ([b677e84](https://github.com/monkescience/vital/commit/b677e841eb0e39c34efeff64e5f31c46b87db5f5))
* use functional options pattern for ProblemDetail ([746ff30](https://github.com/monkescience/vital/commit/746ff3026bcbf78e51535b1c1d058afa158e987f))
