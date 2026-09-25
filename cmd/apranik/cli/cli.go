// Package cli implements Apranik's subcommands. Each command is a plain
// function, so main alone owns control flow: this package never reads os.Args
// and never calls os.Exit.
package cli

// Command is one subcommand. Run receives everything after the command name
// and parses it with its own flag.FlagSet.
type Command struct {
	Name  string
	Short string // one line, for the command list
	Usage string // multi-line detail, printed by `apranik help <cmd>`
	Run   func(args []string) error
}

// All is the command set. Adding a command is a new file plus one line here.
func All() []*Command {
	return []*Command{
		cmdRun,
		cmdCheck,
		cmdModules,
		cmdGenerate,
		cmdVersion,
		helpCommand(),
	}
}

func Lookup(name string) *Command {
	for _, command := range All() {
		if command.Name == name {
			return command
		}
	}
	return nil
}
