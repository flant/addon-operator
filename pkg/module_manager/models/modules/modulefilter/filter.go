package modulefilter

func New() *AddonOperatorFilter {
	return &AddonOperatorFilter{}
}

type Filter interface {
	IsEmbeddedModule(moduleName string) bool
}

type AddonOperatorFilter struct{}

func (f *AddonOperatorFilter) IsEmbeddedModule(_ string) bool {
	return false
}
