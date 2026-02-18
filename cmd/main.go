/*
Copyright 2023.

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

package main

import (
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	operatorconfig "github.com/opendatahub-io/opendatahub-operator/v2/internal/bootstrap/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/bootstrap/factory"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"

	// Import controllers to register them via init().
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/datasciencepipelines"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/feastoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kserve"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/llamastackoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/mlflowoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelcontroller"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelsasservice"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/ray"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/sparkoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trainer"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trainingoperator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/trustyai"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/workbenches"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/auth"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/certconfigmapgenerator"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/monitoring"
	_ "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/setup"
)

var setupLog = ctrl.Log.WithName("setup")

func main() {
	cfg, err := operatorconfig.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading configuration: %s", err.Error())
		os.Exit(1)
	}

	ctrl.SetLogger(logger.NewLogger(cfg.LogMode, cfg.ZapOptions))

	// Root context with signal handling
	ctx := ctrl.SetupSignalHandler()
	ctx = logf.IntoContext(ctx, setupLog)

	// Create factory and get operator
	f := factory.NewFactory(cfg)
	op, err := f.Create(factory.OperatorTypeMain)
	if err != nil {
		setupLog.Error(err, "unable to create operator")
		os.Exit(1)
	}

	// Setup operator
	if err := op.Setup(ctx); err != nil {
		setupLog.Error(err, "unable to setup operator")
		os.Exit(1)
	}

	// Start operator
	setupLog.Info("starting operator")
	if err := op.Start(ctx); err != nil {
		setupLog.Error(err, "problem running operator")
		os.Exit(1)
	}
}
