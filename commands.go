package pickett

import (
	"fmt"
	"os"
)

// CmdRun is the 'run' entry point of the program with the targets filled in
// and a working helper.
func CmdRun(targets []string, config *Config) {
	buildables, runnables := config.EntryPoints()
	run := false
	runTarget := ""

	// if you don't tell us what to build, we build everything with no outgoing
	// edges, the "root" of a backchain
	if len(targets) == 0 {
		targets = buildables
	} else {
		//if you do tell us, we need know if it's runnable
		for _, t := range targets {
			if contains(buildables, t) {
				continue
			}
			if contains(runnables, t) {
				if run {
					fmt.Fprintf(os.Stderr, "[pickett] can only run one target (%s and %s both runnable)\n", runTarget, t)
					os.Exit(1)
				}
				run = true
				runTarget = t
				continue
			}
			fmt.Fprintf(os.Stderr, "[pickett] don't know anything about target %s\n", t)
			os.Exit(1)
		}
	}
	for _, target := range targets {
		if target == runTarget {
			continue
		}
		err := config.Build(target)
		config.helper.CheckFatal(err, "%s: %v", target)
	}
	if runTarget != "" {
		err := config.Execute(runTarget)
		config.helper.CheckFatal(err, "%s: %v", runTarget)
	}
}

// CmdStatus shows the status of all known targets
func CmdStatus(config *Config) {
	targets := confTargets(config)
	fmt.Println(config.cli.TargetsStatus(targets))
}

// CmdStop stops the targets containers
func CmdStop(targets []string, config *Config) {
	if len(targets) == 0 {
		targets = confTargets(config)
	}
	config.cli.TargetsStop(targets)
}

// CmdDrop stops and removes the targets containers
func CmdDrop(targets []string, config *Config) {
	if len(targets) == 0 {
		targets = confTargets(config)
	}
	config.cli.TargetsDrop(targets)
}

// CmdWipe stops the targets containers
func CmdWipe(targets []string, config *Config) {
	if len(targets) == 0 {
		targets = confTargets(config)
	}
	config.cli.TargetsWipe(targets)
}

// checkTargets check the targets against the targets found in the config,
// returns an error if it's not matching, nil otherwise
func checkTargets(config *Config, targets []string) error {
	confTargets := confTargets(config)
	for _, target := range targets {
		if !contains(confTargets, target) {
			return fmt.Errorf("Unknowm target %s", target)
		}
	}
	return nil
}

// allTargets returns all known target names
func confTargets(config *Config) []string {
	buildables, runnables := config.EntryPoints()
	all := append([]string{}, buildables...)
	all = append(all, runnables...)
	return all
}
