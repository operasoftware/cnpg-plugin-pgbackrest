/*
Copyright The CloudNativePG Contributors
Copyright 2025, Opera Norway AS

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package instance

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
	pgbackrestCredentials "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/credentials"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

const (
	firstRecoverabilityPointMetric     = "cnpg_pgbackrest_first_recoverability_point"
	lastAvailableBackupTimestampMetric = "cnpg_pgbackrest_last_available_backup_timestamp"
)

// MetricsServiceImplementation is the implementation of the Metrics Service
type MetricsServiceImplementation struct {
	metrics.UnimplementedMetricsServer
	Client client.Client
}

// GetCapabilities implements the MetricsServer interface
func (m MetricsServiceImplementation) GetCapabilities(
	_ context.Context,
	_ *metrics.MetricsCapabilitiesRequest,
) (*metrics.MetricsCapabilitiesResult, error) {
	return &metrics.MetricsCapabilitiesResult{
		Capabilities: []*metrics.MetricsCapability{
			{
				Type: &metrics.MetricsCapability_Rpc{
					Rpc: &metrics.MetricsCapability_RPC{
						Type: metrics.MetricsCapability_RPC_TYPE_METRICS,
					},
				},
			},
		},
	}, nil
}

// Define implements the MetricsServer interface
func (m MetricsServiceImplementation) Define(
	_ context.Context,
	_ *metrics.DefineMetricsRequest,
) (*metrics.DefineMetricsResult, error) {
	return &metrics.DefineMetricsResult{
		Metrics: []*metrics.Metric{
			{
				FqName: firstRecoverabilityPointMetric,
				Help:   "The first point of recoverability for pgBackRest as a unix timestamp",
				ValueType: &metrics.MetricType{
					Type: metrics.MetricType_TYPE_GAUGE,
				},
			},
			{
				FqName: lastAvailableBackupTimestampMetric,
				Help:   "The last available backup timestamp for pgBackRest as a unix timestamp",
				ValueType: &metrics.MetricType{
					Type: metrics.MetricType_TYPE_GAUGE,
				},
			},
		},
	}, nil
}

// Collect implements the MetricsServer interface
func (m MetricsServiceImplementation) Collect(
	ctx context.Context,
	request *metrics.CollectMetricsRequest,
) (*metrics.CollectMetricsResult, error) {
	contextLogger := log.FromContext(ctx)

	configuration, err := config.NewFromClusterJSON(request.ClusterDefinition)
	if err != nil {
		return nil, fmt.Errorf("while parsing cluster definition: %w", err)
	}

	if configuration.PgbackrestObjectName == "" {
		contextLogger.Debug("No pgbackrest archive configured, skipping metrics collection")
		return &metrics.CollectMetricsResult{}, nil
	}

	var archive pgbackrestv1.Archive
	if err := m.Client.Get(ctx, configuration.GetArchiveObjectKey(), &archive); err != nil {
		return nil, fmt.Errorf("while getting archive object: %w", err)
	}

	env, err := pgbackrestCredentials.EnvSetBackupCloudCredentials(
		ctx,
		m.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		utils.SanitizedEnviron())
	if err != nil {
		return nil, fmt.Errorf("while getting credentials: %w", err)
	}

	backupCatalog, err := pgbackrestCommand.GetBackupList(ctx, &archive.Spec.Configuration, configuration.Stanza, env)
	if err != nil {
		contextLogger.Error(err, "while getting backup list for metrics")
		return &metrics.CollectMetricsResult{
			Metrics: []*metrics.CollectMetric{
				{FqName: firstRecoverabilityPointMetric, Value: 0},
				{FqName: lastAvailableBackupTimestampMetric, Value: 0},
			},
		}, nil
	}

	result := &metrics.CollectMetricsResult{}

	firstRecoverability, lastBackup, err := getRecoveryWindow(backupCatalog)
	if err != nil {
		contextLogger.Debug("No backup data available for metrics", "error", err)
		result.Metrics = append(result.Metrics,
			&metrics.CollectMetric{FqName: firstRecoverabilityPointMetric, Value: 0},
			&metrics.CollectMetric{FqName: lastAvailableBackupTimestampMetric, Value: 0},
		)
	} else {
		result.Metrics = append(result.Metrics,
			&metrics.CollectMetric{FqName: firstRecoverabilityPointMetric, Value: float64(firstRecoverability)},
			&metrics.CollectMetric{FqName: lastAvailableBackupTimestampMetric, Value: float64(lastBackup)},
		)
	}

	return result, nil
}

// getRecoveryWindow extracts first recoverability point and last backup timestamp
// from the backup catalog. Returns unix timestamps.
func getRecoveryWindow(backupCatalog *catalog.Catalog) (firstRecoverability, lastBackup int64, err error) {
	if backupCatalog == nil || len(backupCatalog.Backups) == 0 {
		return 0, 0, errors.New("no backups found")
	}

	firstRecoverability = math.MaxInt64
	lastBackup = 0

	for i := range backupCatalog.Backups {
		backup := &backupCatalog.Backups[i]
		if backup.Time.Start > 0 && backup.Time.Start < firstRecoverability {
			firstRecoverability = backup.Time.Start
		}
		if backup.Time.Stop > lastBackup {
			lastBackup = backup.Time.Stop
		}
	}

	if firstRecoverability == math.MaxInt64 {
		firstRecoverability = 0
	}

	return firstRecoverability, lastBackup, nil
}
