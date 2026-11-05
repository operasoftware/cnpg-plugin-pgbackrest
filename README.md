[![CloudNativePG](./logo/cloudnativepg.png)](https://cloudnative-pg.io/)

# pgBackRest CNPG-I plugin

**Status:** EXPERIMENTAL

Welcome to the codebase of the [pgBackRest](https://pgbackrest.org) CNPG-I
plugin for [CloudNativePG](https://cloudnative-pg.io/).

## Table of contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Usage](#usage)
  - [WAL Archiving](#wal-archiving)
  - [Backup](#backup)
  - [Restore](#restore)
  - [Replica clusters](#replica-clusters)

## Features

This plugin enables continuous backup to object storage for a PostgreSQL
cluster using the [pgbackrest](https://pgbackrest.org) tool suite.

The features provided by this plugin are:

- Data Directory Backup
- Data Directory Restore
- WAL Archiving
- WAL Restoring
- Point-in-Time Recovery (PITR)
- Replica Clusters
- Client-side encryption of both backups and WAL archives

> [!WARNING]
> While all data necessary to use various restore modes in pgbackrest is properly stored
> in the object store, restore is currently tested only with full backup recovery
> to the latest backup. Reports on more advanced recovery attempts are welcome.

This plugin is currently only compatible with S3 object storage.

The following storage solutions have been tested and confirmed to work with
this implementation:

- [MinIO](https://min.io/) – An S3-compatible object storage solution.

Known missing features:

- support for other object storage solutions (GCS, Azure),
- backups from replicas,
- proper support for private certificate authorities.

## Prerequisites

To use this plugin, ensure the following prerequisites are met:

- [**CloudNativePG**](https://cloudnative-pg.io) version **1.25** or newer.
- [**cert-manager**](https://cert-manager.io/) for enabling **TLS communication** between the plugin and the operator.

## Installation

**IMPORTANT NOTES:**

1. The plugin **must** be installed in the same namespace where the operator is
   installed (typically `cnpg-system`).

2. Be aware that the operator's **listening namespaces** may differ from its
   installation namespace. Ensure you verify this distinction to avoid
   configuration issues.

Here’s an enhanced version of your instructions for verifying the prerequisites:

### Step 1 - Verify the Prerequisites

If CloudNativePG is installed in the default `cnpg-system` namespace, verify its version using the following command:

```sh
kubectl get deployment -n cnpg-system cnpg-controller-manager \
  | grep ghcr.io/cloudnative-pg/cloudnative-pg
```

Example output:

```output
image: ghcr.io/cloudnative-pg/cloudnative-pg:1.25.0
```

Ensure that the version displayed is **1.25** or newer.

Then, use the [cmctl](https://cert-manager.io/docs/reference/cmctl/#installation)
tool to confirm that `cert-manager` is correctly installed:

```sh
cmctl check api
```

Example output:

```output
The cert-manager API is ready
```

Both checks are necessary to proceed with the installation.

### Step 2 - Install the pgBackRest Plugin

Use `kubectl` to apply the manifest for the latest commit in the `main` branch:

<!-- x-release-please-start-version -->
```sh
kubectl apply -f \
  https://github.com/operasoftware/cnpg-plugin-pgbackrest/releases/download/v0.9.0/manifest.yaml
```
<!-- x-release-please-end -->

Example output:

```output
customresourcedefinition.apiextensions.k8s.io/archives.pgbackrest.cnpg.opera.com created
serviceaccount/plugin-pgbackrest created
role.rbac.authorization.k8s.io/leader-election-role created
clusterrole.rbac.authorization.k8s.io/archive-editor-role created
clusterrole.rbac.authorization.k8s.io/archive-viewer-role created
clusterrole.rbac.authorization.k8s.io/metrics-auth-role created
clusterrole.rbac.authorization.k8s.io/metrics-reader created
clusterrole.rbac.authorization.k8s.io/plugin-pgbackrest created
rolebinding.rbac.authorization.k8s.io/leader-election-rolebinding created
clusterrolebinding.rbac.authorization.k8s.io/metrics-auth-rolebinding created
clusterrolebinding.rbac.authorization.k8s.io/plugin-pgbackrest-binding created
secret/plugin-pgbackrest--8tfddg42gf created
service/pgbackrest created
deployment.apps/pgbackrest created
certificate.cert-manager.io/pgbackrest-client created
certificate.cert-manager.io/pgbackrest-server created
issuer.cert-manager.io/selfsigned-issuer created
```

After these steps, the plugin will be successfully installed. Make sure it is
ready to use by checking the deployment status as follows:

```sh
kubectl rollout status deployment \
  -n cnpg-system pgbackrest
```

Example output:

```output
deployment "pgbackrest" successfully rolled out
```

This confirms that the plugin is deployed and operational.

### Running More Than One Replica

The `pgbackrest` Deployment can run several replicas. Every replica serves
the CNPG-I interface, so the operator can still reach the plugin when one pod
or its node goes away, and the `Archive` controller runs on the replica that
holds the leader lease. The CloudNativePG operator does not reconcile a
`Cluster` while a plugin it uses is unreachable, which includes setting the
role label on a new primary that the `-rw` Service selects, so a second replica
on another node shortens that gap after a node failure.

> [!NOTE]
> The manifest runs the plugin with a single replica by default.

## Usage

### Defining the `Archive`

An `Archive` object should be created for each pgbackrest storage configuration used in
your PostgreSQL architecture. Below is an example configuration for using a single MinIO repository with encryption enabled:

```yaml
apiVersion: pgbackrest.cnpg.opera.com/v1
kind: Archive
metadata:
  name: minio-store
spec:
  configuration:
    repositories:
      - destinationPath: /
        bucket: backups
        endpointURL: minio:9000
        disableVerifyTLS: true
        encryption: aes-256-cbc
        encryptionKey:
          name: minio
          key: ENCRYPTION_KEY
        s3Credentials:
          region: "dummy"
          accessKeyId:
            name: minio
            key: ACCESS_KEY_ID
          secretAccessKey:
            name: minio
            key: ACCESS_SECRET_KEY
    compression: zst
  instanceSidecarConfiguration:
    resources:
      requests:
        memory: "2Gi"
        cpu: "2"
      limits:
        memory: "2Gi"
        cpu: "2"
    catalogMaintenanceIntervalSeconds: 1800
```

> [!IMPORTANT]
> Unlike Barman, pgBackRest requires object storage to be accessible over HTTPS. While
> it's possible to disable key verification and use self-signed keys, using HTTP
> endpoint is not possible.

### Backup objects cleanup

pgBackRest expires backups from the repository on its own, but it does not know about
the CNPG `Backup` objects that point to them. The sidecar running on the primary periodically
deletes the `completed` `Backup` objects of the cluster whose backup ID is not in the
pgBackRest catalog anymore. The period is set by
`instanceSidecarConfiguration.catalogMaintenanceIntervalSeconds` (default `1800`; `0`
disables the cleanup, otherwise at least `60`).

The plugin only performs the cleanup when the configuration is considered safe: the stanza
matches and all repositories are reported as readable.

> [!IMPORTANT]
> Pointing an `Archive` at another location, removing one of its repositories or renaming the
> stanza means the previously recorded objects can no longer be matched; adding or reordering
> repositories does not.
> Such `Backup` resources, as well as the ones created by plugin versions before this
> feature, must be removed manually.

> [!NOTE]
> Only `Backup` objects labeled `cnpg.io/cluster=<cluster>` are considered. Both
> `ScheduledBackup` and `kubectl cnpg backup` set the label; a hand-applied `Backup`
> manifest without it is never cleaned up. Failed backups are left untouched.

### Configuring WAL Archiving

Once the `Archive` is defined, you can configure a PostgreSQL cluster to archive WALs by
referencing the configuration in the `.spec.plugins` section, as shown below:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  imagePullPolicy: Always
  plugins:
  - name: pgbackrest.cnpg.opera.com
    parameters:
      pgbackrestObjectName: minio-store
  storage:
    size: 1Gi
```

This configuration enables both WAL archiving and data directory backups.

> [!IMPORTANT]
> Archiving will only start working after at least one backup is created. That's due to
> the stanza creation process which currently is only executed on backups.

### Performing a Base Backup

Once WAL archiving is enabled, the cluster is ready for backups. To create a
backup, configure the `backup.spec.pluginConfiguration` section to specify this
plugin:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: backup-example
spec:
  method: plugin
  cluster:
    name: cluster-example
  target: primary
  pluginConfiguration:
    name: pgbackrest.cnpg.opera.com
    parameters:
      type: full
```

> [!IMPORTANT]
> Currently only backups from primary are supported. Trying to run backup from a replica
> will fail.

> [!TIP]
> All keys defined in the `parameters` section are passed to the pgBackRest as flags.
> This feature makes it possible to configure some additional options, most notably
> backup type, for a single backup instead of globally.

### Restoring a Cluster

To restore a cluster from an archive, create a new `Cluster` resource that
references the archive containing the backup. Below is an example configuration:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  instances: 3
  imagePullPolicy: IfNotPresent
  bootstrap:
    recovery:
      source: source
  externalClusters:
  - name: source
    plugin:
      name: pgbackrest.cnpg.opera.com
      parameters:
        pgbackrestObjectName: minio-store
        stanza: cluster-example
  storage:
    size: 1Gi
```

> [!NOTE]
> The above configuration does **not** enable WAL archiving for the restored cluster.
>
> To enable WAL archiving for the restored cluster, include the `.spec.plugins`
> section alongside the `externalClusters.plugin` section, as shown below:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  instances: 3
  imagePullPolicy: IfNotPresent
  bootstrap:
    recovery:
      source: source
  plugins:
  - name: pgbackrest.cnpg.opera.com
    parameters:
      # Backup archive (push, read-write)
      pgbackrestObjectName: minio-store-bis
  externalClusters:
  - name: source
    plugin:
      name: pgbackrest.cnpg.opera.com
      parameters:
        # Recovery archive (pull, read-only)
        pgbackrestObjectName: minio-store
        stanza: cluster-example
  storage:
    size: 1Gi
```

The same archive may be used for both transaction log archiving and
restoring a cluster, or you can configure separate stores for these purposes.

### Tuning WAL Restore

While restoring, the plugin downloads WAL files ahead of the one PostgreSQL
requests and keeps them in the spool directory until PostgreSQL asks for them.
`wal.maxParallel` sets how many downloads run at once, and the plugin keeps
`maxParallel - 1` files downloading ahead of the requested one. When a few
downloads take much longer than the rest, for example over an unreliable link
to the object store, such a short window stops at every slow file.
`wal.extraPrefetch` makes the window longer, so that the other downloads go on
past the slow one:

```yaml
spec:
  configuration:
    wal:
      maxParallel: 32
      extraPrefetch: 96
```

In our tests, with 28 percent of the downloads taking about 80 seconds, a window
of about four times `maxParallel`, that is `extraPrefetch` of about three times
`maxParallel`, restored about three times faster than the default. The files
wait in the spool directory on the pod's ephemeral storage, up to
`maxParallel - 1 + extraPrefetch` WAL segments, so leave room for them if you
set `ephemeralVolumesSizeLimit` on the cluster.

### Configuring Replica Clusters

You can set up a distributed topology by combining the previously defined
configurations with the `.spec.replica` section. Below is an example of how to
define a replica cluster:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-dc-a
spec:
  instances: 3
  primaryUpdateStrategy: unsupervised

  storage:
    storageClass: csi-hostpath-sc
    size: 1Gi

  plugins:
  - name: pgbackrest.cnpg.opera.com
    parameters:
      pgbackrestObjectName: minio-store-a

  replica:
    self: cluster-dc-a
    primary: cluster-dc-a
    source: cluster-dc-b

  externalClusters:
  - name: cluster-dc-a
    plugin:
      name: pgbackrest.cnpg.opera.com
      parameters:
        pgbackrestObjectName: minio-store-a

  - name: cluster-dc-b
    plugin:
      name: pgbackrest.cnpg.opera.com
      parameters:
        pgbackrestObjectName: minio-store-b
```
