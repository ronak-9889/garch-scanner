package main

import (
	"math"
	"runtime"
	"sync"
)

// parallel runs fn(0..n-1) on all CPU cores.
func parallel(n int, fn func(i int)) {
	w := runtime.NumCPU()
	if w > n {
		w = n
	}
	if w < 1 {
		w = 1
	}
	var wg sync.WaitGroup
	ch := make(chan int)
	for k := 0; k < w; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

func pctReturns(c []float64) []float64 {
	r := make([]float64, 0, len(c))
	for i := 1; i < len(c); i++ {
		r = append(r, 100*math.Log(c[i]/c[i-1]))
	}
	return r
}
