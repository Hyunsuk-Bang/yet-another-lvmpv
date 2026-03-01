package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type LogicalVolumePhase string

const (
	LogicalVolumePending LogicalVolumePhase = "Pending"
	LogicalVolumeReady   LogicalVolumePhase = "Ready"
	LogicalVolumeFailed  LogicalVolumePhase = "Failed"
)

type LogicalVolumeSpec struct {
	// NodeName is the node where the LV will be created
	NodeName string `json:"nodeName"`
	// VGName is the LVM Volume Group to carve the LV from
	VGName string `json:"vgName"`
	// Size is the requested size (e.g. "10Gi")
	Size string `json:"size"`
}

type LogicalVolumeStatus struct {
	// DevicePath is set by the node plugin after lvcreate succeeds (e.g. "/dev/vg0/pvc-xxx")
	// +optional
	DevicePath string `json:"devicePath,omitempty"`
	// Phase is the current lifecycle phase of the volume
	// +optional
	Phase LogicalVolumePhase `json:"phase,omitempty"`
	// Message provides human-readable details about the current state
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeName`
// +kubebuilder:printcolumn:name="VG",type=string,JSONPath=`.spec.vgName`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.spec.size`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Device",type=string,JSONPath=`.status.devicePath`

type LogicalVolume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogicalVolumeSpec   `json:"spec,omitempty"`
	Status LogicalVolumeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type LogicalVolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogicalVolume `json:"items"`
}
