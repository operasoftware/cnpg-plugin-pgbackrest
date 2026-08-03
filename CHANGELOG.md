# Changelog

## [0.6.1](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.6.0...v0.6.1) (2026-08-03)


### Bug Fixes

* **deps:** Update all non-major go dependencies ([#90](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/90)) ([702bd43](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/702bd43d3a9586453d1e9b476441354e9562c103))
* **deps:** Update controller-gen in Makefile to 0.19.0 ([1ff3bc0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/1ff3bc0f6be096d9e84798b6ff74b61482c1ab81))
* **deps:** Update k8s.io/utils digest to cf1189d ([#89](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/89)) ([4b75501](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/4b7550188244aa35951c34bc70dd47ff6a4e60d6))
* **deps:** Update kubernetes monorepo to v0.36.2 ([#93](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/93)) ([57aae4d](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/57aae4dd0c4638bad8a44c4f2aebaf0973a2e9e9))
* **deps:** Update module github.com/cert-manager/cert-manager to v1.21.1 ([#128](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/128)) ([07421f9](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/07421f9a1ed4d94219561fcc39a5089c4830599b))
* **deps:** Update module sigs.k8s.io/controller-runtime to v0.24.1 ([#123](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/123)) ([971a16d](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/971a16d7370acd1c8dbeebb1287a78b8ad864d0b))
* **deps:** Update module sigs.k8s.io/kustomize/api to v0.21.1 ([#95](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/95)) ([a07d77a](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/a07d77af83997d3188e0c2e1b4e1d467f8d94003))
* **deps:** Update module sigs.k8s.io/kustomize/kyaml to v0.21.1 ([#96](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/96)) ([61b98a0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/61b98a09ce2d12b01478b98bf44dc8b999fe9068))

## [0.6.0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/compare/v0.5.2...v0.6.0) (2026-05-28)


### Features

* Replace TYPE_PATCH with TYPE_EVALUATE in lifecycle capabilities ([5d4f120](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/5d4f12058a522a2a2ade92974d041398bcd58ba0))
* Return proper gRPC error codes for expected conditions ([#67](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/67)) ([50ce1e3](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/50ce1e3cfcba794dbe109bb41dbdf1757fa48227))


### Bug Fixes

* Add clusters/finalizers RBAC permission for OwnerReferencesPermissionEnforcement ([daf6b8c](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/daf6b8c24ef2bca3d69deff1c277c10da91a0334))
* **ci:** Make golangci-lint output visible and fix issues ([d49d9ed](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/d49d9edff4b8b1f68cfe684f234b35b82e363d35))
* Correct restore_command on CNPG 1.29 ([#80](https://github.com/operasoftware/cnpg-plugin-pgbackrest/issues/80)) ([8019b0c](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/8019b0cf64c676ae1c5e34bbdd68ca8ce97d2b75))
* Deduplicate Archive object references to prevent duplicate volume projections ([5471c32](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/5471c32bf6efadac7d9bdf00831ab3fc78ec4ce0))
* Disable end-of-wal flag management during backup restoration ([79108f0](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/79108f0f6de5904c4f8345568b1674b2230afd85))
* Improve reliability of object cache management ([8676127](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/8676127344fbb37359cb803f6f07701aa79909cc))
* Set LeaderElectionReleaseOnCancel to true to enable RollingUpdates ([716d295](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/716d295cd4acefe4452de2066e45c38975cb14b2))
* Update dagger uncommitted module to fix setuptools error ([f98313a](https://github.com/operasoftware/cnpg-plugin-pgbackrest/commit/f98313a77046dc1ff674f76f21a06a1b2ad885ca))

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
