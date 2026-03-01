package v1

const (
	// AnnotationVGFreeBytes is patched onto the k8s Node object by the node plugin.
	// The controller reads this in GetCapacity to report per-node storage availability
	// to the Kubernetes scheduler.
	AnnotationVGFreeBytes = "lvmpv.yetanother.io/vg-free-bytes"
)
