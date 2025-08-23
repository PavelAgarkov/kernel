package utils

import "golang.org/x/exp/constraints"

func SortedDifference[T constraints.Ordered](base, exclude []T) []T {
	result := make([]T, 0)
	baseIdx, exclIdx := 0, 0

	for baseIdx < len(base) && exclIdx < len(exclude) {
		switch {
		case base[baseIdx] < exclude[exclIdx]:
			result = append(result, base[baseIdx])
			baseIdx++
		case base[baseIdx] > exclude[exclIdx]:
			exclIdx++
		default:
			baseIdx++
			exclIdx++
		}
	}

	result = append(result, base[baseIdx:]...)
	return result
}

func Distinct[T comparable](src []T) []T {
	seen := make(map[T]struct{}, len(src))
	out := make([]T, 0, len(src))

	for _, v := range src {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
