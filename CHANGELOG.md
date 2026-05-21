# Changelog

## [2.2.0](https://github.com/onecli/onecli-cli/compare/v2.1.0...v2.2.0) (2026-05-21)


### Features

* add disconnect api endpoints ([#67](https://github.com/onecli/onecli-cli/issues/67)) ([f458d89](https://github.com/onecli/onecli-cli/commit/f458d891964a08da63a949eea1f631c397904399))

## [2.1.0](https://github.com/onecli/onecli-cli/compare/v2.0.1...v2.1.0) (2026-05-20)


### Features

* add org subcommands for apps, connections, rules, and secrets ([#65](https://github.com/onecli/onecli-cli/issues/65)) ([5411e02](https://github.com/onecli/onecli-cli/commit/5411e029e6ace0d31fc81f484af8f8778c5eb119))

## [2.0.1](https://github.com/onecli/onecli-cli/compare/v2.0.0...v2.0.1) (2026-05-19)


### Bug Fixes

* bc for api ([#63](https://github.com/onecli/onecli-cli/issues/63)) ([30e73b7](https://github.com/onecli/onecli-cli/commit/30e73b799199d41077a94749898cf58bfdba5fd8))

## [2.0.0](https://github.com/onecli/onecli-cli/compare/v1.7.3...v2.0.0) (2026-05-19)


### ⚠ BREAKING CHANGES

* All API endpoints now use /v1 prefix instead of /api. The default base URL is now api.onecli.sh. Clients using the previous /api prefix must update their configuration.

### Features

* migrate API prefix from /api to /v1 and default to api.onecli.sh ([#61](https://github.com/onecli/onecli-cli/issues/61)) ([c960ba3](https://github.com/onecli/onecli-cli/commit/c960ba3fd6fbb4da8ac062d062425e245c20cd5a))

## [1.7.3](https://github.com/onecli/onecli-cli/compare/v1.7.2...v1.7.3) (2026-05-19)


### Bug Fixes

* accept openai as valid secret type in secrets create ([#59](https://github.com/onecli/onecli-cli/issues/59)) ([5d236e4](https://github.com/onecli/onecli-cli/commit/5d236e4bb3d699430142a1573435836cb210588c))
* require --confirm flag for project deletion ([#57](https://github.com/onecli/onecli-cli/issues/57)) ([eac5b4f](https://github.com/onecli/onecli-cli/commit/eac5b4fe2e5c2817eb774d3f1e8b2742f129c6d2))

## [1.7.2](https://github.com/onecli/onecli-cli/compare/v1.7.1...v1.7.2) (2026-05-18)


### Bug Fixes

* show server version and status in version command ([#31](https://github.com/onecli/onecli-cli/issues/31)) ([e13fa7e](https://github.com/onecli/onecli-cli/commit/e13fa7e1502a7386d0d6fb9e3a0756ce0d9cfb93))
* use canonical onecli.sh domain without www prefix ([#55](https://github.com/onecli/onecli-cli/issues/55)) ([0f334f7](https://github.com/onecli/onecli-cli/commit/0f334f7bc2967883e72da651e3da037b6cb08464))

## [1.7.1](https://github.com/onecli/onecli-cli/compare/v1.7.0...v1.7.1) (2026-05-17)


### Bug Fixes

* auto-install gateway detection hook on agent launch ([#54](https://github.com/onecli/onecli-cli/issues/54)) ([8568f74](https://github.com/onecli/onecli-cli/commit/8568f74be86c27cec663e01b9a30c5e819211832))
* replace raw Go errors with user-friendly messages ([#51](https://github.com/onecli/onecli-cli/issues/51)) ([6c13313](https://github.com/onecli/onecli-cli/commit/6c133134deec70918fe59d94f7b44d71f59c7ed2))
* replace raw network errors with user-friendly gateway messages ([#49](https://github.com/onecli/onecli-cli/issues/49)) ([177c968](https://github.com/onecli/onecli-cli/commit/177c96894331ca9351480944bc6efa2c6ac2fb46))
* surface gateway credential warnings before launching agent ([#53](https://github.com/onecli/onecli-cli/issues/53)) ([227f251](https://github.com/onecli/onecli-cli/commit/227f2516e378df989bf69374f8eb4841d6f564d0))

## [1.7.0](https://github.com/onecli/onecli-cli/compare/v1.6.1...v1.7.0) (2026-05-07)


### Features

* unified gateway skill with API fetch ([#47](https://github.com/onecli/onecli-cli/issues/47)) ([e8e272d](https://github.com/onecli/onecli-cli/commit/e8e272ddf3ec1f89373fa7e2f5ad483947e2b50c))

## [1.6.1](https://github.com/onecli/onecli-cli/compare/v1.6.0...v1.6.1) (2026-05-07)


### Bug Fixes

* include system CAs in gateway CA bundle ([#45](https://github.com/onecli/onecli-cli/issues/45)) ([5ec9556](https://github.com/onecli/onecli-cli/commit/5ec95566d8ca5f45e945bbab8a57e1ddc43cf0c1))

## [1.6.0](https://github.com/onecli/onecli-cli/compare/v1.5.3...v1.6.0) (2026-05-03)


### Features

* split Secrets tab into Custom and LLMs tabs on Connections page ([#42](https://github.com/onecli/onecli-cli/issues/42)) ([149f451](https://github.com/onecli/onecli-cli/commit/149f451a11b2b667c25203844a6bc40020e80c19))

## [1.5.3](https://github.com/onecli/onecli-cli/compare/v1.5.2...v1.5.3) (2026-05-02)


### Bug Fixes

* use ONECLI_URL template var for OAuth connect URL in gateway skill ([#40](https://github.com/onecli/onecli-cli/issues/40)) ([98e66d2](https://github.com/onecli/onecli-cli/commit/98e66d214aab3b531808bdf412bb61988d5bcc45))

## [1.5.2](https://github.com/onecli/onecli-cli/compare/v1.5.1...v1.5.2) (2026-05-02)


### Bug Fixes

* strip ANTHROPIC_API_KEY to avoid Claude Code prompt ([#38](https://github.com/onecli/onecli-cli/issues/38)) ([9d977e8](https://github.com/onecli/onecli-cli/commit/9d977e8e49da631db110033647d773c45355a57a))

## [1.5.1](https://github.com/onecli/onecli-cli/compare/v1.5.0...v1.5.1) (2026-05-01)


### Bug Fixes

* resolve Electron proxy auth and improve gateway skill UX ([#36](https://github.com/onecli/onecli-cli/issues/36)) ([4a69e32](https://github.com/onecli/onecli-cli/commit/4a69e32a5f0be063d9da5fc520148e6209abb355))

## [1.5.0](https://github.com/onecli/onecli-cli/compare/v1.4.1...v1.5.0) (2026-04-28)


### Features

* add project scoping and restore agent secret commands ([#34](https://github.com/onecli/onecli-cli/issues/34)) ([f0c567c](https://github.com/onecli/onecli-cli/commit/f0c567c4bb26b9ac8eaada0f58c5c82814d64edd))
* support query param injection in secrets CLI ([#32](https://github.com/onecli/onecli-cli/issues/32)) ([9a04dee](https://github.com/onecli/onecli-cli/commit/9a04dee92c45ace05b182432e1caea6a0df2b2b5))

## [1.4.1](https://github.com/onecli/onecli-cli/compare/v1.4.0...v1.4.1) (2026-04-23)


### Bug Fixes

* dynamic skill generation and correct connection polling ([#27](https://github.com/onecli/onecli-cli/issues/27)) ([1e93f8d](https://github.com/onecli/onecli-cli/commit/1e93f8d8c6e139fc65a29e4db59bf4da3f4f1099))
* rename ONECLI_DASHBOARD_URL to ONECLI_URL to match SDK convention ([#29](https://github.com/onecli/onecli-cli/issues/29)) ([d2d28a6](https://github.com/onecli/onecli-cli/commit/d2d28a63d3e93ce8b59d1ab9d423d59586cde159))

## [1.4.0](https://github.com/onecli/onecli-cli/compare/v1.3.1...v1.4.0) (2026-04-23)


### Features

* add "onecli run" command to wrap agent processes with gateway access ([#23](https://github.com/onecli/onecli-cli/issues/23)) ([87172a1](https://github.com/onecli/onecli-cli/commit/87172a10e2d8e9724fb2cab26b59ceedbccb806c))

## [1.3.1](https://github.com/onecli/onecli-cli/compare/v1.3.0...v1.3.1) (2026-04-23)


### Bug Fixes

* use server-side proxy for version lookup to avoid github api rate limits ([#24](https://github.com/onecli/onecli-cli/issues/24)) ([74a9211](https://github.com/onecli/onecli-cli/commit/74a9211d11e1e6e83120a48eead819a16b458860))

## [1.3.0](https://github.com/onecli/onecli-cli/compare/v1.2.1...v1.3.0) (2026-04-20)


### Features

* add data migration from self-hosted to cloud ([#21](https://github.com/onecli/onecli-cli/issues/21)) ([c34d2e7](https://github.com/onecli/onecli-cli/commit/c34d2e77b1ccf3cb03ee31d2fa066f9822ffc2eb))

## [1.2.1](https://github.com/onecli/onecli-cli/compare/v1.2.0...v1.2.1) (2026-04-06)


### Bug Fixes

* add apps get command with server-side hint passthrough ([#19](https://github.com/onecli/onecli-cli/issues/19)) ([9cef29e](https://github.com/onecli/onecli-cli/commit/9cef29ec4c273adee3f8a29b3ce739cfd2755b5f))

## [1.2.0](https://github.com/onecli/onecli-cli/compare/v1.1.1...v1.2.0) (2026-04-06)


### Features

* add apps connect/list/disconnect commands for OAuth app connections ([#12](https://github.com/onecli/onecli-cli/issues/12)) ([49cccb2](https://github.com/onecli/onecli-cli/commit/49cccb26e64c89ede99b65bb54e5bf6bab1fb25f))


### Bug Fixes

* add contextual dashboard hint as first property in all JSON responses ([#13](https://github.com/onecli/onecli-cli/issues/13)) ([b127ac2](https://github.com/onecli/onecli-cli/commit/b127ac2bcc41373ed7b13cad84c4fac7588c08f6))
* add warning field to secrets API response ([#11](https://github.com/onecli/onecli-cli/issues/11)) ([eabe16a](https://github.com/onecli/onecli-cli/commit/eabe16a621346b6fddd5f751bcad85098d9a279f))
* unify REST API under /api/apps/:provider ([#15](https://github.com/onecli/onecli-cli/issues/15)) ([d7b61c7](https://github.com/onecli/onecli-cli/commit/d7b61c705f0ff69daf50a1b545af1dca4f4c1fdd))

## [1.1.1](https://github.com/onecli/onecli-cli/compare/v1.1.0...v1.1.1) (2026-04-05)


### Bug Fixes

* add contextual dashboard hint as first property in all JSON responses ([#8](https://github.com/onecli/onecli-cli/issues/8)) ([cb64385](https://github.com/onecli/onecli-cli/commit/cb64385180b1633284351bda25e3055060e0c35c))

## [1.1.0](https://github.com/onecli/onecli-cli/compare/v1.0.1...v1.1.0) (2026-03-23)


### Features

* add policy rules commands with input hardening ([#7](https://github.com/onecli/onecli-cli/issues/7)) ([871ebc4](https://github.com/onecli/onecli-cli/commit/871ebc4ea1e6a801e68aff3134a394519f599ce3))


### Bug Fixes

* add curl install command to README ([#5](https://github.com/onecli/onecli-cli/issues/5)) ([56cc104](https://github.com/onecli/onecli-cli/commit/56cc104cd1845fda79cd42dde01e9c0eb15012a3))

## [1.0.1](https://github.com/onecli/onecli-cli/compare/v1.0.0...v1.0.1) (2026-03-17)


### Bug Fixes

* set-secrets automatically switches agent to selective mode ([#3](https://github.com/onecli/onecli-cli/issues/3)) ([fd1131b](https://github.com/onecli/onecli-cli/commit/fd1131b4d7a33c3837e70302b24d306f9c37eb29))

## 1.0.0 (2026-03-17)


### Features

* add onecli CLI tool for managing agents, secrets, and configuration ([#1](https://github.com/onecli/onecli-cli/issues/1)) ([aa3628b](https://github.com/onecli/onecli-cli/commit/aa3628b53f6619187d0d82442ffa48582c6d0357))
