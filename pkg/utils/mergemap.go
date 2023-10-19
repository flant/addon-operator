package utils

import (
	"reflect"
)

var (
	MaxDepth = 32
)

// mergeMap recursively merges the src and dst maps. Key conflicts are resolved by
// preferring src, or recursively descending, if both src and dst are maps.
func mergeMap(dst, src map[string]interface{}) map[string]interface{} {
	return merge(dst, src, 0)
}

func merge(dst, src map[string]interface{}, depth int) map[string]interface{} {
	if depth > MaxDepth {
		panic("too deep!")
	}
	for key, srcVal := range src {
		if dstVal, ok := dst[key]; ok {
			srcMap, srcMapOk := mapify(srcVal)
			dstMap, dstMapOk := mapify(dstVal)
			if srcMapOk && dstMapOk {
				srcVal = merge(dstMap, srcMap, depth+1)
			}
		}
		dst[key] = srcVal
	}
	return dst
}

func mapify(i interface{}) (map[string]interface{}, bool) {
	switch i.(type) {
	case map[string]interface{}:
		return i.(map[string]interface{}), true
	case Values:
		return i.(Values), true
	}

	value := reflect.ValueOf(i)
	if value.Kind() == reflect.Map {
		m := make(map[string]interface{}, value.Len())
		for _, k := range value.MapKeys() {
			m[k.String()] = value.MapIndex(k).Interface()
		}
		return m, true
	}
	return map[string]interface{}{}, false
}
