package nessndemu

import "fmt"

func assert(expr bool) {
	if !expr {
		panic(expr)
	}
}

func require(expr bool) {
	if !expr {
		panic(fmt.Sprintf("unment requirement: %t", expr))
	}
}
