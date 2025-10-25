# Changelog

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
