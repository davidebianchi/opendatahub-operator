package factory

import (
	"context"
	"errors"
	"fmt"

	operatorconfig "github.com/opendatahub-io/opendatahub-operator/v2/internal/bootstrap/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/bootstrap/operator"
)

// Operator defines the contract for different operator implementations.
// Each operator type (e.g., main operator, cloud manager) implements this interface.
type Operator interface {
	// Setup initializes the operator: creates the manager, registers controllers,
	// webhooks, and any startup tasks. Must be called before Start.
	Setup(ctx context.Context) error

	// Start runs the operator (blocking). This starts the manager and all
	// registered controllers. Returns when the context is cancelled or on error.
	Start(ctx context.Context) error
}

// OperatorType defines the available operator implementations.
type OperatorType string

const (
	OperatorTypeMain         OperatorType = "main"
	OperatorTypeCloudManager OperatorType = "cloud-manager"
)

// Factory creates Operator instances based on the operator type.
type Factory struct {
	config *operatorconfig.Config
}

// NewFactory creates a new Factory with the given configuration.
func NewFactory(cfg *operatorconfig.Config) *Factory {
	return &Factory{config: cfg}
}

// Create returns an Operator implementation for the specified type.
func (f *Factory) Create(operatorType OperatorType) (Operator, error) {
	switch operatorType {
	case OperatorTypeMain:
		return operator.New(f.config), nil
	case OperatorTypeCloudManager:
		return nil, errors.New("cloud-manager operator not yet implemented")
	default:
		return nil, fmt.Errorf("unknown operator type: %s", operatorType)
	}
}
