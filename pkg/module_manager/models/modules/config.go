package modules

type Config struct {
	// required fields
	// parameters used by addon operator
	Name   string
	Weight uint32
	Path   string

	Tags           []string
	Subsystems     []string
	Namespace      string
	Stage          string
	ExclusiveGroup string

	Descriptions *ModuleDescriptions
	Requirements *ModuleRequirements

	DisableOptions *ModuleDisableOptions

	Accessibility *Accessibility
}

type ModuleDescriptions struct {
	Ru string
	En string
}

type ModuleRequirements struct {
	ModulePlatformRequirements
	ParentModules map[string]string
}

type ModulePlatformRequirements struct {
	Deckhouse    string
	Kubernetes   string
	Bootstrapped string
}

type ModuleDisableOptions struct {
	Confirmation bool
	Message      string
}

type Bundle string

type FeatureFlag string

type Batch struct {
	Available    bool
	Enabled      bool
	FeatureFlags []FeatureFlag
}

type Batches map[string]Batch

type Edition struct {
	Available        bool
	EnabledInBundles []Bundle
	FeatureFlags     []FeatureFlag
}

type Editions struct {
	Default *Edition
	Ee      *Edition
	Se      *Edition
	Be      *Edition
}

type Accessibility struct {
	Batches  *Batches
	Editions *Editions
}
