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

package config

import (
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/decoder"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
)

// ConfigurationError represents a mistake in the plugin configuration
type ConfigurationError struct {
	messages []string
}

// Error implements the error interface
func (e *ConfigurationError) Error() string {
	return strings.Join(e.messages, ",")
}

// NewConfigurationError creates a new empty configuration error
func NewConfigurationError() *ConfigurationError {
	return &ConfigurationError{}
}

// WithMessage adds a new error message to a potentially empty
// ConfigurationError
func (e *ConfigurationError) WithMessage(msg string) *ConfigurationError {
	if e == nil {
		return &ConfigurationError{
			messages: []string{msg},
		}
	}

	return &ConfigurationError{
		messages: append(e.messages, msg),
	}
}

// IsEmpty returns true if there's no error messages
func (e *ConfigurationError) IsEmpty() bool {
	return len(e.messages) == 0
}

// PluginConfiguration is the configuration of the plugin
type PluginConfiguration struct {
	Cluster *cnpgv1.Cluster

	PgbackrestObjectName string
	Stanza               string

	RecoveryPgbackrestObjectName string
	RecoveryStanza               string

	ReplicaSourcePgbackrestObjectName string
	ReplicaSourceStanza               string
}

// GetArchiveObjectKey gets the namespaced name of the pgbackrest archive object
func (config *PluginConfiguration) GetArchiveObjectKey() types.NamespacedName {
	return types.NamespacedName{
		Namespace: config.Cluster.Namespace,
		Name:      config.PgbackrestObjectName,
	}
}

// GetRecoveryArchiveObjectKey gets the namespaced name of the recovery pgbackrest
// archive object
func (config *PluginConfiguration) GetRecoveryArchiveObjectKey() types.NamespacedName {
	return types.NamespacedName{
		Namespace: config.Cluster.Namespace,
		Name:      config.RecoveryPgbackrestObjectName,
	}
}

// GetReplicaSourceArchiveObjectKey gets the namespaced name of the replica source
// pgbackrest archive object
func (config *PluginConfiguration) GetReplicaSourceArchiveObjectKey() types.NamespacedName {
	return types.NamespacedName{
		Namespace: config.Cluster.Namespace,
		Name:      config.ReplicaSourcePgbackrestObjectName,
	}
}

// GetReferredArchiveObjectsKey gets the list of pgbackrest archive objects referred by
// this plugin configuration
func (config *PluginConfiguration) GetReferredArchiveObjectsKey() []types.NamespacedName {
	result := make([]types.NamespacedName, 0, 3)

	if len(config.PgbackrestObjectName) > 0 {
		result = append(result, config.GetArchiveObjectKey())
	}
	if len(config.RecoveryPgbackrestObjectName) > 0 {
		result = append(result, config.GetRecoveryArchiveObjectKey())
	}
	if len(config.ReplicaSourcePgbackrestObjectName) > 0 {
		result = append(result, config.GetReplicaSourceArchiveObjectKey())
	}

	return result
}

func getClusterGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   cnpgv1.GroupVersion.Group,
		Version: cnpgv1.GroupVersion.Version,
		Kind:    cnpgv1.ClusterKind,
	}
}

// NewFromClusterJSON decodes a JSON representation of a cluster.
func NewFromClusterJSON(clusterJSON []byte) (*PluginConfiguration, error) {
	var result cnpgv1.Cluster

	if err := decoder.DecodeObject(clusterJSON, &result, getClusterGVK()); err != nil {
		return nil, err
	}

	return NewFromCluster(&result), nil
}

