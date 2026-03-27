package builder

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestPtToPixels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pt      string
		ppi     int
		want    float64
		wantErr bool
	}{
		{name: "standard", pt: "311.5pt", ppi: 192, want: 830.6666666666666},
		{name: "one inch", pt: "72pt", ppi: 192, want: 192.0},
		{name: "zero", pt: "0pt", ppi: 192, want: 0.0},
		{name: "real data", pt: "185.17pt", ppi: 192, want: 493.7866666666667},
		{name: "invalid", pt: "abc", ppi: 192, wantErr: true},
		{name: "wrong suffix", pt: "100px", ppi: 192, wantErr: true},
		{name: "empty", pt: "", ppi: 192, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := PtToPixels(tt.pt, tt.ppi)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("PtToPixels(%q, %d) error = nil, want error", tt.pt, tt.ppi)
				}
				return
			}

			if err != nil {
				t.Fatalf("PtToPixels(%q, %d) unexpected error: %v", tt.pt, tt.ppi, err)
			}
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("PtToPixels(%q, %d) = %v, want %v", tt.pt, tt.ppi, got, tt.want)
			}
		})
	}
}

func TestParseAllLabels(t *testing.T) {
	t.Parallel()

	parseLabels := func(input string) ([]string, error) {
		var results []typstQueryResult
		if err := json.Unmarshal([]byte(input), &results); err != nil {
			return nil, err
		}

		labels := make([]string, 0, len(results))
		for _, result := range results {
			labels = append(labels, strings.Trim(result.Label, "<>"))
		}

		return labels, nil
	}

	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "labels from typst metadata",
			input: `[{"label":"<education>","value":{"pos":{"page":1,"x":"72pt","y":"185pt"},"size":{"width":"468pt","height":"160pt"}}},{"label":"<jobs>","value":{"pos":{"page":1,"x":"72pt","y":"345pt"},"size":{"width":"468pt","height":"200pt"}}}]`,
			want:  []string{"education", "jobs"},
		},
		{
			name:  "empty array",
			input: `[]`,
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseLabels(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseLabels() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLabels() unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseLabels() len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseLabels()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
