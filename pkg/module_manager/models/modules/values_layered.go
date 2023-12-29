package modules

import "github.com/flant/addon-operator/pkg/utils"

type valuesTransform func(values utils.Values) utils.Values

type valuesTransformer interface {
	Transform(values utils.Values) utils.Values
}

func mergeLayers(initial utils.Values, layers ...interface{}) utils.Values {
	res := utils.MergeValues(initial)

	for _, layer := range layers {
		switch layer := layer.(type) {
		case utils.Values:
			res = utils.MergeValues(res, layer)
		case map[string]interface{}:
			res = utils.MergeValues(res, layer)
		case string:
			// Ignore error to be handy for tests.
			tmp, _ := utils.NewValuesFromBytes([]byte(layer))
			res = utils.MergeValues(res, tmp)
		case valuesTransform:
			res = utils.MergeValues(res, layer(res))
		case valuesTransformer:
			res = utils.MergeValues(res, layer.Transform(res))
		case nil:
			continue
		}
	}

	return res
}
