package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	lvmpvv1 "github.com/Hyunsuk-Bang/yet-another-lvmpv/api/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DriverName    = "lvmpv.yetanother.io"
	DriverVersion = "v0.1.0"
)

type Server struct {
	csi.UnimplementedControllerServer
	csi.UnimplementedIdentityServer
	// client is used for CRD operations (LogicalVolume create/delete/get)
	client client.Client
	// reader bypasses the informer cache — used for Node reads in GetCapacity
	// so we always get fresh annotation values without waiting for cache sync
	reader client.Reader
}

func NewServer(c client.Client, r client.Reader) *Server {
	return &Server{client: c, reader: r}
}

// --- Identity ---

func (s *Server) GetPluginInfo(_ context.Context, _ *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: DriverVersion,
	}, nil
}

func (s *Server) GetPluginCapabilities(_ context.Context, _ *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			// Declaring this tells the external-provisioner that our driver
			// understands topology. It will then:
			//   1. Wait for a pod to be scheduled (WaitForFirstConsumer)
			//   2. Pass AccessibilityRequirements.Preferred[0] in CreateVolume
			// Without this, the provisioner skips topology entirely and calls
			// CreateVolume immediately with no node information.
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}

func (s *Server) Probe(_ context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

// --- Controller ---

func (s *Server) ControllerGetCapabilities(_ context.Context, _ *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	caps := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_GET_CAPACITY,
	}
	var csiCaps []*csi.ControllerServiceCapability
	for _, c := range caps {
		csiCaps = append(csiCaps, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{Type: c},
			},
		})
	}
	return &csi.ControllerGetCapabilitiesResponse{Capabilities: csiCaps}, nil
}

func (s *Server) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Info().Str("name", req.Name).Msg("CreateVolume")

	if req.AccessibilityRequirements == nil || len(req.AccessibilityRequirements.Preferred) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"no topology provided — StorageClass must use volumeBindingMode: WaitForFirstConsumer")
	}
	nodeName := req.AccessibilityRequirements.Preferred[0].Segments["lvmpv.yetanother.io/node"]

	vgName, ok := req.Parameters["vgName"]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "missing required parameter: vgName")
	}

	// lvcreate accepts sizes with a unit suffix; 'b' means bytes
	sizeStr := fmt.Sprintf("%db", req.CapacityRange.GetRequiredBytes())

	lv := &lvmpvv1.LogicalVolume{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name},
		Spec: lvmpvv1.LogicalVolumeSpec{
			NodeName: nodeName,
			VGName:   vgName,
			Size:     sizeStr,
		},
	}

	if err := s.client.Create(ctx, lv); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, status.Errorf(codes.Internal, "create LogicalVolume CR: %v", err)
	}

	// Wait for the node reconciler to run lvcreate and set status.phase = Ready
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			fetched := &lvmpvv1.LogicalVolume{}
			if err := s.client.Get(ctx, types.NamespacedName{Name: req.Name}, fetched); err != nil {
				return false, err
			}
			switch fetched.Status.Phase {
			case lvmpvv1.LogicalVolumeReady:
				return true, nil
			case lvmpvv1.LogicalVolumeFailed:
				return false, fmt.Errorf("volume failed: %s", fetched.Status.Message)
			default: // Pending — keep polling
				return false, nil
			}
		},
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "waiting for volume ready: %v", err)
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      req.Name,
			CapacityBytes: req.CapacityRange.GetRequiredBytes(),
			VolumeContext: map[string]string{
				"vgName": vgName,
			},
			AccessibleTopology: []*csi.Topology{
				{Segments: map[string]string{"lvmpv.yetanother.io/node": nodeName}},
			},
		},
	}, nil
}

func (s *Server) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Info().Str("volumeID", req.VolumeId).Msg("DeleteVolume")

	lv := &lvmpvv1.LogicalVolume{}
	if err := s.client.Get(ctx, types.NamespacedName{Name: req.VolumeId}, lv); err != nil {
		if apierrors.IsNotFound(err) {
			return &csi.DeleteVolumeResponse{}, nil // already gone — idempotent
		}
		return nil, status.Errorf(codes.Internal, "get LogicalVolume: %v", err)
	}

	if err := s.client.Delete(ctx, lv); err != nil && !apierrors.IsNotFound(err) {
		return nil, status.Errorf(codes.Internal, "delete LogicalVolume: %v", err)
	}

	// The node reconciler sees DeletionTimestamp and runs lvremove before removing the finalizer
	return &csi.DeleteVolumeResponse{}, nil
}

// GetCapacity reads the free-bytes annotation that the node plugin periodically patches
// onto the k8s Node object, and returns it to the scheduler.
func (s *Server) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	if req.AccessibleTopology == nil {
		return &csi.GetCapacityResponse{AvailableCapacity: 0}, nil
	}

	nodeName := req.AccessibleTopology.Segments["lvmpv.yetanother.io/node"]
	if nodeName == "" {
		return &csi.GetCapacityResponse{AvailableCapacity: 0}, nil
	}

	node := &corev1.Node{}
	if err := s.reader.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return nil, status.Errorf(codes.Internal, "get node %s: %v", nodeName, err)
	}

	freeStr, ok := node.Annotations[lvmpvv1.AnnotationVGFreeBytes]
	if !ok {
		log.Warn().Str("node", nodeName).Msg("node has no VG capacity annotation yet")
		return &csi.GetCapacityResponse{AvailableCapacity: 0}, nil
	}

	freeBytes, err := strconv.ParseInt(freeStr, 10, 64)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "invalid capacity annotation on %s: %v", nodeName, err)
	}

	log.Debug().Str("node", nodeName).Int64("freeBytes", freeBytes).Msg("GetCapacity")
	return &csi.GetCapacityResponse{AvailableCapacity: freeBytes}, nil
}
