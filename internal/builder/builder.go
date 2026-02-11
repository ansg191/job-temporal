package builder

import (
	"context"
	"fmt"
)

type Builder interface {
	Build(ctx context.Context, path, outputPath string) (*BuildResult, error)
}

type BuildResult struct {
	Success bool
	Errors  []string
}

func NewBuilder(builderType string, opts ...func(Builder)) (Builder, error) {
	switch builderType {
	case "typst":
		return newTypstBuilder(opts...)
	default:
		return nil, fmt.Errorf("unsupported builder type: %s", builderType)
	}
}
