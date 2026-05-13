//nolint:testpackage // white-box tests for unexported monitorDependencies
package cloudmanager

import (
	"context"
	"testing"
	"time"

	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	ccmcharts "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

var testResourceID = labels.NormalizePartOfValue(ccmv1alpha1.AzureKubernetesEngineKind)

var testOperatorGVK = schema.GroupVersionKind{
	Group:   "test.cloudmanager.io",
	Version: "v1",
	Kind:    "TestDependencyOperator",
}

func TestMonitorDependencies(t *testing.T) {
	tests := []struct {
		name            string
		dependencies    ccmcommon.Dependencies
		objects         func(ns string) []client.Object
		expectedStatus  map[string]metav1.ConditionStatus
		expectedReasons map[string]string
	}{
		{
			name: "unmanaged dependency is True with Unmanaged reason",
			dependencies: ccmcommon.Dependencies{
				CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
				GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Unmanaged},
				LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
				SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
			},
			expectedStatus: map[string]metav1.ConditionStatus{
				status.ConditionCertManagerAvailable:  metav1.ConditionTrue,
				status.ConditionGatewayAPIAvailable:   metav1.ConditionTrue,
				status.ConditionLWSAvailable:          metav1.ConditionTrue,
				status.ConditionSailOperatorAvailable: metav1.ConditionTrue,
			},
			expectedReasons: map[string]string{
				status.ConditionCertManagerAvailable:  status.UnmanagedReason,
				status.ConditionGatewayAPIAvailable:   status.UnmanagedReason,
				status.ConditionLWSAvailable:          status.UnmanagedReason,
				status.ConditionSailOperatorAvailable: status.UnmanagedReason,
			},
		},
		{
			name: "managed GatewayAPI without deployments or CR is True",
			dependencies: ccmcommon.Dependencies{
				CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
				GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Managed},
				LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
				SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
			},
			expectedStatus: map[string]metav1.ConditionStatus{
				status.ConditionGatewayAPIAvailable: metav1.ConditionTrue,
			},
		},
		{
			name: "deployment not ready is False",
			dependencies: ccmcommon.Dependencies{
				CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
				GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Unmanaged},
				LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
				SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
			},
			objects: func(_ string) []client.Object {
				return []client.Object{
					newDeployment("cert-manager-operator", ccmcommon.DefaultNamespaceCertManagerOperator, 1, 0),
				}
			},
			expectedStatus: map[string]metav1.ConditionStatus{
				status.ConditionCertManagerAvailable: metav1.ConditionFalse,
			},
			expectedReasons: map[string]string{
				status.ConditionCertManagerAvailable: status.ConditionDeploymentsNotAvailableReason,
			},
		},
		{
			name: "deployment ready is True",
			dependencies: ccmcommon.Dependencies{
				CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
				GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Unmanaged},
				LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
				SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
			},
			objects: func(_ string) []client.Object {
				return []client.Object{
					newDeployment("cert-manager-operator", ccmcommon.DefaultNamespaceCertManagerOperator, 1, 1),
				}
			},
			expectedStatus: map[string]metav1.ConditionStatus{
				status.ConditionCertManagerAvailable: metav1.ConditionTrue,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			var objects []client.Object
			if tt.objects != nil {
				objects = tt.objects(xid.New().String())
			}

			cl, err := fakeclient.New(fakeclient.WithObjects(objects...))
			g.Expect(err).ShouldNot(HaveOccurred())

			instance := &ccmv1alpha1.AzureKubernetesEngine{
				Spec: ccmv1alpha1.AzureKubernetesEngineSpec{
					Dependencies: tt.dependencies,
				},
			}
			rr := &types.ReconciliationRequest{
				Client:   cl,
				Instance: instance,
				Release:  common.Release{Name: cluster.OpenDataHub},
			}
			rr.Conditions = conditions.NewManager(instance, status.ConditionTypeReady, ConditionsTypes...)

			action, err := NewReconcileAction(testResourceID)
			g.Expect(err).ShouldNot(HaveOccurred())

			err = action(ctx, rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			for condType, expected := range tt.expectedStatus {
				cond := rr.Conditions.GetCondition(condType)
				g.Expect(cond).NotTo(BeNil(), "condition %s should exist", condType)
				g.Expect(cond.Status).To(Equal(expected), "condition %s status", condType)
			}

			for condType, expectedReason := range tt.expectedReasons {
				cond := rr.Conditions.GetCondition(condType)
				g.Expect(cond.Reason).To(Equal(expectedReason), "condition %s reason", condType)
			}
		})
	}
}

