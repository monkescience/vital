# Changelog

## [0.1.0](https://github.com/monkescience/vital/compare/v0.0.1...v0.1.0) (2026-02-20)


### ⚠ BREAKING CHANGES

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
