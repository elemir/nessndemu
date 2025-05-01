package cbool

import "golang.org/x/exp/constraints"

type Bool bool

func FromInt[I constraints.Integer](i I) Bool {
	if i == 0 {
		return false
	}

	return true
}

func ToInt[I constraints.Integer](b Bool) I {
	if b {
		return 1
	}

	return 0
}
