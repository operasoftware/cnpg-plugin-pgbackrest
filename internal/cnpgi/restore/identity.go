package restore

import (
	"context"

	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
)

// IdentityImplementation implements IdentityServer
type IdentityImplementation struct {
	identity.UnimplementedIdentityServer
	// TODO: Should this field be removed?
	// nolint: unused
	archiveObjectKey client.ObjectKey
	Client           client.Client
}

// GetPluginMetadata implements IdentityServer
func (i IdentityImplementation) GetPluginMetadata(
	_ context.Context,
	_ *identity.GetPluginMetadataRequest,
) (*identity.GetPluginMetadataResponse, error) {
	return &metadata.Data, nil
}

// GetPluginCapabilities implements IdentityServer
func (i IdentityImplementation) GetPluginCapabilities(
	_ context.Context,
	_ *identity.GetPluginCapabilitiesRequest,
) (*identity.GetPluginCapabilitiesResponse, error) {
	return &identity.GetPluginCapabilitiesResponse{
		Capabilities: []*identity.PluginCapability{
			{
				Type: &identity.PluginCapability_Service_{
					Service: &identity.PluginCapability_Service{
						Type: identity.PluginCapability_Service_TYPE_RESTORE_JOB,
					},
				},
			},
			{
				Type: &identity.PluginCapability_Service_{
					Service: &identity.PluginCapability_Service{
						Type: identity.PluginCapability_Service_TYPE_WAL_SERVICE,
					},
				},
			},
		},
	}, nil
}

// Probe implements IdentityServer
func (i IdentityImplementation) Probe(
	_ context.Context,
	_ *identity.ProbeRequest,
) (*identity.ProbeResponse, error) {
	return &identity.ProbeResponse{
		Ready: true,
	}, nil
}
