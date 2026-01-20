package builder

import (
	"strings"
	"testing"
)

func TestNewBuilder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		builderType string
		wantErr     bool
		errContains string
	}{
		{
			name:        "typst builder returns valid builder",
			builderType: "typst",
			wantErr:     false,
		},
		{
			name:        "unsupported type returns error",
			builderType: "unsupported",
			wantErr:     true,
			errContains: "unsupported builder type",
		},
		{
			name:        "empty type returns error",
			builderType: "",
			wantErr:     true,
			errContains: "unsupported builder type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder, err := NewBuilder(tt.builderType)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewBuilder(%q) expected error, got nil", tt.builderType)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("NewBuilder(%q) error = %v, want error containing %q", tt.builderType, err, tt.errContains)
				}
				if builder != nil {
					t.Errorf("NewBuilder(%q) expected nil builder on error, got %v", tt.builderType, builder)
				}
			} else {
				if err != nil {
					t.Errorf("NewBuilder(%q) unexpected error: %v", tt.builderType, err)
					return
				}
				if builder == nil {
					t.Errorf("NewBuilder(%q) expected non-nil builder, got nil", tt.builderType)
				}
			}
		})
	}
}
