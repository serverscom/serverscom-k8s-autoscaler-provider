// Command serverscom-k8s-autoscaler-provider serves the cluster-autoscaler external gRPC CloudProvider
// service for a single Servers.com Kubernetes cluster.
//
// It runs next to an upstream, unforked cluster-autoscaler started with
// --cloud-provider=externalgrpc. Several instances may run at once: the service holds no state,
// so which one answers makes no difference.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	"google.golang.org/grpc"
	klog "k8s.io/klog/v2"

	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/negcache"
	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/protos"
	"github.com/serverscom/serverscom-k8s-autoscaler-provider/internal/provider"
)

// version is stamped at build time, see the Makefile.
var version = "dev"

const (
	tokenEnvKey     = "SERVERSCOM_TOKEN"
	baseURLEnvKey   = "SERVERSCOM_BASE_URL"
	clusterIDEnvKey = "SERVERSCOM_CLUSTER_ID"
)

var (
	address        = flag.String("address", ":8086", "The address to expose the gRPC service on.")
	metricsAddress = flag.String("metrics-address", ":8087",
		"The address to expose Prometheus metrics on, at /metrics. Empty disables the endpoint.")
	clusterID = flag.String("cluster-id", os.Getenv(clusterIDEnvKey),
		"ID of the Kubernetes cluster to serve. Defaults to $"+clusterIDEnvKey+".")
	maxPods = flag.Int64("max-pods", 110,
		"Pod capacity reported in the node template. The Public API does not expose it.")
	apiTimeout = flag.Duration("api-timeout", 30*time.Second,
		"Timeout of a Public API call, applied only when the incoming gRPC call carries no deadline of its own.")
	showVersion = flag.Bool("version", false, "Print the version and exit.")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if err := run(); err != nil {
		klog.Error(err)
		klog.Flush()
		os.Exit(1)
	}
}

func run() error {
	token := os.Getenv(tokenEnvKey)
	if token == "" {
		return fmt.Errorf("environment variable %q is required", tokenEnvKey)
	}

	if *clusterID == "" {
		return fmt.Errorf("-cluster-id or the environment variable %q is required", clusterIDEnvKey)
	}

	var client *serverscom.Client
	if baseURL := os.Getenv(baseURLEnvKey); baseURL != "" {
		client = serverscom.NewClientWithEndpoint(token, baseURL)
	} else {
		client = serverscom.NewClient(token)
	}

	client.SetupUserAgent(fmt.Sprintf("serverscom-k8s-autoscaler-provider/%s", version))

	cache := negcache.New(prometheus.DefaultRegisterer)

	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(cache.UnaryServerInterceptor))

	protos.RegisterCloudProviderServer(grpcServer,
		provider.New(client.KubernetesClusters, *clusterID, *maxPods, *apiTimeout))

	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", *address, err)
	}

	metricsServer, err := startMetricsServer(*metricsAddress)
	if err != nil {
		listener.Close()
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		klog.Info("shutting down")

		if metricsServer != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := metricsServer.Shutdown(shutdownCtx); err != nil {
				klog.Errorf("metrics server shutdown: %v", err)
			}
		}

		grpcServer.GracefulStop()
	}()

	klog.Infof("serving cluster %s on %s", *clusterID, *address)

	if err := grpcServer.Serve(listener); err != nil {
		return fmt.Errorf("gRPC server failed: %w", err)
	}

	return nil
}

// startMetricsServer serves /metrics on its own listener, apart from the gRPC one. It binds
// synchronously, so a busy port fails the startup instead of going unnoticed. An empty address
// disables the endpoint and returns a nil server.
func startMetricsServer(address string) (*http.Server, error) {
	if address == "" {
		return nil, nil
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("cannot listen on %s for metrics: %w", address, err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			klog.Errorf("metrics server failed: %v", err)
		}
	}()

	klog.Infof("serving metrics on %s/metrics", address)

	return server, nil
}
