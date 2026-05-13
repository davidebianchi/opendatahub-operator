package cloudmanager

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	ccmcharts "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func monitorDependencies(ctx context.Context, rr *types.ReconciliationRequest, resourceID string, configs []ccmcharts.DependencyMonitorConfig) error {
	for _, cfg := range configs {
		if cfg.Policy == ccmcommon.Unmanaged {
			rr.Conditions.MarkTrue(
				cfg.ConditionType,
				conditions.WithReason(status.UnmanagedReason),
			)

			continue
		}

		// Tier 1: operator deployment health
		if cfg.HasDeployments {
			depAction := deployments.NewAction(
				deployments.WithConditionType(cfg.ConditionType),
				deployments.InNamespace(cfg.Namespace),
				deployments.WithPartOfLabel(labels.InfrastructurePartOf),
				deployments.WithSelectorLabel(labels.InfrastructurePartOf, resourceID),
			)

			if err := depAction(ctx, rr); err != nil {
				return fmt.Errorf("deployment check for %s failed: %w", cfg.ReleaseName, err)
			}
		}

		// Tier 2: operator CR health (skip if Tier 1 already marked unhealthy)
		cond := rr.Conditions.GetCondition(cfg.ConditionType)
		if cfg.OperatorGVK.Kind != "" && (cond == nil || cond.Status != metav1.ConditionFalse) {
			crAction := dependency.NewAction(
				dependency.WithConditionType(cfg.ConditionType),
				dependency.MonitorOperator(dependency.OperatorConfig{
					OperatorGVK: cfg.OperatorGVK,
					CRName:      cfg.CRName,
					CRNamespace: cfg.CRNamespace,
				}),
			)

			if err := crAction(ctx, rr); err != nil {
				return fmt.Errorf("operator CR check for %s failed: %w", cfg.ReleaseName, err)
			}
		}

		// No deployments and no CR (e.g. GatewayAPI): mark available after successful deploy
		if !cfg.HasDeployments && cfg.OperatorGVK.Kind == "" {
			rr.Conditions.MarkTrue(cfg.ConditionType)
		}
	}

	return nil
}
