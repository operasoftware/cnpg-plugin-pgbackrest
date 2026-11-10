package main

import (
	"context"
	"fmt"

	"dagger/e-2-e/internal/dagger"
)

type E2E struct{}

// Run runs the E2E tests on a Kubernetes cluster. It returns the output of the tests.
// We expect a kubeconfig file that allows access to the cluster, and optionally
// a service to bind to, if the cluster is not directly exposed to the dagger container running the tests.
func (m *E2E) Run(
	ctx context.Context,
	// source is the directory containing the source code for the project
	source *dagger.Directory,
	// kubeconfig is the kubeconfig file to use for the tests
	kubeconfig *dagger.File,
	// svc is the Kubernetes service to bind to. It will be known as "kubernetes" in the container.
	// +optional
	svc *dagger.Service,
	// version of the golang image to use
	// +optional
	// +default="latest"
	goVersion string,
) (string, error) {
	goDag := dag.Go(dagger.GoOpts{Version: goVersion}).WithCgoDisabled().WithSource(source)
	if svc != nil {
		goDag = goDag.WithServiceBinding("kubernetes", svc)
	}
	return goDag.Container().
		WithMountedFile("/kubeconfig", kubeconfig).
		WithEnvVariable("KUBECONFIG", "/kubeconfig").
		WithExec([]string{"go", "run", "github.com/onsi/ginkgo/v2/ginkgo",
			"--procs=4",
			"--randomize-all",
			"--randomize-suites",
			"--fail-on-pending",
			"--fail-on-empty",
			"--keep-going",
			"--timeout=45m",
			"--github-output",
			"./test/e2e"}).Stdout(ctx)
}

// RunEphemeral creates a k3s cluster in dagger and then runs the E2E tests on it.
// If a private registry is used, its url and the ca certificate for the registry should be provided.
func (m *E2E) RunEphemeral(
	ctx context.Context,
	// source is the directory containing the source code for the project
	source *dagger.Directory,
	// registry is a private registry
	// +optional
	// +default="registry.pgbackrest-plugin:5000"
	registry string,
	// ca is the certificate authority for the registry
	// +optional
	ca *dagger.File,
	// name is the name of the ephemeral container
	// +optional
	// +default="e2e"
	name string,
	// version of the k3s image to use
	// +optional
	// +default="latest"
	kubernetesVersion string,
	// version of the golang image to use
	// +optional
	// +default="latest"
	goVersion string,
) (string, error) {
	k3sImage := fmt.Sprintf("rancher/k3s:%s", kubernetesVersion)
	k3s := dag.K3S(name, dagger.K3SOpts{
		Image: k3sImage,
	})
	ctr := k3s.Container()
	if ca != nil {
		ctr = ctr.WithMountedFile("/usr/local/share/ca-certificates/ca.crt", ca)
	}
	if registry != "" {
		ctr = ctr.WithNewFile("/registries.yaml", fmt.Sprintf(`
configs:
  "%s":
    tls:
      ca_file: "/usr/local/share/ca-certificates/ca.crt"
`, registry)).
			WithExec([]string{"sh", "-c", "cat /registries.yaml > /etc/rancher/k3s/registries.yaml"})
	}

	ctr, err := ctr.Sync(ctx)
	if err != nil {
		return "", err
	}
	kServer := k3s.WithContainer(ctr).Server()

	kServer, err = kServer.Start(ctx)
	if err != nil {
		return "", err
	}
	defer kServer.Stop(ctx)

	return m.Run(ctx, source, k3s.Config(), kServer, goVersion)
}

// WalArchiveMemory checks the pgBackRest package used in the sidecar for excessive memory retention.
func (m *E2E) WalArchiveMemory(ctx context.Context, source *dagger.Directory) (string, error) {
	runner := source.DockerBuild(dagger.DirectoryDockerBuildOpts{
		Dockerfile: "containers/Dockerfile.sidecar",
		Target:     "pgbackrestinstaller",
	}).
		WithEnvVariable("DEBIAN_FRONTEND", "noninteractive").
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "--no-install-recommends", "curl", "openssl", "postgresql-18", "python3"}).
		WithExec([]string{"openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
			"-subj", "/CN=minio", "-keyout", "/tmp/minio.key", "-out", "/tmp/minio.crt"})

	// Build a pinned MinIO release so the test does not depend on registry access
	// to a third-party MinIO image.
	minioBinary := dag.Container().From("golang:1.27.3").
		WithEnvVariable("CGO_ENABLED", "0").
		WithExec([]string{"go", "install", "github.com/minio/minio@v0.0.0-20251015172955-9e49d5e7a648"}).
		File("/go/bin/minio")
	minio := dag.Container().From("debian:13-slim").
		WithMountedFile("/usr/local/bin/minio", minioBinary).
		WithMountedFile("/certs/private.key", runner.File("/tmp/minio.key")).
		WithMountedFile("/certs/public.crt", runner.File("/tmp/minio.crt")).
		WithEnvVariable("MINIO_ROOT_USER", "wal-memory").
		WithEnvVariable("MINIO_ROOT_PASSWORD", "wal-memory-test").
		WithEnvVariable("MINIO_BROWSER", "off").
		WithEnvVariable("MINIO_UPDATE", "off").
		WithExposedPort(9000).
		WithDefaultArgs([]string{"/usr/local/bin/minio", "server", "--certs-dir", "/certs", "/tmp/wal-memory"}).
		AsService()

	return runner.WithUser("postgres").WithServiceBinding("minio", minio).
		WithMountedFile("/wal-memory.py", source.File("test/wal-memory/archive.py")).
		WithExec([]string{"python3", "/wal-memory.py"}).
		Stdout(ctx)
}
