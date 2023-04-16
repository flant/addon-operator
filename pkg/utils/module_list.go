package utils

import (
	"sort"
)

// SortReverseByReference returns a new array with items, reverse sorted
// by the order of 'ref' array.
func SortReverseByReference(in []string, ref []string) []string {
	res := make([]string, 0)

	inValues := make(map[string]bool)
	for _, v := range in {
		inValues[v] = true
	}
	for _, v := range ref {
		if _, hasKey := inValues[v]; hasKey {
			// prepend
			res = append([]string{v}, res...)
		}
	}

	return res
}

// SortReverse creates a copy of 'in' array and sort it in a reverse order.
func SortReverse(in []string) []string {
	res := make([]string, 0)
	res = append(res, in...)
	sort.Sort(sort.Reverse(sort.StringSlice(res)))

	return res
}

// SortByReference returns a new array with items sorted by the order of 'ref' array.
func SortByReference(in []string, ref []string) []string {
	res := make([]string, 0)

	inValues := make(map[string]bool)
	for _, v := range in {
		inValues[v] = true
	}
	for _, v := range ref {
		if _, hasKey := inValues[v]; hasKey {
			res = append(res, v)
		}
	}

	return res
}

// KeysSortedByReference returns keys from map sorted by the order of 'ref' array.
// Note: keys not in ref are ignored.
func KeysSortedByReference(m map[string]struct{}, ref []string) []string {
	res := make([]string, 0)

	for _, v := range ref {
		if _, hasKey := m[v]; hasKey {
			res = append(res, v)
		}
	}

	return res
}

// ListSubtract creates a new array from 'src' array with items that are
// not present in 'ignored' arrays.
func ListSubtract(src []string, ignored ...[]string) (result []string) {
	ignoredMap := make(map[string]bool)
	for _, arr := range ignored {
		for _, v := range arr {
			ignoredMap[v] = true
		}
	}

	for _, v := range src {
		if _, ok := ignoredMap[v]; !ok {
			result = append(result, v)
		}
	}
	return
}

// ListIntersection returns an array with items that are present in all 'arrs' arrays.
func ListIntersection(arrs ...[]string) (result []string) {
	if len(arrs) == 0 {
		return
	}

	// Counts each item in arrays.
	m := make(map[string]int)
	for _, a := range arrs {
		for _, v := range a {
			m[v]++
		}
	}

	for k, v := range m {
		if v == len(arrs) {
			result = append(result, k)
		}
	}

	return
}

// ListUnion creates a new array with unique items from all src arrays.
func ListUnion(src []string, more ...[]string) []string {
	m := make(map[string]struct{})
	for _, v := range src {
		m[v] = struct{}{}
	}

	for _, arr := range more {
		for _, v := range arr {
			m[v] = struct{}{}
		}
	}

	res := make([]string, 0, len(m))

	for k := range m {
		res = append(res, k)
	}

	return res
}

// ListFullyIn returns whether all 'arr' items contains in `ref` array.
func ListFullyIn(arr []string, ref []string) bool {
	res := true

	m := make(map[string]bool)
	for _, v := range ref {
		m[v] = true
	}

	for _, v := range arr {
		if _, ok := m[v]; !ok {
			return false
		}
	}

	return res
}

func MapStringStructKeys(m map[string]struct{}) []string {
	res := make([]string, 0, len(m))

	for k := range m {
		res = append(res, k)
	}

	return res
}

func ListToMapStringStruct(items []string) map[string]struct{} {
	res := make(map[string]struct{})

	for _, item := range items {
		res[item] = struct{}{}
	}

	return res
}
