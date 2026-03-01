package node

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	lvmpvv1 "github.com/Hyunsuk-Bang/yet-another-lvmpv/api/v1"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CapacityReporter periodically reads VG free space and writes it to the Node
// annotation so the controller's GetCapacity can report accurate per-node capacity
// to the Kubernetes scheduler.
type CapacityReporter struct {
	// Writer is used for Patch (writes go directly to API server, no cache needed)
	Writer client.Client
	// Reader bypasses the informer cache — always returns fresh data from API server.
	// Used for Get so we don't need to wait for cache sync on startup.
	Reader   client.Reader
	NodeName string
	VGName   string
	Interval time.Duration
}

// Start implements controller-runtime's Runnable interface.
// The manager calls Start after the informer cache is synced.
func (r *CapacityReporter) Start(ctx context.Context) error {
	// Report immediately, then on each tick
	r.report(ctx)

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.report(ctx)
		case <-ctx.Done():
			return nil
		}
	}
}

func (r *CapacityReporter) report(ctx context.Context) {
	freeBytes, err := vgFreeBytes(ctx, r.VGName)
	if err != nil {
		log.Error().Err(err).Str("vg", r.VGName).Msg("failed to read VG free space")
		return
	}

	node := &corev1.Node{}
	if err := r.Reader.Get(ctx, types.NamespacedName{Name: r.NodeName}, node); err != nil {
		log.Error().Err(err).Str("node", r.NodeName).Msg("failed to get node for capacity patch")
		return
	}

	patch := client.MergeFrom(node.DeepCopy())
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[lvmpvv1.AnnotationVGFreeBytes] = strconv.FormatInt(freeBytes, 10)

	if err := r.Writer.Patch(ctx, node, patch); err != nil {
		log.Error().Err(err).Str("node", r.NodeName).Msg("failed to patch node capacity annotation")
		return
	}

	log.Debug().Str("node", r.NodeName).Str("vg", r.VGName).Int64("freeBytes", freeBytes).Msg("capacity reported")
}

// vgFreeBytes runs `vgs` and returns the free bytes in the named VG.
func vgFreeBytes(ctx context.Context, vgName string) (int64, error) {
	out, err := exec.CommandContext(ctx,
		"vgs", "--noheadings", "--nosuffix", "--units", "b", "-o", "vg_free", vgName,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("vgs: %w", err)
	}

	freeBytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse vgs output %q: %w", string(out), err)
	}

	return freeBytes, nil
}
