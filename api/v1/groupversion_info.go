// Package v1 contains API Schema definitions for the lvmpv.yetanother.io v1 API group.
// +kubebuilder:object:generate=true
// +groupName=lvmpv.yetanother.io
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "lvmpv.yetanother.io", Version: "v1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&LogicalVolume{}, &LogicalVolumeList{})
}
