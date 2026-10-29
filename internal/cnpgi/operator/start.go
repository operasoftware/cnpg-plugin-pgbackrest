package operator

import (
	"context"

	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/http"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// CNPGI is the implementation of the CNPG-i server
type CNPGI struct {
	Client         client.Client
	PluginPath     string
	ServerCertPath string
	ServerKeyPath  string
	ClientCertPath string
	ServerAddress  string
}

// controller-runtime starts a runnable passed to mgr.Add only on the leader,
// unless it implements LeaderElectionRunnable and returns false, in which case
// it starts on every replica once the caches have synced.
//
// The gRPC server runs on every replica, so the Service balances across pods
// and the non-leaders pass the readiness probe. The handlers read through the
// cache, and the only writes, the Role and RoleBinding of the reconciler hook,
// are idempotent. The Archive controller keeps running on the leader only.
var _ manager.LeaderElectionRunnable = &CNPGI{}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
func (c *CNPGI) NeedLeaderElection() bool {
	return false
}

// Start starts the GRPC server
// of the operator plugin
func (c *CNPGI) Start(ctx context.Context) error {
	enrich := func(server *grpc.Server) error {
		reconciler.RegisterReconcilerHooksServer(server, ReconcilerImplementation{
			Client: c.Client,
		})
		lifecycle.RegisterOperatorLifecycleServer(server, LifecycleImplementation{
			Client: c.Client,
		})
		return nil
	}

	srv := http.Server{
		IdentityImpl:   IdentityImplementation{},
		Enrichers:      []http.ServerEnricher{enrich},
		PluginPath:     c.PluginPath,
		ServerCertPath: c.ServerCertPath,
		ServerKeyPath:  c.ServerKeyPath,
		ClientCertPath: c.ClientCertPath,
		ServerAddress:  c.ServerAddress,
	}

	return srv.Start(ctx)
}
