# Changelog

## [0.5.2](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.5.1...v0.5.2) (2026-01-12)


### Bug Fixes

* Parallelarchive e2e test not executed by test runner ([#59](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/59)) ([46938ae](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/46938ae74207c20e47cdcc35fb5c3bb4bf996cc4))
* Wal archive - early return if WAL was already archived ([#45](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/45)) ([420c5db](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/420c5dbc85b191e8ac346ce56f5db08a548e478b))

## [0.5.1](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.5.0...v0.5.1) (2025-12-16)


### Bug Fixes

* Always use absolute paths for WAL upload ([#45](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/45)) ([d98d3ec](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/d98d3ecaecdc56e1414a75b2928cecf00e0e3f4a))

## [0.5.0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.4.1...v0.5.0) (2025-12-15)


### Features

* Parallel WAL upload ([#45](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/45)) ([23d7807](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/23d7807a7ec8c9cdc01ca7c03a439a120a1a8b09))

## [0.4.1](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.4.0...v0.4.1) (2025-11-13)


### Bug Fixes

* Parsing PgbackrestRetention.History ([#43](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/43)) ([5dd340d](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/5dd340dab0e520a7cffd3460c5d3c845b4ce61df))

## [0.4.0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.3.1...v0.4.0) (2025-10-25)


### Features

* Support multiple AWS key types ([27540db](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/27540db9cd43c0529b3bb8ca92f0458427492696))

## [0.3.1](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.3.0...v0.3.1) (2025-09-30)


### Bug Fixes

* Conflicting leaderElectionId between backup plugins ([e7e9a99](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/e7e9a99846a2c0b25541938ff9169bcb5ddc23e7))

## [0.3.0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.2.1...v0.3.0) (2025-09-15)


### Features

* Add configurable `SecurityContext` to `InstanceSidecarConfiguration` ([e5aa70c](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/e5aa70cc9637d0cc76dada825c3642d991fa89b5))

## [0.2.1](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.2.0...v0.2.1) (2025-06-02)


### Bug Fixes

* Patch roles properly ([#7](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/7)) ([7e7a3b7](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/7e7a3b71f49d4ea3c272df67e668cc856265de70))

## [0.2.0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.1.1...v0.2.0) (2025-05-28)


### Features

* Support custom params and parallelism for restore ([#6](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/6)) ([0b8491d](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/0b8491d04cfe06afcf348851bc57633f929fc6b9))


### Bug Fixes

* Add plugin metadata to release-please config ([#5](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/5)) ([2947174](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/2947174b7c8c5fd2df680d95d5435d864611e0fc))
* Update plugin version in metadata ([#5](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/5)) ([81a5547](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/81a554765da4400cc2fafad0c51d10fa40f41985))

## [0.1.1](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.1.0...v0.1.1) (2025-05-13)


### Bug Fixes

* Add missing license header annotation ([#2](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/2)) ([2ced468](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/2ced468a0c90d2f0d209464138c510520b46aba7))

## 0.1.0


### Features

* Initial public release after forking the Barman Cloud plugin.
* Archive resource.
* WAL archiving.
* WAL restore.
* Backups.
* Restore and replication.
* Readme and documentation updates.
