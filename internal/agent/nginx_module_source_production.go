//go:build !module_dev

package agent

func configuredModuleSource() moduleSource { return officialModuleSource() }
