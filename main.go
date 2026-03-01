package main

import (
	"flag"
	"net"
	"net/url"
	"os"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	lvmpvv1 "github.com/Hyunsuk-Bang/yet-another-lvmpv/api/v1"
	lvmcontroller "github.com/Hyunsuk-Bang/yet-another-lvmpv/internal/controller"
	"github.com/Hyunsuk-Bang/yet-another-lvmpv/internal/node"
)

var (
	mode        = flag.String("mode", "", "Running mode: controller or node")
	csiEndpoint = flag.String("csi-endpoint", "unix:///csi/csi.sock", "CSI endpoint")
	nodeID      = flag.String("node-id", "", "Node ID (required for node mode)")
	vgName      = flag.String("vg-name", "", "LVM Volume Group name (required for node mode)")
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	flag.Parse()

	scheme := buildScheme()

	switch *mode {
	case "controller":
		runController(scheme)
	case "node":
		if *nodeID == "" {
			log.Fatal().Msg("--node-id is required in node mode")
		}
		if *vgName == "" {
			log.Fatal().Msg("--vg-name is required in node mode")
		}
		runNode(scheme)
	default:
		log.Fatal().Msgf("--mode must be 'controller' or 'node', got: %q", *mode)
	}
}

func runController(scheme *runtime.Scheme) {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create manager")
	}

	// reader bypasses the informer cache — gives GetCapacity always-fresh Node annotations
	srv := lvmcontroller.NewServer(mgr.GetClient(), mgr.GetAPIReader())
	grpcSrv := grpc.NewServer()
	csi.RegisterControllerServer(grpcSrv, srv)
	csi.RegisterIdentityServer(grpcSrv, srv)

	go mustServe(*csiEndpoint, grpcSrv)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal().Err(err).Msg("manager exited with error")
	}
}

func runNode(scheme *runtime.Scheme) {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create manager")
	}

	if err := (&node.Reconciler{
		Client:   mgr.GetClient(),
		NodeName: *nodeID,
	}).SetupWithManager(mgr); err != nil {
		log.Fatal().Err(err).Msg("unable to setup node reconciler")
	}

	// CapacityReporter implements Runnable — the manager starts it after cache sync,
	// so mgr.GetClient() reads are safe (cache is ready by then)
	if err := mgr.Add(&node.CapacityReporter{
		Writer:   mgr.GetClient(),
		Reader:   mgr.GetAPIReader(),
		NodeName: *nodeID,
		VGName:   *vgName,
		Interval: 30 * time.Second,
	}); err != nil {
		log.Fatal().Err(err).Msg("unable to add capacity reporter")
	}

	srv := node.NewServer(*nodeID)
	grpcSrv := grpc.NewServer()
	csi.RegisterNodeServer(grpcSrv, srv)
	csi.RegisterIdentityServer(grpcSrv, srv)

	go mustServe(*csiEndpoint, grpcSrv)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal().Err(err).Msg("manager exited with error")
	}
}

// buildScheme registers both our CRDs and the core k8s types (needed to Get/Patch Node objects)
func buildScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Fatal().Err(err).Msg("failed to add client-go scheme")
	}
	if err := lvmpvv1.AddToScheme(scheme); err != nil {
		log.Fatal().Err(err).Msg("failed to add lvmpv scheme")
	}
	_ = corev1.AddToScheme(scheme) // already included in clientgoscheme, harmless
	return scheme
}

// mustServe parses a unix:// endpoint, removes any stale socket, and starts serving.
func mustServe(endpoint string, srv *grpc.Server) {
	u, err := url.Parse(endpoint)
	if err != nil {
		log.Fatal().Err(err).Msgf("invalid endpoint: %s", endpoint)
	}

	addr := u.Path
	os.Remove(addr) // clean up stale socket from a previous run

	l, err := net.Listen(u.Scheme, addr)
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to listen on %s", endpoint)
	}

	log.Info().Msgf("CSI gRPC server listening on %s", endpoint)
	if err := srv.Serve(l); err != nil {
		log.Fatal().Err(err).Msg("gRPC server error")
	}
}
