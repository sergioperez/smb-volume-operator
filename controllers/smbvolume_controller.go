/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	//kresource "k8s.io/apimachinery/pkg/api/resource"

	storagev1alpha1 "github.com/sergioperez/smb-volume-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SMBVolumeReconciler reconciles a SMBVolume object
type SMBVolumeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=storage.sergio.link,resources=smbvolumes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=storage.sergio.link,resources=smbvolumes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=storage.sergio.link,resources=smbvolumes/finalizers,verbs=update

//+kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SMBVolume object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *SMBVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	rLog := log.FromContext(ctx)

	//name := smbVolume.Name
	//namespace := smbVolume.Namespace
	name := req.NamespacedName.Name
	namespace := req.NamespacedName.Namespace

	// Fetch SMBVolume instance
	smbVolume := &storagev1alpha1.SMBVolume{}
	err := r.Get(ctx, req.NamespacedName, smbVolume)
	if err != nil {
		if errors.IsNotFound(err) {
			rLog.Info("SMBVolume { " + name + "} resource not found. Deleting PV " + getPVName(name, namespace))
			// Delete PV ( PVC will be deleted by ownerReference)
			r.DeletePV(ctx, req)
			return ctrl.Result{}, nil
		}
		rLog.Error(err, "Failed to get SMBVolume")
		return ctrl.Result{}, err
	}

	// Create PVC if it does not exist
	pvc := &v1.PersistentVolumeClaim{}
	err = r.Get(ctx, types.NamespacedName{Name: getPVCName(name), Namespace: namespace}, pvc)
	if err != nil && errors.IsNotFound(err) {
		pvc = r.GeneratePVC(smbVolume)
		err = r.Create(ctx, pvc)
		if err != nil {
			rLog.Error(err, "Failed to create PVC. Namespace:{"+namespace+"} Name:{"+getPVCName(name)+"}")
			return ctrl.Result{}, err
		} else {
			rLog.Info("Created PVC {" + getPVCName(name) + "} in namespace {" + namespace + "}")
		}
	} else if err != nil {
		rLog.Error(err, "Failed to get PVC "+getPVCName(name))
		return ctrl.Result{}, err
	}

	// Create PV if it does not exist
	pv := &v1.PersistentVolume{}
	err = r.Get(ctx, types.NamespacedName{Name: getPVName(name, namespace)}, pv)
	if err != nil && errors.IsNotFound(err) {
		// Prepare PV metadata

		pv = r.GeneratePV(smbVolume)
		err = r.Create(ctx, pv)
		if err != nil {
			rLog.Error(err, "Failed to create PV. Name:{"+getPVName(name, namespace)+"}")
			return ctrl.Result{}, err
		} else {
			rLog.Info("Created PV {" + getPVName(name, namespace) + "}")
		}
	} else if err != nil {
		rLog.Error(err, "Failed to get PV "+getPVName(name, namespace))
		return ctrl.Result{}, err
	}

	// PV and PVC created successfully.
	//return ctrl.Result{Requeue: true}, nil
	return ctrl.Result{Requeue: false}, nil
}

type PVData struct {
	Name, PVCNamespace, Path, SecretName string
	ReadOnly                             bool
}

func (r *SMBVolumeReconciler) DeletePV(ctx context.Context, req ctrl.Request) error {
	pv := &v1.PersistentVolume{}
	name := req.NamespacedName.Name
	namespace := req.NamespacedName.Namespace
	err := r.Get(ctx, types.NamespacedName{Name: getPVName(name, namespace)}, pv)
	if err != nil {
		return err
	}
	err = r.Delete(ctx, pv)
	return err
}

func (r *SMBVolumeReconciler) GeneratePVC(smbVolume *storagev1alpha1.SMBVolume) *v1.PersistentVolumeClaim {
	pvc := &v1.PersistentVolumeClaim{}
	// PVC metadata
	pvc.Name = getPVCName(smbVolume.Name)
	pvc.Namespace = smbVolume.Namespace

	// PVC spec
	emptyString := ""
	mode := v1.PersistentVolumeFilesystem
	pvc.Spec = v1.PersistentVolumeClaimSpec{
		Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): resource.MustParse("1Mi"),
			},
		},
		AccessModes: []v1.PersistentVolumeAccessMode{
			v1.ReadWriteMany,
		},
		StorageClassName: &emptyString,
		VolumeMode:       &mode,
		VolumeName:       getPVName(pvc.Name, pvc.Namespace),
	}
	ctrl.SetControllerReference(smbVolume, pvc, r.Scheme)
	return pvc
}

func (r *SMBVolumeReconciler) GeneratePV(smbVolume *storagev1alpha1.SMBVolume) *v1.PersistentVolume {
	pv := &v1.PersistentVolume{}
	// PVC metadata
	pv.Name = getPVName(smbVolume.Name, smbVolume.Namespace)
	pv.Annotations = map[string]string{"smbVolume/pvc": getPVCName(smbVolume.Name), "smbVolume/namespace": smbVolume.Namespace}

	// PVC spec
	mode := v1.PersistentVolumeFilesystem
	pv.Spec = v1.PersistentVolumeSpec{
		AccessModes: getVolumeAccessMode(smbVolume.Spec.ReadOnly),
		Capacity: v1.ResourceList{
			v1.ResourceName(v1.ResourceStorage): resource.MustParse("1Mi"),
		},
		PersistentVolumeSource: v1.PersistentVolumeSource{
			CSI: &v1.CSIPersistentVolumeSource{
				Driver:           "smb.csi.k8s.io",
				VolumeAttributes: map[string]string{"source": smbVolume.Spec.Path},
				VolumeHandle:     getPVName(smbVolume.Name, smbVolume.Namespace),
				NodeStageSecretRef: &v1.SecretReference{
					Name:      smbVolume.Spec.SecretName,
					Namespace: smbVolume.Namespace,
				},
			},
		},
		MountOptions:                  getMountOptions(smbVolume.Spec.ReadOnly),
		PersistentVolumeReclaimPolicy: v1.PersistentVolumeReclaimRetain,
		VolumeMode:                    &mode,
		ClaimRef: &v1.ObjectReference{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
			Name:       getPVCName(smbVolume.Name),
			Namespace:  smbVolume.Namespace,
		},
	}
	//err := ctrl.SetControllerReference(smbVolume, pv, r.Scheme)
	return pv
}

func getVolumeAccessMode(isReadOnly bool) []v1.PersistentVolumeAccessMode {
	if isReadOnly == true {
		return []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany}
	} else {
		return []v1.PersistentVolumeAccessMode{v1.ReadWriteMany}
	}
}

func getMountOptions(isReadOnly bool) []string {
	if isReadOnly == true {
		return []string{"dir_mode=0555", "file_mode=0444", "vers=3.0", "ro"}
	} else {
		return []string{"dir_mode=0775", "file_mode=0664", "vers=3.0", "rw"}
	}
}

func getPVCName(name string) string {
	return "smb-" + name
}

func getPVName(name string, pvcNamespace string) string {
	h := sha1.New()
	h.Write([]byte(pvcNamespace))
	hashedNamespace := hex.EncodeToString(h.Sum(nil))

	return "smb-" + name + "-" + hashedNamespace[0:6]
}

// SetupWithManager sets up the controller with the Manager.
func (r *SMBVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.SMBVolume{}).
		Owns(&v1.PersistentVolumeClaim{}).
		Complete(r)
}
