// GARCH(1,1) - the textbook model (Bollerslev 1986), fit by maximum likelihood.
//
//	r_t       = mu + e_t                          daily % return = a constant mean + a surprise
//	e_t       = sigma_t * z_t                     z ~ N(0,1)
//	sigma_t^2 = omega + alpha*e_{t-1}^2 + beta*sigma_{t-1}^2
//
// Same model, same likelihood and same variance back-cast as the Python reference
// (garch_nifty.py, `arch` package) - see verify.go, which compares the two. No third-party
// libraries: a small Nelder-Mead optimiser is included below.
package main

import (
	"errors"
	"math"
	"sort"
)

type Params struct {
	Mu, Omega, Alpha, Beta float64
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// x -> (mu, omega, alpha, beta). Always a valid, stationary model: omega>0, alpha,beta>=0,
// alpha+beta<0.999. The optimiser works in x-space, so it can never produce an invalid model.
func unpack(x []float64) Params {
	alpha := 0.999 * sigmoid(x[2])
	beta := (0.999 - alpha) * sigmoid(x[3])
	return Params{Mu: x[0], Omega: math.Exp(x[1]), Alpha: alpha, Beta: beta}
}

// backcast: the arch package's convention for the starting variance - an exponentially weighted
// average (weight 0.94^i) of the first min(75,n) squared, demeaned observations.
func backcast(e []float64) float64 {
	tau := len(e)
	if tau > 75 {
		tau = 75
	}
	w, sw, s2 := 1.0, 0.0, 0.0
	for i := 0; i < tau; i++ {
		s2 += w * e[i] * e[i]
		sw += w
		w *= 0.94
	}
	return s2 / sw
}

func negLogLik(r []float64, p Params) float64 {
	n := len(r)
	e := make([]float64, n)
	for i, v := range r {
		e[i] = v - p.Mu
	}
	s2 := backcast(e)
	ll := 0.0
	for t := 0; t < n; t++ {
		if t > 0 {
			s2 = p.Omega + p.Alpha*e[t-1]*e[t-1] + p.Beta*s2
		}
		if s2 < 1e-10 {
			s2 = 1e-10
		}
		ll += -0.5 * (math.Log(2*math.Pi) + math.Log(s2) + e[t]*e[t]/s2)
	}
	return -ll
}

// nelderMead minimises f from x0. Standard coefficients (1, 2, 0.5, 0.5), adaptive-free, with a
// tight simultaneous tolerance on the simplex size and on the spread of function values.
func nelderMead(f func([]float64) float64, x0 []float64, step []float64, maxIter int) ([]float64, float64) {
	n := len(x0)
	type vert struct {
		x []float64
		f float64
	}
	s := make([]vert, n+1)
	s[0] = vert{append([]float64(nil), x0...), f(x0)}
	for i := 0; i < n; i++ {
		x := append([]float64(nil), x0...)
		x[i] += step[i]
		s[i+1] = vert{x, f(x)}
	}
	cen := make([]float64, n)
	mk := func(c, w []float64, t float64) []float64 { // c + t*(w-c)
		x := make([]float64, n)
		for i := range x {
			x[i] = c[i] + t*(w[i]-c[i])
		}
		return x
	}
	for it := 0; it < maxIter; it++ {
		sort.Slice(s, func(a, b int) bool { return s[a].f < s[b].f })
		size := 0.0
		for i := 1; i <= n; i++ {
			for j := 0; j < n; j++ {
				size = math.Max(size, math.Abs(s[i].x[j]-s[0].x[j]))
			}
		}
		if size < 1e-9 && math.Abs(s[n].f-s[0].f) < 1e-10 {
			break
		}
		for j := range cen {
			cen[j] = 0
			for i := 0; i < n; i++ {
				cen[j] += s[i].x[j] / float64(n)
			}
		}
		worst := s[n]
		xr := mk(cen, worst.x, -1)
		fr := f(xr)
		switch {
		case fr < s[0].f:
			xe := mk(cen, worst.x, -2)
			if fe := f(xe); fe < fr {
				s[n] = vert{xe, fe}
			} else {
				s[n] = vert{xr, fr}
			}
		case fr < s[n-1].f:
			s[n] = vert{xr, fr}
		default:
			var xc []float64
			if fr < worst.f {
				xc = mk(cen, worst.x, -0.5) // outside contraction
			} else {
				xc = mk(cen, worst.x, 0.5) // inside contraction
			}
			if fc := f(xc); fc < math.Min(fr, worst.f) {
				s[n] = vert{xc, fc}
			} else { // shrink towards the best vertex
				for i := 1; i <= n; i++ {
					s[i].x = mk(s[0].x, s[i].x, 0.5)
					s[i].f = f(s[i].x)
				}
			}
		}
	}
	sort.Slice(s, func(a, b int) bool { return s[a].f < s[b].f })
	return s[0].x, s[0].f
}

// FitGARCH11 fits (mu, omega, alpha, beta) by maximum likelihood on a series of PERCENT returns
// (100*ln(p_t/p_{t-1})). The likelihood has a flat ridge near persistence = 1, which can strand a
// single Nelder-Mead run, so it restarts from several starting points, keeps the best, then
// polishes it with further restarts from its own result until it stops improving.
func FitGARCH11(r []float64) (Params, error) {
	if len(r) < 100 {
		return Params{}, errors.New("need at least 100 returns to fit GARCH")
	}
	meanR, sampleVar := 0.0, 0.0
	for _, v := range r {
		meanR += v
	}
	meanR /= float64(len(r))
	for _, v := range r {
		sampleVar += (v - meanR) * (v - meanR)
	}
	sampleVar /= float64(len(r))

	obj := func(x []float64) float64 {
		v := negLogLik(r, unpack(x))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 1e18
		}
		return v
	}
	step := []float64{0.05, 0.5, 0.5, 0.5}
	starts := [][]float64{
		{meanR, math.Log(sampleVar * 0.05), -1.5, 2.2},
		{meanR, math.Log(sampleVar * 0.05), 0.0, 2.2},
		{meanR, math.Log(sampleVar * 0.10), 0.5, 1.0},
		{meanR, math.Log(sampleVar * 0.02), -1.0, 3.5},
		{meanR, math.Log(sampleVar * 0.20), 1.0, 0.0},
	}
	var bx []float64
	bf := math.Inf(1)
	for _, x0 := range starts {
		x, f := nelderMead(obj, x0, step, 4000)
		if f < bf {
			bx, bf = x, f
		}
	}
	for k := 0; k < 6; k++ { // polish
		x, f := nelderMead(obj, bx, []float64{0.01, 0.1, 0.1, 0.1}, 4000)
		if f < bf-1e-9 {
			bx, bf = x, f
		} else {
			break
		}
	}
	if bx == nil || bf >= 1e17 {
		return Params{}, errors.New("GARCH fit did not converge")
	}
	return unpack(bx), nil
}

