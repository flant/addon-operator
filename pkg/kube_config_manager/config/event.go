package config

type Op string

const (
	EventDelete Op = "Delete"
	EventUpdate Op = "Update"
	EventAdd    Op = "Add"
)

type Event struct {
	// Key possible values
	// "" - reset the whole config
	// "batch" - set global and modules config at once
	// "global" - set only global config
	// "<moduleName> - set only config for the module <moduleName>
	Key    string
	Config *KubeConfig
	Err    error
	Op     Op
}
