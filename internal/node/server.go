package node

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DriverName    = "lvmpv.yetanother.io"
	DriverVersion = "v0.1.0"
)

type Server struct {
	csi.UnimplementedNodeServer
	csi.UnimplementedIdentityServer
	nodeID string
}

func NewServer(nodeID string) *Server {
	return &Server{nodeID: nodeID}
}

// --- Identity ---

func (s *Server) GetPluginInfo(_ context.Context, _ *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: DriverVersion,
	}, nil
}

func (s *Server) GetPluginCapabilities(_ context.Context, _ *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{}, nil
}

func (s *Server) Probe(_ context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

// --- Node ---

func (s *Server) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: s.nodeID,
		// Topology tells Kubernetes: "volumes on this node are only reachable here"
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				"lvmpv.yetanother.io/node": s.nodeID,
			},
		},
	}, nil
}

func (s *Server) NodeGetCapabilities(_ context.Context, _ *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

// NodeStageVolume formats (if needed) and mounts the LV to a node-global staging path.
// Called once per volume per node, before NodePublishVolume.
func (s *Server) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	log.Info().Str("volumeID", req.VolumeId).Str("stagingPath", req.StagingTargetPath).Msg("NodeStageVolume")

	devicePath, ok := req.VolumeContext["devicePath"]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "devicePath missing from VolumeContext")
	}

	if err := os.MkdirAll(req.StagingTargetPath, 0750); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create staging dir: %v", err)
	}

	// Format only if the device has no filesystem yet
	out, _ := exec.CommandContext(ctx, "blkid", "-s", "TYPE", "-o", "value", devicePath).Output()
	if strings.TrimSpace(string(out)) == "" {
		log.Info().Str("device", devicePath).Msg("no filesystem found, formatting with ext4")
		if out, err := exec.CommandContext(ctx, "mkfs.ext4", "-F", devicePath).CombinedOutput(); err != nil {
			return nil, status.Errorf(codes.Internal, "mkfs.ext4 failed: %s: %v", string(out), err)
		}
	}

	if out, err := exec.CommandContext(ctx, "mount", devicePath, req.StagingTargetPath).CombinedOutput(); err != nil {
		return nil, status.Errorf(codes.Internal, "mount failed: %s: %v", string(out), err)
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume unmounts the LV from the staging path.
func (s *Server) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	log.Info().Str("volumeID", req.VolumeId).Str("stagingPath", req.StagingTargetPath).Msg("NodeUnstageVolume")

	if out, err := exec.CommandContext(ctx, "umount", req.StagingTargetPath).CombinedOutput(); err != nil {
		return nil, status.Errorf(codes.Internal, "umount failed: %s: %v", string(out), err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume bind-mounts from the staging path into the pod's volume path.
// Called once per pod that uses the volume.
func (s *Server) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Info().Str("volumeID", req.VolumeId).Str("targetPath", req.TargetPath).Msg("NodePublishVolume")

	if err := os.MkdirAll(req.TargetPath, 0750); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create target dir: %v", err)
	}

	if out, err := exec.CommandContext(ctx,
		"mount", "--bind", req.StagingTargetPath, req.TargetPath,
	).CombinedOutput(); err != nil {
		return nil, status.Errorf(codes.Internal, "bind mount failed: %s: %v", string(out), err)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume unmounts the pod's volume path.
func (s *Server) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Info().Str("volumeID", req.VolumeId).Str("targetPath", req.TargetPath).Msg("NodeUnpublishVolume")

	if out, err := exec.CommandContext(ctx, "umount", req.TargetPath).CombinedOutput(); err != nil {
		return nil, status.Errorf(codes.Internal, "umount failed: %s: %v", string(out), err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// nodeVolumeID returns the LV name used for a given CSI volume ID.
func nodeVolumeID(volumeID, vgName string) string {
	return fmt.Sprintf("%s/%s", vgName, volumeID)
}
