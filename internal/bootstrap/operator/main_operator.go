package operator

import (
	"context"
	"fmt"
	"os"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	crtlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	operatorconfig "github.com/opendatahub-io/opendatahub-operator/v2/internal/bootstrap/config"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	dscctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"
	dscictrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/initialinstall"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

// mainOperator implements the Operator interface for the main OpenDataHub operator.
type mainOperator struct {
	config *operatorconfig.Config
	mgr    *manager.Manager
	scheme *runtime.Scheme
}

// New creates a new MainOperator instance.
func New(cfg *operatorconfig.Config) *mainOperator {
	scheme := runtime.NewScheme()
	RegisterSchemes(scheme)
	return &mainOperator{
		config: cfg,
		scheme: scheme,
	}
}

// Setup initializes the operator: creates the manager, registers controllers,
// webhooks, and any startup tasks.
func (o *mainOperator) Setup(ctx context.Context) error {
	// Create a non-cached client for setup operations
	setupClient, err := client.New(o.config.RestConfig, client.Options{Scheme: o.scheme})
	if err != nil {
		return fmt.Errorf("error getting client for setup: %w", err)
	}

	// Initialize cluster configuration
	if err := cluster.Init(ctx, setupClient); err != nil {
		return fmt.Errorf("unable to initialize cluster config: %w", err)
	}

	// Get operator platform
	release := cluster.GetRelease()
	platform := release.Name

	// Initialize services
	if err := o.initServices(ctx, platform); err != nil {
		return fmt.Errorf("unable to init services: %w", err)
	}

	// Initialize components
	if err := o.initComponents(ctx, platform); err != nil {
		return fmt.Errorf("unable to init components: %w", err)
	}

	// Create cache configurations
	secretCache, err := o.createSecretCacheConfig(platform)
	if err != nil {
		return fmt.Errorf("unable to get application namespace into cache: %w", err)
	}

	odhCache, err := o.createODHGeneralCacheConfig(platform)
	if err != nil {
		return fmt.Errorf("unable to get application namespace into cache: %w", err)
	}

	cacheOptions := cache.Options{
		Scheme: o.scheme,
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Secret{}: {
				Namespaces: secretCache,
			},
			&corev1.ConfigMap{}: {
				Namespaces: odhCache,
			},
			&operatorv1.IngressController{}: {
				Field: fields.Set{"metadata.name": "default"}.AsSelector(),
			},
			&configv1.Authentication{}: {
				Field: fields.Set{"metadata.name": cluster.ClusterAuthenticationObj}.AsSelector(),
			},
			&appsv1.Deployment{}: {
				Namespaces: odhCache,
			},
			&promv1.PrometheusRule{}: {
				Namespaces: odhCache,
			},
			&promv1.ServiceMonitor{}: {
				Namespaces: odhCache,
			},
			&routev1.Route{}: {
				Namespaces: odhCache,
			},
			&networkingv1.NetworkPolicy{}: {
				Namespaces: odhCache,
			},
			&rbacv1.Role{}: {
				Namespaces: odhCache,
			},
			&rbacv1.RoleBinding{}: {
				Namespaces: odhCache,
			},
		},
		DefaultTransform: func(in any) (any, error) {
			if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
				obj.SetManagedFields(nil)
			}
			return in, nil
		},
	}

	// Create the controller-runtime manager
	ctrlMgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  o.scheme,
		Metrics: ctrlmetrics.Options{BindAddress: o.config.MetricsAddr},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port: 9443,
		}),
		PprofBindAddress:       o.config.PprofAddr,
		HealthProbeBindAddress: o.config.HealthProbeAddr,
		Cache:                  cacheOptions,
		LeaderElection:         o.config.LeaderElection,
		LeaderElectionID:       "07ed84f7.opendatahub.io",
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					resources.GvkToUnstructured(gvk.OpenshiftIngress),
					&ofapiv1alpha1.Subscription{},
					&authorizationv1.SelfSubjectRulesReview{},
					&corev1.Pod{},
					&userv1.Group{},
					&ofapiv1alpha1.CatalogSource{},
				},
				Unstructured: true,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// Wrap the manager with custom client
	o.mgr = manager.New(ctrlMgr)

	// Register webhooks
	if err := webhook.RegisterAllWebhooks(o.mgr); err != nil {
		return fmt.Errorf("unable to register webhooks: %w", err)
	}

	// Setup DSCInitialization controller
	if err := (&dscictrl.DSCInitializationReconciler{
		Client:   o.mgr.GetClient(),
		Scheme:   o.mgr.GetScheme(),
		Recorder: o.mgr.GetEventRecorderFor("dscinitialization-controller"),
	}).SetupWithManager(ctx, o.mgr); err != nil {
		return fmt.Errorf("unable to create controller DSCInitialization: %w", err)
	}

	// Setup DataScienceCluster controller
	if err := dscctrl.NewDataScienceClusterReconciler(ctx, o.mgr); err != nil {
		return fmt.Errorf("unable to create controller DataScienceCluster: %w", err)
	}

	// Initialize service reconcilers
	if err := o.createServiceReconcilers(ctx); err != nil {
		return fmt.Errorf("unable to create service controllers: %w", err)
	}

	// Initialize component reconcilers
	if err := o.createComponentReconcilers(ctx); err != nil {
		return fmt.Errorf("unable to create component controllers: %w", err)
	}

	// Add startup tasks
	if err := o.addStartupTasks(ctx, setupClient, platform); err != nil {
		return err
	}

	// Add health checks
	if err := o.mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := o.mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	return nil
}

