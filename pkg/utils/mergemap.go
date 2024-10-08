package utils

import (
	"reflect"
)

const maxDepth = 32

// mergeMap recursively merges the src and dst maps. Key conflicts are resolved by
// preferring src, or recursively descending, if both src and dst are maps.
func mergeMap(dst, src map[string]interface{}) map[string]interface{} {
	return merge(dst, src, 0)
}

func merge(dst, src map[string]interface{}, depth int) map[string]interface{} {
	if depth > maxDepth {
		panic("too deep!")
	}

	for key, srcVal := range src {
		srcMap, srcMapOk := mapify(srcVal)
		if dstVal, ok := dst[key]; ok {
			dstMap, dstMapOk := mapify(dstVal)
			if srcMapOk && dstMapOk {
				dst[key] = merge(dstMap, srcMap, depth+1)
				continue
			}
		}

		if srcMapOk {
			dst[key] = deepCopyMap(srcMap)
		} else {
			dst[key] = srcVal
		}
	}
	return dst
}

func mapify(i interface{}) (map[string]interface{}, bool) {
	switch v := i.(type) {
	case map[string]interface{}:
		return v, true
	case Values:
		return v, true
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