func TestMonitorDependencies_OperatorCR(t *testing.T) {
	tests := []struct {
		name             string
		condType         string
		condStatus       string
		reason           string
		message          string
		expectedStatus   metav1.ConditionStatus
		expectedReason   string
		expectedMsgMatch string
	}{
		{
			name:             "degraded CR sets condition False",
			condType:         "Degraded",
			condStatus:       "True",
			reason:           "TestFailed",
			message:          "test dependency degraded",
			expectedStatus:   metav1.ConditionFalse,
			expectedReason:   "DependencyDegraded",
			expectedMsgMatch: "Degraded=True",
		},
		{
			name:           "healthy CR sets condition True",
			condType:       "Ready",
			condStatus:     "True",
			reason:         "Ready",
			message:        "all good",
			expectedStatus: metav1.ConditionTrue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			envTest, err := envt.New()
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = envTest.Stop() })

			ctx := context.Background()
			cli := envTest.Client()

			crd, err := envTest.RegisterCRD(ctx, testOperatorGVK, "testdependencyoperators", "testdependencyoperator", apiextensionsv1.NamespaceScoped, envt.WithPermissiveSchema())
			g.Expect(err).NotTo(HaveOccurred())
			envt.CleanupDelete(t, g, ctx, cli, crd)

			nsn := xid.New().String()
			ns := &unstructured.Unstructured{}
			ns.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"})
			ns.SetName(nsn)
			g.Expect(cli.Create(ctx, ns)).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = cli.Delete(ctx, ns) })

			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-operator",
					Namespace: nsn,
					Labels:    map[string]string{labels.InfrastructurePartOf: testResourceID},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
					Template: corev1PodTemplate("test"),
				},
			}
			g.Expect(cli.Create(ctx, dep)).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = cli.Delete(ctx, dep) })

			dep.Status = appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1}
			g.Expect(cli.Status().Update(ctx, dep)).NotTo(HaveOccurred())

			operatorCR := &unstructured.Unstructured{}
			operatorCR.SetGroupVersionKind(testOperatorGVK)
			operatorCR.SetName("default")
			operatorCR.SetNamespace(nsn)
			g.Expect(cli.Create(ctx, operatorCR)).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = cli.Delete(ctx, operatorCR) })

			setCRCondition(g, ctx, cli, operatorCR, tt.condType, tt.condStatus, tt.reason, tt.message)

			conditionType := status.ConditionSailOperatorAvailable
			instance := &ccmv1alpha1.AzureKubernetesEngine{}
			rr := &types.ReconciliationRequest{
				Client:   cli,
				Instance: instance,
				Release:  common.Release{Name: cluster.OpenDataHub},
			}
			rr.Conditions = conditions.NewManager(instance, status.ConditionTypeReady, ConditionsTypes...)

			configs := []ccmcharts.DependencyMonitorConfig{
				{
					ConditionType:  conditionType,
					Policy:         ccmcommon.Managed,
					HasDeployments: true,
					Namespace:      nsn,
					OperatorGVK:    testOperatorGVK,
					CRName:         "default",
					CRNamespace:    nsn,
				},
			}

			err = monitorDependencies(ctx, rr, testResourceID, configs)
			g.Expect(err).ShouldNot(HaveOccurred())

			cond := rr.Conditions.GetCondition(conditionType)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(tt.expectedStatus))

			if tt.expectedReason != "" {
				g.Expect(cond.Reason).To(Equal(tt.expectedReason))
			}

			if tt.expectedMsgMatch != "" {
				g.Expect(cond.Message).To(ContainSubstring(tt.expectedMsgMatch))
			}
		})
	}
}

func newDeployment(name, namespace string, replicas, readyReplicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{labels.InfrastructurePartOf: testResourceID},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      replicas,
			ReadyReplicas: readyReplicas,
		},
	}
}

func setCRCondition(g *WithT, ctx context.Context, cli client.Client, cr *unstructured.Unstructured, condType, condStatus, reason, message string) {
	crConditions := []any{
		map[string]any{
			"type":               condType,
			"status":             condStatus,
			"reason":             reason,
			"message":            message,
			"lastTransitionTime": metav1.Now().UTC().Format(time.RFC3339),
		},
	}

	err := unstructured.SetNestedSlice(cr.Object, crConditions, "status", "conditions")
	g.Expect(err).NotTo(HaveOccurred())

	err = cli.Status().Update(ctx, cr)
	g.Expect(err).NotTo(HaveOccurred())
}

func corev1PodTemplate(label string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": label}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
	}
}