// Start runs the operator (blocking).
func (o *mainOperator) Start(ctx context.Context) error {
	return o.mgr.Start(ctx)
}

func (o *mainOperator) initServices(_ context.Context, p common.Platform) error {
	return sr.ForEach(func(sh sr.ServiceHandler) error {
		return sh.Init(p)
	})
}

func (o *mainOperator) initComponents(_ context.Context, p common.Platform) error {
	return cr.ForEach(func(ch cr.ComponentHandler) error {
		return ch.Init(p)
	})
}

func (o *mainOperator) createServiceReconcilers(ctx context.Context) error {
	log := logf.FromContext(ctx)
	return sr.ForEach(func(sh sr.ServiceHandler) error {
		log.Info("creating reconciler", "type", "service", "name", sh.GetName())
		if err := sh.NewReconciler(ctx, o.mgr); err != nil {
			return fmt.Errorf("error creating %s service reconciler: %w", sh.GetName(), err)
		}
		return nil
	})
}

func (o *mainOperator) createComponentReconcilers(ctx context.Context) error {
	log := logf.FromContext(ctx)
	return cr.ForEach(func(ch cr.ComponentHandler) error {
		log.Info("creating reconciler", "type", "component", "name", ch.GetName())
		if err := ch.NewComponentReconciler(ctx, o.mgr); err != nil {
			return fmt.Errorf("error creating %s component reconciler: %w", ch.GetName(), err)
		}
		return nil
	})
}

func (o *mainOperator) addStartupTasks(ctx context.Context, setupClient client.Client, platform common.Platform) error {
	setupLog := logf.FromContext(ctx)

	// Check if user opted for disabling DSC configuration
	disableDSCConfig, existDSCConfig := os.LookupEnv("DISABLE_DSC_CONFIG")
	if existDSCConfig && disableDSCConfig != "false" {
		setupLog.Info("DSCI auto creation is disabled")
	} else {
		createDefaultDSCIFunc := leaderElectionRunnableFunc(func(ctx context.Context) error {
			setupLog.Info("create default DSCI")
			err := initialinstall.CreateDefaultDSCI(ctx, setupClient, platform, o.config.MonitoringNamespace)
			if err != nil {
				setupLog.Error(err, "unable to create initial setup for the operator")
			}
			return err
		})

		if err := o.mgr.Add(createDefaultDSCIFunc); err != nil {
			return fmt.Errorf("error scheduling DSCI creation: %w", err)
		}
	}

	// Create default DSC CR for managed RHOAI
	if platform == cluster.ManagedRhoai {
		createDefaultDSCFunc := leaderElectionRunnableFunc(func(ctx context.Context) error {
			setupLog.Info("create default DSC")
			err := initialinstall.CreateDefaultDSC(ctx, setupClient)
			if err != nil {
				setupLog.Error(err, "unable to create default DSC CR by the operator")
			}
			return err
		})
		if err := o.mgr.Add(createDefaultDSCFunc); err != nil {
			return fmt.Errorf("error scheduling DSC creation: %w", err)
		}
	}

	// Cleanup resources from previous v2 releases
	cleanup := leaderElectionRunnableFunc(func(ctx context.Context) error {
		setupLog.Info("run upgrade task")
		if err := upgrade.CleanupExistingResource(ctx, setupClient); err != nil {
			setupLog.Error(err, "unable to perform cleanup")
			return err
		}
		return nil
	})

	if err := o.mgr.Add(cleanup); err != nil {
		setupLog.Error(err, "error remove deprecated resources from previous version")
	}

	return nil
}

func (o *mainOperator) getCommonCache(platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs := map[string]cache.Config{}

	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil, err
	}

	namespaceConfigs[operatorNs] = cache.Config{}
	namespaceConfigs["redhat-ods-monitoring"] = cache.Config{}

	appNamespace := cluster.GetApplicationNamespace()
	namespaceConfigs[appNamespace] = cache.Config{}

	if platform == cluster.ManagedRhoai {
		namespaceConfigs[cluster.NamespaceConsoleLink] = cache.Config{}
	}

	return namespaceConfigs, nil
}

func (o *mainOperator) createSecretCacheConfig(platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs, err := o.getCommonCache(platform)
	if err != nil {
		return nil, err
	}

	namespaceConfigs["openshift-ingress"] = cache.Config{}

	return namespaceConfigs, nil
}

func (o *mainOperator) createODHGeneralCacheConfig(platform common.Platform) (map[string]cache.Config, error) {
	namespaceConfigs, err := o.getCommonCache(platform)
	if err != nil {
		return nil, err
	}

	namespaceConfigs["openshift-operators"] = cache.Config{}
	namespaceConfigs["openshift-ingress"] = cache.Config{}

	return namespaceConfigs, nil
}

//nolint:ireturn
func leaderElectionRunnableFunc(fn crtlmanager.RunnableFunc) crtlmanager.Runnable {
	return &leaderElectionRunnableWrapper{Fn: fn}
}

type leaderElectionRunnableWrapper struct {
	Fn crtlmanager.RunnableFunc
}

func (l *leaderElectionRunnableWrapper) Start(ctx context.Context) error {
	return l.Fn(ctx)
}

func (l *leaderElectionRunnableWrapper) NeedLeaderElection() bool {
	return true
}
