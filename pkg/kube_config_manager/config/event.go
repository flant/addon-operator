package config

type Event struct {
	// Key possible values
	// "" - reset the whole config
	// "batch" - set global and modules config at once
	// "global" - set only global config
	// "<moduleName> - set only config for the module <moduleName>
	Key    string
	Config *KubeConfig
	Err    error
}
