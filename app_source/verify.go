// -verify <csv>: fit on a local CSV (date,open,high,low,close,volume) and print the numbers the
// Python reference (garch_nifty.py) prints, so the two can be compared without a Dhan token.
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

func runVerify(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var closes []float64
	var dates []string
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		parts := strings.Split(sc.Text(), ",")
		if len(parts) < 5 {
			continue
		}
		c, err := strconv.ParseFloat(parts[4], 64)
		if err != nil {
			continue
		}
		dates = append(dates, parts[0])
		closes = append(closes, c)
	}
	r := pctReturns(closes)
	fmt.Printf("%d days -> %d returns, %s to %s\n", len(closes), len(r), dates[0], dates[len(dates)-1])
	p, err := FitGARCH11(r)
	if err != nil {
		fmt.Println("fit:", err)
		os.Exit(1)
	}
	persist := p.Alpha + p.Beta
	fmt.Printf("mu=%.4f  omega=%.4f  alpha=%.4f  beta=%.4f  loglik=%.4f\n", p.Mu, p.Omega, p.Alpha, p.Beta, -negLogLik(r, p))
	fmt.Printf("persistence=%.4f  half_life_days=%.2f  unconditional_sigma_pct=%.4f\n", persist, math.Log(0.5)/math.Log(persist), math.Sqrt(p.Omega/(1-persist)))
	fmt.Printf("tomorrow's sigma_1d_pct=%.4f\n", Forecast1(r, p))
	oos, err := RollingOOS(r, 756, 21)
	if err != nil {
		fmt.Println("check:", err)
		return
	}
	in1, in2 := 0, 0
	for _, o := range oos {
		dev := math.Abs(r[o.Idx] - o.Mu)
		if dev <= o.Sigma {
			in1++
		}
		if dev <= 1.96*o.Sigma {
			in2++
		}
	}
	fmt.Printf("check (last %d days): inner %d = %.1f%%   wide %d = %.1f%%\n", len(oos), in1, 100*float64(in1)/float64(len(oos)), in2, 100*float64(in2)/float64(len(oos)))
	if len(os.Args) > 3 { // optional: dump the sigma series to compare with garch_series.csv
		out, _ := os.Create(os.Args[3])
		defer out.Close()
		for _, o := range oos {
			fmt.Fprintf(out, "%d,%.6f,%.6f\n", o.Idx, o.Sigma, o.Mu)
		}
	}
}

func loadRet(path string) []float64 {
	b, _ := os.ReadFile(path)
	var c []float64
	for i, l := range strings.Split(string(b), "\n") {
		p := strings.Split(l, ",")
		if i == 0 || len(p) < 5 {
			continue
		}
		v, err := strconv.ParseFloat(p[4], 64)
		if err == nil {
			c = append(c, v)
		}
	}
	return pctReturns(c)
}
