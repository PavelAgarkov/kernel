package utils

import "fmt"

func SliceToStrings[T any](items []T) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		itStr := fmt.Sprintf("%v", it)
		out = append(out, itStr)
	}
	return out
}