// NewFromCluster extracts the configuration from the cluster
func NewFromCluster(cluster *cnpgv1.Cluster) *PluginConfiguration {
	helper := NewPlugin(
		*cluster,
		metadata.PluginName,
	)

	stanza := cluster.Name
	for _, plugin := range cluster.Spec.Plugins {
		if plugin.IsEnabled() && plugin.Name == metadata.PluginName {
			if pluginStanza, ok := plugin.Parameters["stanza"]; ok {
				stanza = pluginStanza
			}
		}
	}

	recoveryStanza := ""
	recoveryPgbackrestObjectName := ""
	if recoveryParameters := getRecoveryParameters(cluster); recoveryParameters != nil {
		recoveryPgbackrestObjectName = recoveryParameters["pgbackrestObjectName"]
		recoveryStanza = recoveryParameters["stanza"]
		if len(recoveryStanza) == 0 {
			recoveryStanza = cluster.Name
		}
	}

	replicaSourceStanza := ""
	replicaSourcePgbackrestObjectName := ""
	if replicaSourceParameters := getReplicaSourceParameters(cluster); replicaSourceParameters != nil {
		replicaSourcePgbackrestObjectName = replicaSourceParameters["pgbackrestObjectName"]
		replicaSourceStanza = replicaSourceParameters["stanza"]
		if len(replicaSourceStanza) == 0 {
			replicaSourceStanza = cluster.Name
		}
	}

	result := &PluginConfiguration{
		Cluster: cluster,
		// used for the backup/archive
		PgbackrestObjectName: helper.Parameters["pgbackrestObjectName"],
		Stanza:               stanza,
		// used for restore and wal_restore during backup recovery
		RecoveryStanza:               recoveryStanza,
		RecoveryPgbackrestObjectName: recoveryPgbackrestObjectName,
		// used for wal_restore in the designed primary of a replica cluster
		ReplicaSourceStanza:               replicaSourceStanza,
		ReplicaSourcePgbackrestObjectName: replicaSourcePgbackrestObjectName,
	}

	return result
}

func getRecoveryParameters(cluster *cnpgv1.Cluster) map[string]string {
	recoveryPluginConfiguration := getRecoverySourcePlugin(cluster)
	if recoveryPluginConfiguration == nil {
		return nil
	}

	if recoveryPluginConfiguration.Name != metadata.PluginName {
		return nil
	}

	return recoveryPluginConfiguration.Parameters
}

func getReplicaSourceParameters(cluster *cnpgv1.Cluster) map[string]string {
	replicaSourcePluginConfiguration := getReplicaSourcePlugin(cluster)
	if replicaSourcePluginConfiguration == nil {
		return nil
	}

	if replicaSourcePluginConfiguration.Name != metadata.PluginName {
		return nil
	}

	return replicaSourcePluginConfiguration.Parameters
}

// getRecoverySourcePlugin returns the configuration of the plugin being
// the recovery source of the cluster. If no such plugin have been configured,
// nil is returned
func getRecoverySourcePlugin(cluster *cnpgv1.Cluster) *cnpgv1.PluginConfiguration {
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		return nil
	}

	recoveryConfig := cluster.Spec.Bootstrap.Recovery
	if len(recoveryConfig.Source) == 0 {
		// Plugin-based recovery is supported only with
		// An external cluster definition
		return nil
	}

	recoveryExternalCluster, found := cluster.ExternalCluster(recoveryConfig.Source)
	if !found {
		// This error should have already been detected
		// by the validating webhook.
		return nil
	}

	return recoveryExternalCluster.PluginConfiguration
}

// getRecoverySourcePlugin returns the configuration of the plugin being
// the recovery source of the cluster. If no such plugin have been configured,
// nil is returned
func getReplicaSourcePlugin(cluster *cnpgv1.Cluster) *cnpgv1.PluginConfiguration {
	if cluster.Spec.ReplicaCluster == nil || len(cluster.Spec.ReplicaCluster.Source) == 0 {
		return nil
	}

	recoveryExternalCluster, found := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)
	if !found {
		// This error should have already been detected
		// by the validating webhook.
		return nil
	}

	return recoveryExternalCluster.PluginConfiguration
}

// Validate checks if the pgbackrestObjectName is set
func (config *PluginConfiguration) Validate() error {
	err := NewConfigurationError()

	if len(config.PgbackrestObjectName) == 0 && len(config.RecoveryPgbackrestObjectName) == 0 {
		return err.WithMessage("no reference to pgbackrestObjectName have been included")
	}

	return nil
}

// Plugin represents a plugin with its associated cluster and parameters.
type Plugin struct {
	Cluster *cnpgv1.Cluster
	// Parameters are the configuration parameters of this plugin
	Parameters  map[string]string
	PluginIndex int
}

// NewPlugin creates a new Plugin instance for the given cluster and plugin name.
func NewPlugin(cluster cnpgv1.Cluster, pluginName string) *Plugin {
	result := &Plugin{Cluster: &cluster}

	result.PluginIndex = -1
	for idx, cfg := range result.Cluster.Spec.Plugins {
		if cfg.Name == pluginName {
			result.PluginIndex = idx
			result.Parameters = cfg.Parameters
		}
	}

	return result
}