// condVar runs the recursion over r[:n] and returns sigma^2 for the NEXT day (index n).
func condVar(r []float64, n int, p Params) float64 {
	e := make([]float64, n)
	for k := 0; k < n; k++ {
		e[k] = r[k] - p.Mu
	}
	s2 := backcast(e)
	for t := 1; t < n; t++ {
		s2 = p.Omega + p.Alpha*e[t-1]*e[t-1] + p.Beta*s2
	}
	return p.Omega + p.Alpha*e[n-1]*e[n-1] + p.Beta*s2
}

// condVarOOS is the reference's out-of-sample recursion: start from the sample variance of the
// demeaned history, then run the recursion over every observation (mirrors garch_nifty.py).
func condVarOOS(r []float64, n int, p Params) float64 {
	m := 0.0
	for k := 0; k < n; k++ {
		m += r[k] - p.Mu
	}
	m /= float64(n)
	s2 := 0.0
	for k := 0; k < n; k++ {
		d := r[k] - p.Mu - m
		s2 += d * d
	}
	s2 /= float64(n)
	for k := 0; k < n; k++ {
		e := r[k] - p.Mu
		s2 = p.Omega + p.Alpha*e*e + p.Beta*s2
	}
	return s2
}

// Forecast1: one-step-ahead sigma (in %) after the last observation in r.
func Forecast1(r []float64, p Params) float64 { return math.Sqrt(condVar(r, len(r), p)) }

// OOSPoint is one day of the honest, out-of-sample test: sigma and mu known BEFORE that day.
type OOSPoint struct {
	Idx   int
	Sigma float64
	Mu    float64
	P     Params // the parameters in force that day (refit every 21 days)
}

// RollingOOS: for each of the last `window` returns, a forecast made only from data up to the day
// before, refitting every `refit` days (21 = about once a month). Refits run in parallel.
func RollingOOS(r []float64, window, refit int) ([]OOSPoint, error) {
	n := len(r)
	start := n - window
	if start < 250 {
		return nil, errors.New("not enough history for the out-of-sample check")
	}
	nfit := (window + refit - 1) / refit
	fits := make([]Params, nfit)
	errs := make([]error, nfit)
	parallel(nfit, func(k int) { fits[k], errs[k] = FitGARCH11(r[:start+k*refit]) })
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	out := make([]OOSPoint, window)
	parallel(window, func(j int) {
		i := start + j
		p := fits[j/refit]
		out[j] = OOSPoint{i, math.Sqrt(condVarOOS(r, i, p)), p.Mu, p}
	})
	return out, nil
}

// HorizonSigma: sigma (in %) of the h-day cumulative return, given tomorrow's variance v1.
// GARCH variance fades back to its long-run level: v_k = L + (alpha+beta)^(k-1) * (v1 - L), with
// L = omega/(1-alpha-beta); the h-day variance is the sum v_1 + ... + v_h (same as the arch package's
// forecast(horizon=h) summed, as in garch_nifty.py's term structure).
func HorizonSigma(p Params, v1 float64, h int) float64 {
	pers := p.Alpha + p.Beta
	long := p.Omega / (1 - pers)
	sum := 0.0
	for k := 1; k <= h; k++ {
		sum += long + math.Pow(pers, float64(k-1))*(v1-long)
	}
	return math.Sqrt(sum)
}
