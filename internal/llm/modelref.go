package llm

import (
	"fmt"
	"strings"
)

type BackendType string

const (
	BackendOpenAI    BackendType = "openai"
	BackendAnthropic BackendType = "anthropic"
)

type ModelRef struct {
	Raw      string
	Backend  BackendType
	Provider string
	Model    string
}

func ParseModelRef(model string) (ModelRef, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ModelRef{}, fmt.Errorf("model is empty")
	}

	parts := strings.Split(model, "/")
	switch len(parts) {
	case 2:
		if parts[1] == "" {
			return ModelRef{}, fmt.Errorf("model id is empty in %q", model)
		}
		switch parts[0] {
		case string(BackendOpenAI):
			return ModelRef{Raw: model, Backend: BackendOpenAI, Provider: string(BackendOpenAI), Model: parts[1]}, nil
		case string(BackendAnthropic):
			return ModelRef{Raw: model, Backend: BackendAnthropic, Provider: string(BackendAnthropic), Model: parts[1]}, nil
		default:
			return ModelRef{}, fmt.Errorf("unsupported model backend %q", parts[0])
		}
	default:
		return ModelRef{}, fmt.Errorf("invalid model format %q: expected openai/<model> or anthropic/<model>", model)
	}
}
