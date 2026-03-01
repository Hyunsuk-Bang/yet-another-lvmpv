package node

import (
	"context"
	"fmt"
	"os/exec"

	lvmpvv1 "github.com/Hyunsuk-Bang/yet-another-lvmpv/api/v1"
	"github.com/rs/zerolog/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const finalizer = "lvmpv.yetanother.io/node-cleanup"

type Reconciler struct {
	client.Client
	NodeName string
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lv := &lvmpvv1.LogicalVolume{}
	log.Debug().Msgf("Reconciling LogicalVolume %s", req.NamespacedName)

	if err := r.Get(ctx, req.NamespacedName, lv); err != nil {
		log.Error().Err(err).Msgf("LogicalVolume %s not found", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// This node only handles its own volumes
	if lv.Spec.NodeName != r.NodeName {
		log.Debug().Msgf("LogicalVolume %s is assigned to node %s, skipping", req.NamespacedName, lv.Spec.NodeName)
		return ctrl.Result{}, nil
	}

	if !lv.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, lv)
	}

	// Ensure our finalizer is present so we can clean up on deletion
	if !controllerutil.ContainsFinalizer(lv, finalizer) {
		controllerutil.AddFinalizer(lv, finalizer)
		if err := r.Update(ctx, lv); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Already provisioned — nothing to do
	if lv.Status.Phase == lvmpvv1.LogicalVolumeReady {
		return ctrl.Result{}, nil
	}

	return r.reconcileCreate(ctx, lv)
}

func (r *Reconciler) reconcileCreate(ctx context.Context, lv *lvmpvv1.LogicalVolume) (ctrl.Result, error) {
	cmd := exec.CommandContext(ctx,
		"lvcreate",
		"--size", lv.Spec.Size,
		"--name", lv.Name,
		lv.Spec.VGName,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return r.setFailed(ctx, lv, fmt.Sprintf("lvcreate failed: %s: %v", string(out), err))
	}
	lv.Status.Phase = lvmpvv1.LogicalVolumeReady
	lv.Status.Message = ""
	if err := r.Status().Update(ctx, lv); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) reconcileDelete(ctx context.Context, lv *lvmpvv1.LogicalVolume) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(lv, finalizer) {
		return ctrl.Result{}, nil
	}

	cmd := exec.CommandContext(ctx,
		"lvremove", "--yes", fmt.Sprintf("%s/%s", lv.Spec.VGName, lv.Name),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return r.setFailed(ctx, lv, fmt.Sprintf("lvremove failed: %s: %v", string(out), err))
	}

	controllerutil.RemoveFinalizer(lv, finalizer)
	return ctrl.Result{}, r.Update(ctx, lv)
}

func (r *Reconciler) setFailed(ctx context.Context, lv *lvmpvv1.LogicalVolume, msg string) (ctrl.Result, error) {
	lv.Status.Phase = lvmpvv1.LogicalVolumeFailed
	lv.Status.Message = msg
	return ctrl.Result{}, r.Status().Update(ctx, lv)
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lvmpvv1.LogicalVolume{}).
		Complete(r)

	/*
		ctrl.NewControllerManagedBy(mgr). <- Register controller with the Mansager
		Manager holds shared dependencies like the client and starts the controller's event loop.
		Run informer cache
		Start all registered controllers
	*/
}
