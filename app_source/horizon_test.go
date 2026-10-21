package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

// The weekly / monthly sigma must equal the Python reference's term structure (garch_results.json).
func TestHorizonSigmaMatchesPython(t *testing.T) {
	b, err := os.ReadFile("../code/garch_results.json")
	if err != nil {
		t.Skip("pack data not found")
	}
	var ref struct {
		Params   struct{ Mu, Omega, Alpha, Beta float64 }
		Forecast struct {
			Sigma float64            `json:"sigma_1d_pct"`
			Term  map[string]float64 `json:"term_structure_sigma_pct"`
		}
		Last float64 `json:"last_close"`
	}
	json.Unmarshal(b, &ref)
	// json tags are lower-case in the file
	var raw map[string]any
	json.Unmarshal(b, &raw)
	pm := raw["params"].(map[string]any)
	p := Params{pm["mu"].(float64), pm["omega"].(float64), pm["alpha"].(float64), pm["beta"].(float64)}
	for _, h := range []int{1, 5, 10, 21} {
		got := HorizonSigma(p, ref.Forecast.Sigma*ref.Forecast.Sigma, h)
		want := ref.Forecast.Term[fmt.Sprintf("%dd", h)]
		fmt.Printf("h=%d go %.4f python %.4f\n", h, got, want)
		if math.Abs(got-want) > 1e-6 {
			t.Errorf("h=%d: %v vs %v", h, got, want)
		}
	}
}
